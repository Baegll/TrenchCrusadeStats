package models

// StatsFilters represents the common query parameters for stats endpoints.
// @Description Common filter parameters for all stats queries
type StatsFilters struct {
	RankedOnly bool   `json:"ranked_only"`
	Since      string `json:"since,omitempty"`
	Until      string `json:"until,omitempty"`
	Scenario   string `json:"scenario,omitempty"`
	Faction    string `json:"faction,omitempty"`
	MinGames   int    `json:"min_games"`
}

// FactionStatsResponse is the response for GET /api/v1/stats/factions.
// @Description Faction win rates and pick rates
type FactionStatsResponse struct {
	GeneratedAt string        `json:"generated_at"`
	Filters     StatsFilters  `json:"filters"`
	TotalGames  int           `json:"total_games"`
	Factions    []FactionStat `json:"factions"`
}

// FactionStat is a single faction's stats row.
type FactionStat struct {
	FactionSlug      string  `json:"faction_slug"`
	BaseFaction      string  `json:"base_faction"`
	DisplayName      string  `json:"display_name"`
	IsVariant        bool    `json:"is_variant"`
	Wins             int     `json:"wins"`
	Losses           int     `json:"losses"`
	Draws            int     `json:"draws"`
	TotalGames       int     `json:"total_games"`
	WinRate          float64 `json:"win_rate"`
	PickRate         float64 `json:"pick_rate"`
	AvgWarbandDucats int     `json:"avg_warband_ducats"`
}

// UnitStatsResponse is the response for GET /api/v1/stats/units.
type UnitStatsResponse struct {
	GeneratedAt string       `json:"generated_at"`
	Filters     StatsFilters `json:"filters"`
	Units       []UnitStat   `json:"units"`
}

// UnitStat is a single unit's stats row.
type UnitStat struct {
	BaseFaction string  `json:"base_faction"`
	UnitID      string  `json:"unit_id"`
	UnitName    string  `json:"unit_name"`
	UnitType    string  `json:"unit_type"`
	Appearances int     `json:"appearances"`
	WinAppearances int  `json:"win_appearances"`
	WinRate     float64 `json:"win_rate"`
	PickPct     float64 `json:"pick_pct"`
}

// EquipmentStatsResponse is the response for GET /api/v1/stats/equipment.
type EquipmentStatsResponse struct {
	GeneratedAt string          `json:"generated_at"`
	Filters     StatsFilters    `json:"filters"`
	Equipment   []EquipmentStat `json:"equipment"`
}

// EquipmentStat is a single equipment item's usage stats.
type EquipmentStat struct {
	UnitID        string `json:"unit_id"`
	UnitName      string `json:"unit_name"`
	EquipmentID   string `json:"equipment_id"`
	EquipmentName string `json:"equipment_name"`
	TimesTaken    int    `json:"times_taken"`
	TimesWon      int    `json:"times_won"`
}

// ScenarioStatsResponse is the response for GET /api/v1/stats/scenarios.
type ScenarioStatsResponse struct {
	GeneratedAt string         `json:"generated_at"`
	Filters     StatsFilters   `json:"filters"`
	Scenarios   []ScenarioStat `json:"scenarios"`
}

// ScenarioStat is a faction's win rate for a specific scenario.
type ScenarioStat struct {
	ScenarioID  string  `json:"scenario_id"`
	BaseFaction string  `json:"base_faction"`
	Wins        int     `json:"wins"`
	Total       int     `json:"total"`
	WinRate     float64 `json:"win_rate"`
}

// WarbandCostStatsResponse is the response for GET /api/v1/stats/warband-costs.
type WarbandCostStatsResponse struct {
	GeneratedAt string            `json:"generated_at"`
	Filters     StatsFilters      `json:"filters"`
	Costs       []WarbandCostStat `json:"costs"`
}

// WarbandCostStat is ducat distribution for a faction+result combination.
type WarbandCostStat struct {
	BaseFaction  string `json:"base_faction"`
	Result       string `json:"result"`
	AvgDucats    int    `json:"avg_ducats"`
	MedianDucats int    `json:"median_ducats"`
	MinDucats    int    `json:"min_ducats"`
	MaxDucats    int    `json:"max_ducats"`
}

// DeedStatsResponse is the response for GET /api/v1/stats/deeds.
type DeedStatsResponse struct {
	GeneratedAt string     `json:"generated_at"`
	Filters     StatsFilters `json:"filters"`
	Deeds       []DeedStat `json:"deeds"`
}

// DeedStat is a single deed's frequency stats.
type DeedStat struct {
	DeedID                string `json:"deed_id"`
	DeedName              string `json:"deed_name"`
	TotalOccurrences      int    `json:"total_occurrences"`
	AttributedOccurrences int    `json:"attributed_occurrences"`
	GamesWithDeed         int    `json:"games_with_deed"`
}

// SummaryResponse is the response for GET /api/v1/stats/summary.
type SummaryResponse struct {
	TotalGames    int    `json:"total_games"`
	RankedGames   int    `json:"ranked_games"`
	UnrankedGames int    `json:"unranked_games"`
	EarliestGame  string `json:"earliest_game"`
	LatestGame    string `json:"latest_game"`
	FactionCount  int    `json:"faction_count"`
	LastUpdated   string `json:"last_updated"`
}

// MetaResponse is the response for GET /api/v1/stats/meta.
type MetaResponse struct {
	Factions  []MetaFaction `json:"factions"`
	Scenarios []string      `json:"scenarios"`
	DateRange MetaDateRange `json:"date_range"`
}

// MetaFaction is a faction entry in the meta response.
type MetaFaction struct {
	FactionSlug string `json:"faction_slug"`
	BaseFaction string `json:"base_faction"`
	DisplayName string `json:"display_name"`
	IsVariant   bool   `json:"is_variant"`
}

// MetaDateRange is the date range of available data.
type MetaDateRange struct {
	Earliest string `json:"earliest"`
	Latest   string `json:"latest"`
}

// SyncStatusResponse is the response for GET /admin/sync/status.
type SyncStatusResponse struct {
	Status          string  `json:"status"`
	RunType         string  `json:"run_type,omitempty"`
	StartedAt       string  `json:"started_at,omitempty"`
	CompletedAt     *string `json:"completed_at,omitempty"`
	ReportsFound    int     `json:"reports_found"`
	ReportsInserted int     `json:"reports_inserted"`
	IDsScanned      int     `json:"ids_scanned"`
	TotalItems      int     `json:"total_items"`
	Errors          int     `json:"errors"`
	PrevStartedAt   *string `json:"prev_started_at,omitempty"`
	PrevCompletedAt *string `json:"prev_completed_at,omitempty"`
}

// ErrorResponse is the standard error response shape for all API errors.
// @Description Standard error response
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Status  int    `json:"status"`
}

// HealthResponse is the response for GET /api/v1/health.
type HealthResponse struct {
	Status        string `json:"status"`
	LastIngestion string `json:"last_ingestion,omitempty"`
}
