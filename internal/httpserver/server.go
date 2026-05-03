package httpserver

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// NewRouter builds a chi router with standard middleware and slog-based request logging.
func NewRouter(log *slog.Logger) *chi.Mux {
	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(requestLogger(log))
	return r
}

func requestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, req.ProtoMajor)
			defer func() {
				log.Info("http",
					"method", req.Method,
					"path", req.URL.Path,
					"status", ww.Status(),
					"bytes", ww.BytesWritten(),
					"duration", time.Since(start).String(),
					"remote", req.RemoteAddr,
				)
			}()
			next.ServeHTTP(ww, req)
		})
	}
}

// Serve runs the HTTP server until ctx is cancelled, then shuts down gracefully.
func Serve(ctx context.Context, log *slog.Logger, addr string, h http.Handler) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		err := srv.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			log.Warn("http server shutdown", "err", err)
		}
		err := <-errCh
		if err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}
