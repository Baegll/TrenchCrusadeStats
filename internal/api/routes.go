package api

import (
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/natalie-johanek/trench-analytics/internal/db"
	"github.com/natalie-johanek/trench-analytics/internal/ingestion"
)

// Server holds all API dependencies for lifecycle management.
type Server struct {
	Router  *chi.Mux
	Sync    *SyncHandlers
	limiter *rateLimiter
}

// Close cleans up background resources.
func (s *Server) Close() {
	s.limiter.Close()
}

// NewServer creates the Chi router with all routes registered.
func NewServer(store *db.DB, syncer *ingestion.Syncer, apiKey, adminUser, adminPass string, rateLimitPerMin int) *Server {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)

	h := &Handlers{DB: store}
	sh := &SyncHandlers{Syncer: syncer, DB: store}
	rl := newRateLimiter(rateLimitPerMin)

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
	})

	// Admin (basic auth)
	r.Group(func(r chi.Router) {
		r.Use(BasicAuth(adminUser, adminPass))
		r.Post("/admin/sync", sh.TriggerSync)
		r.Get("/admin/sync/status", sh.SyncStatus)
	})

	return &Server{Router: r, Sync: sh, limiter: rl}
}
