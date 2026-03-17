package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/natalie-johanek/trench-analytics/internal/db"
	"github.com/natalie-johanek/trench-analytics/internal/ingestion"
	"github.com/natalie-johanek/trench-analytics/internal/logging"
)

// Server holds all API dependencies for lifecycle management.
type Server struct {
	Router  *chi.Mux
	Sync    *SyncHandlers
	limiter *rateLimiter
	LogBuf  *logging.Buffer
}

// Close cleans up background resources.
func (s *Server) Close() {
	s.limiter.Close()
}

// NewServer creates the Chi router with all routes registered.
func NewServer(store *db.DB, syncer *ingestion.Syncer, apiKey, adminUser, adminPass string, rateLimitPerMin int) *Server {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(WideEventMiddleware)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)

	h := &Handlers{DB: store}
	sh := &SyncHandlers{Syncer: syncer, DB: store}
	rl := newRateLimiter(rateLimitPerMin)
	logBuf := logging.NewBuffer(1000)
	logging.SetBuffer(logBuf)
	lh := &LogsHandler{Buffer: logBuf}

	// Dashboard
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Write(dashboardHTML)
	})

	// OpenAPI spec & docs
	r.Get("/api/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		w.Write(openapiSpec)
	})
	r.Get("/api/docs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<!DOCTYPE html>
<html><head><title>API Docs</title>
<link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
</head><body>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script>SwaggerUIBundle({url:"/api/openapi.yaml",dom_id:"#swagger-ui"})</script>
</body></html>`))
	})

	// Public
	r.Get("/api/v1/health", h.GetHealth)

	// Stats API (API key auth + rate limiting)
	r.Group(func(r chi.Router) {
		r.Use(APIKeyAuth(apiKey))
		r.Use(RateLimit(rl))
		r.Get("/api/v1/stats/factions", h.GetFactionStats)
		r.Get("/api/v1/stats/units", h.GetUnitStats)
		r.Get("/api/v1/stats/equipment", h.GetEquipmentStats)
		r.Get("/api/v1/stats/scenarios", h.GetScenarioStats)
		r.Get("/api/v1/stats/warband-costs", h.GetWarbandCostStats)
		r.Get("/api/v1/stats/deeds", h.GetDeedStats)
		r.Get("/api/v1/stats/summary", h.GetSummary)
		r.Get("/api/v1/stats/meta", h.GetMeta)
		r.Get("/api/v1/sync/status", sh.SyncStatus)
	})

	// Admin (basic auth)
	r.Group(func(r chi.Router) {
		r.Use(BasicAuth(adminUser, adminPass))
		r.Post("/admin/sync", sh.TriggerSync)
		r.Get("/admin/sync/status", sh.SyncStatus)
		r.Get("/admin/logs", lh.ServeHTTP)
	})

	return &Server{Router: r, Sync: sh, limiter: rl, LogBuf: logBuf}
}
