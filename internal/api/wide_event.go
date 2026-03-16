package api

import (
	"log/slog"
	"net/http"

	"github.com/natalie-johanek/trench-analytics/internal/logging"
)

// WideEventMiddleware creates a wide event per request, attaches it to context,
// and emits it once when the response completes. Handlers enrich the event
// with business context via logging.FromContext(r.Context()).Set(...).
func WideEventMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		event := logging.New(
			"method", r.Method,
			"path", r.URL.Path,
			"query", r.URL.RawQuery,
			"remote_ip", r.RemoteAddr,
			"user_agent", r.UserAgent(),
		)

		ctx := logging.WithEvent(r.Context(), event)
		rw := &responseWriter{ResponseWriter: w, status: 200}

		next.ServeHTTP(rw, r.WithContext(ctx))

		event.Set(
			"status", rw.status,
			"bytes", rw.bytes,
			"outcome", outcomeFromStatus(rw.status),
		)

		level := slog.LevelInfo
		if rw.status >= 500 {
			level = slog.LevelError
		} else if rw.status >= 400 {
			level = slog.LevelWarn
		}
		event.Emit(level)
	})
}

// responseWriter wraps http.ResponseWriter to capture status code and bytes written.
type responseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.bytes += n
	return n, err
}

func outcomeFromStatus(status int) string {
	switch {
	case status < 400:
		return "success"
	case status < 500:
		return "client_error"
	default:
		return "server_error"
	}
}
