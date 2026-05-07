package httpserver

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestServe_gracefulShutdown(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- Serve(ctx, log, "127.0.0.1:0", h)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Serve: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for Serve to return")
	}
}

func TestNewRouter_healthz(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := NewRouter(log)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	ts := httptest.NewServer(r)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
}

func TestWebhookUpdateHandler(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	var n int
	r := NewRouter(log)
	r.Post("/webhook/update", WebhookUpdateHandler("secret", func() { n++ }))
	ts := httptest.NewServer(r)
	defer ts.Close()

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/webhook/update", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-API-Key", "secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("want %d, got %d", http.StatusAccepted, resp.StatusCode)
	}
	if n != 1 {
		t.Fatalf("trigger count: %d", n)
	}

	req2, _ := http.NewRequest(http.MethodPost, ts.URL+"/webhook/update", nil)
	req2.Header.Set("X-API-Key", "wrong")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", resp2.StatusCode)
	}

	req3, _ := http.NewRequest(http.MethodPost, ts.URL+"/webhook/update", nil)
	req3.Header.Set("Authorization", "Bearer secret")
	resp3, err := http.DefaultClient.Do(req3)
	if err != nil {
		t.Fatal(err)
	}
	resp3.Body.Close()
	if resp3.StatusCode != http.StatusAccepted {
		t.Fatalf("bearer: want %d, got %d", http.StatusAccepted, resp3.StatusCode)
	}
	if n != 2 {
		t.Fatalf("trigger count after bearer: %d", n)
	}
}
