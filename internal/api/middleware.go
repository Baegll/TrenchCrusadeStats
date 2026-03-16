package api

import (
	"crypto/subtle"
	"net/http"

	"github.com/natalie-johanek/trench-analytics/internal/models"
)

// BasicAuth returns middleware that checks basic auth credentials using constant-time comparison.
func BasicAuth(username, password string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, p, ok := r.BasicAuth()
			if !ok ||
				subtle.ConstantTimeCompare([]byte(u), []byte(username)) != 1 ||
				subtle.ConstantTimeCompare([]byte(p), []byte(password)) != 1 {
				w.Header().Set("WWW-Authenticate", `Basic realm="admin"`)
				writeError(w, http.StatusUnauthorized, "unauthorized", "invalid credentials")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// APIKeyAuth returns middleware that checks the X-Api-Key header using constant-time comparison.
func APIKeyAuth(key string) func(http.Handler) http.Handler {
	keyBytes := []byte(key)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			provided := []byte(r.Header.Get("X-Api-Key"))
			if subtle.ConstantTimeCompare(provided, keyBytes) != 1 {
				writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or missing API key")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RateLimit returns middleware that limits requests per key.
func RateLimit(limiter *rateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("X-Api-Key")
			if key == "" {
				key = r.RemoteAddr
			}
			if !limiter.allow(key) {
				writeError(w, http.StatusTooManyRequests, "rate_limited",
					"exceeded rate limit")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// writeError writes a standard error response.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, models.ErrorResponse{
		Error:   code,
		Message: message,
		Status:  status,
	})
}
