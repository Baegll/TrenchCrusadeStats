package api

import (
	"net/http"
	"strconv"

	"github.com/natalie-johanek/trench-analytics/internal/logging"
)

// LogsHandler serves recent wide events from the in-memory ring buffer.
// GET /admin/logs?n=100&level=ERROR&path=/api/v1/stats/factions
type LogsHandler struct {
	Buffer *logging.Buffer
}

// ServeHTTP handles GET /admin/logs with optional filters.
func (h *LogsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n := 100
	if v := r.URL.Query().Get("n"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			n = parsed
		}
	}

	events := h.Buffer.Recent(n)

	// Apply filters
	levelFilter := r.URL.Query().Get("level")
	pathFilter := r.URL.Query().Get("path")
	outcomeFilter := r.URL.Query().Get("outcome")

	if levelFilter != "" || pathFilter != "" || outcomeFilter != "" {
		filtered := make([]map[string]any, 0, len(events))
		for _, e := range events {
			if levelFilter != "" {
				if lvl, ok := e["level"].(string); !ok || lvl != levelFilter {
					continue
				}
			}
			if pathFilter != "" {
				if p, ok := e["path"].(string); !ok || p != pathFilter {
					continue
				}
			}
			if outcomeFilter != "" {
				if o, ok := e["outcome"].(string); !ok || o != outcomeFilter {
					continue
				}
			}
			filtered = append(filtered, e)
		}
		events = filtered
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"count":  len(events),
		"events": events,
	})
}
