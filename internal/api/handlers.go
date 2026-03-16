package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/natalie-johanek/trench-analytics/internal/db"
	"github.com/natalie-johanek/trench-analytics/internal/logging"
	"github.com/natalie-johanek/trench-analytics/internal/models"
)

// Handlers holds dependencies for HTTP handlers.
type Handlers struct {
	DB *db.DB
}

// parseFilters extracts common query parameters from the request.
func parseFilters(r *http.Request) (db.QueryFilters, error) {
	f := db.QueryFilters{
		RankedOnly: true,
		MinGames:   5,
	}

	if v := r.URL.Query().Get("ranked_only"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return f, err
		}
		f.RankedOnly = b
	}

	if v := r.URL.Query().Get("min_games"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return f, err
		}
		if n < 1 {
			n = 1
		}
		if n > 10000 {
			n = 10000
		}
		f.MinGames = n
	}

	if v := r.URL.Query().Get("since"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			return f, err
		}
		f.Since = &t
	}

	if v := r.URL.Query().Get("until"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			return f, err
		}
		f.Until = &t
	}

	if v := r.URL.Query().Get("scenario"); v != "" {
		if len(v) > 200 {
			v = v[:200]
		}
		f.Scenario = &v
	}

	if v := r.URL.Query().Get("faction"); v != "" {
		if len(v) > 200 {
			v = v[:200]
		}
		f.Faction = &v
	}

	return f, nil
}

// filtersToResponse converts QueryFilters to the API response shape.
func filtersToResponse(f db.QueryFilters) models.StatsFilters {
	sf := models.StatsFilters{
		RankedOnly: f.RankedOnly,
		MinGames:   f.MinGames,
	}
	if f.Since != nil {
		sf.Since = f.Since.Format("2006-01-02")
	}
	if f.Until != nil {
		sf.Until = f.Until.Format("2006-01-02")
	}
	if f.Scenario != nil {
		sf.Scenario = *f.Scenario
	}
	if f.Faction != nil {
		sf.Faction = *f.Faction
	}
	return sf
}

// GetFactionStats handles GET /api/v1/stats/factions.
func (h *Handlers) GetFactionStats(w http.ResponseWriter, r *http.Request) {
	f, err := parseFilters(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_parameter", err.Error())
		return
	}

	stats, totalGames, err := db.QueryFactionStats(r.Context(), h.DB.Pool(), f)
	if err != nil {
		logging.FromContext(r.Context()).Set("error", err.Error(), "error_source", "query faction stats")
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to query faction stats")
		return
	}

	logging.FromContext(r.Context()).Set(
		"endpoint", "factions",
		"filters", filtersToResponse(f),
		"result_count", len(stats),
		"total_games", totalGames,
	)

	writeJSON(w, http.StatusOK, models.FactionStatsResponse{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Filters:     filtersToResponse(f),
		TotalGames:  totalGames,
		Factions:    stats,
	})
}

// GetUnitStats handles GET /api/v1/stats/units.
func (h *Handlers) GetUnitStats(w http.ResponseWriter, r *http.Request) {
	f, err := parseFilters(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_parameter", err.Error())
		return
	}

	stats, err := db.QueryUnitStats(r.Context(), h.DB.Pool(), f)
	if err != nil {
		logging.FromContext(r.Context()).Set("error", err.Error(), "error_source", "query unit stats")
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to query unit stats")
		return
	}

	writeJSON(w, http.StatusOK, models.UnitStatsResponse{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Filters:     filtersToResponse(f),
		Units:       stats,
	})
}

// GetEquipmentStats handles GET /api/v1/stats/equipment.
func (h *Handlers) GetEquipmentStats(w http.ResponseWriter, r *http.Request) {
	f, err := parseFilters(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_parameter", err.Error())
		return
	}

	unitID := r.URL.Query().Get("unit")

	stats, err := db.QueryEquipmentStats(r.Context(), h.DB.Pool(), f, unitID)
	if err != nil {
		logging.FromContext(r.Context()).Set("error", err.Error(), "error_source", "query equipment stats")
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to query equipment stats")
		return
	}

	writeJSON(w, http.StatusOK, models.EquipmentStatsResponse{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Filters:     filtersToResponse(f),
		Equipment:   stats,
	})
}

// GetScenarioStats handles GET /api/v1/stats/scenarios.
func (h *Handlers) GetScenarioStats(w http.ResponseWriter, r *http.Request) {
	f, err := parseFilters(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_parameter", err.Error())
		return
	}

	stats, err := db.QueryScenarioStats(r.Context(), h.DB.Pool(), f)
	if err != nil {
		logging.FromContext(r.Context()).Set("error", err.Error(), "error_source", "query scenario stats")
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to query scenario stats")
		return
	}

	writeJSON(w, http.StatusOK, models.ScenarioStatsResponse{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Filters:     filtersToResponse(f),
		Scenarios:   stats,
	})
}

// GetWarbandCostStats handles GET /api/v1/stats/warband-costs.
func (h *Handlers) GetWarbandCostStats(w http.ResponseWriter, r *http.Request) {
	f, err := parseFilters(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_parameter", err.Error())
		return
	}

	stats, err := db.QueryWarbandCostStats(r.Context(), h.DB.Pool(), f)
	if err != nil {
		logging.FromContext(r.Context()).Set("error", err.Error(), "error_source", "query warband cost stats")
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to query warband cost stats")
		return
	}

	writeJSON(w, http.StatusOK, models.WarbandCostStatsResponse{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Filters:     filtersToResponse(f),
		Costs:       stats,
	})
}

// GetDeedStats handles GET /api/v1/stats/deeds.
func (h *Handlers) GetDeedStats(w http.ResponseWriter, r *http.Request) {
	f, err := parseFilters(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_parameter", err.Error())
		return
	}

	stats, err := db.QueryDeedStats(r.Context(), h.DB.Pool(), f)
	if err != nil {
		logging.FromContext(r.Context()).Set("error", err.Error(), "error_source", "query deed stats")
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to query deed stats")
		return
	}

	writeJSON(w, http.StatusOK, models.DeedStatsResponse{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Filters:     filtersToResponse(f),
		Deeds:       stats,
	})
}

// GetSummary handles GET /api/v1/stats/summary.
func (h *Handlers) GetSummary(w http.ResponseWriter, r *http.Request) {
	s, err := db.QuerySummary(r.Context(), h.DB.Pool())
	if err != nil {
		logging.FromContext(r.Context()).Set("error", err.Error(), "error_source", "query summary")
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to query summary")
		return
	}
	writeJSON(w, http.StatusOK, s)
}

// GetMeta handles GET /api/v1/stats/meta.
func (h *Handlers) GetMeta(w http.ResponseWriter, r *http.Request) {
	m, err := db.QueryMeta(r.Context(), h.DB.Pool())
	if err != nil {
		logging.FromContext(r.Context()).Set("error", err.Error(), "error_source", "query meta")
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to query meta")
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// GetHealth handles GET /api/v1/health.
func (h *Handlers) GetHealth(w http.ResponseWriter, r *http.Request) {
	run, err := db.QueryLatestIngestion(r.Context(), h.DB.Pool())
	if err != nil {
		logging.FromContext(r.Context()).Set("error", err.Error(), "error_source", "query health")
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to query health")
		return
	}

	resp := models.HealthResponse{Status: "ok"}
	if run != nil && run.CompletedAt != nil {
		resp.LastIngestion = run.CompletedAt.Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, resp)
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("encoding response", "err", err)
	}
}
