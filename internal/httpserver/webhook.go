package httpserver

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// WebhookUpdateHandler verifies the shared secret and invokes trigger (typically enqueueing an update tick).
// key must be non-empty; callers should register this only when a key is configured.
func WebhookUpdateHandler(key string, trigger func()) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := extractAPIKey(r)
		if !constantTimeKeyEq(key, got) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		trigger()
		w.WriteHeader(http.StatusAccepted)
	}
}

func extractAPIKey(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("X-API-Key")); v != "" {
		return v
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return strings.TrimSpace(parts[1])
	}
	return ""
}

func constantTimeKeyEq(expected, got string) bool {
	if len(expected) != len(got) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(got)) == 1
}
