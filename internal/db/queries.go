package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/natalie-johanek/trench-analytics/internal/models"
)

// QueryFilters holds the parsed common filter parameters for stats queries.
type QueryFilters struct {
	RankedOnly bool
	Since      *time.Time
	Until      *time.Time
	Scenario   *string
	Faction    *string
	MinGames   int
}

// Executor is the common interface for *sql.Tx and *sql.Conn.
type Executor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// InsertRawReport inserts a raw JSON report. Returns false if already exists.
func InsertRawReport(ctx context.Context, ex Executor, id int, rawJSON json.RawMessage) (bool, error) {
	res, err := ex.ExecContext(ctx,
		`INSERT OR IGNORE INTO raw_game_reports (game_report_id, raw_json) VALUES ($1, $2)`,
		id, string(rawJSON),
	)
	if err != nil {
		return false, fmt.Errorf("inserting raw report %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("checking rows affected for report %d: %w", id, err)
	}
	return n > 0, nil
}

// InsertGame inserts a game row.
func InsertGame(ctx context.Context, ex Executor, g models.Game) error {
	_, err := ex.ExecContext(ctx,
		`INSERT INTO games (game_report_id, report_date, scenario_id, is_ranked, winner_warband_id, is_draw)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		g.GameReportID, g.ReportDate, g.ScenarioID, g.IsRanked, g.WinnerWarbandID, g.IsDraw,
	)
	return err
}

// InsertParticipant inserts a game_participants row.
func InsertParticipant(ctx context.Context, ex Executor, p models.GameParticipant) error {
	_, err := ex.ExecContext(ctx,
		`INSERT INTO game_participants
		 (game_report_id, warband_id, player_id, faction_slug, base_faction, result,
		  kills, vp, warband_name, warband_ducats, warband_glory, roster_hash, warband_snapshot)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		p.GameReportID, p.WarbandID, p.PlayerID, p.FactionSlug, p.BaseFaction, p.Result,
		p.Kills, p.VP, p.WarbandName, p.WarbandDucats, p.WarbandGlory, p.RosterHash,
		string(p.WarbandSnapshot),
	)
	return err
}

// InsertUnit inserts a game_units row.
func InsertUnit(ctx context.Context, ex Executor, u models.GameUnit) error {
	_, err := ex.ExecContext(ctx,
		`INSERT INTO game_units
		 (game_report_id, warband_id, row_index, faction_slug, base_faction,
		  unit_id, unit_name, unit_type, cost_ducats, cost_glory, result,
		  equipment, advances, injuries, upgrades)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		u.GameReportID, u.WarbandID, u.RowIndex, u.FactionSlug, u.BaseFaction,
		u.UnitID, u.UnitName, u.UnitType, u.CostDucats, u.CostGlory, u.Result,
		nullJSON(u.Equipment), nullJSON(u.Advances), nullJSON(u.Injuries), nullJSON(u.Upgrades),
	)
	return err
}

// InsertDeed inserts a game_deeds row.
func InsertDeed(ctx context.Context, ex Executor, d models.GameDeed) error {
	_, err := ex.ExecContext(ctx,
		`INSERT INTO game_deeds (game_report_id, row_index, deed_id, deed_name, warband_id)
		 VALUES ($1, $2, $3, $4, $5)`,
		d.GameReportID, d.RowIndex, d.DeedID, d.DeedName, d.WarbandID,
	)
	return err
}

// MaxGameReportID returns the highest game_report_id, or 0 if empty.
func MaxGameReportID(ctx context.Context, ex Executor) (int, error) {
	var id int
	err := ex.QueryRowContext(ctx, "SELECT COALESCE(MAX(game_report_id), 0) FROM raw_game_reports").Scan(&id)
	return id, err
}

// LastCompletedRunTime returns the run_at time of the most recent completed ingestion run.
// Returns nil if no completed runs exist (triggers a full sync).
func LastCompletedRunTime(ctx context.Context, ex Executor) (*time.Time, error) {
	var t sql.NullTime
	err := ex.QueryRowContext(ctx,
		`SELECT run_at FROM ingestion_log WHERE status = 'completed' ORDER BY run_at DESC LIMIT 1`,
	).Scan(&t)
	if err == sql.ErrNoRows || !t.Valid {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying last completed run: %w", err)
	}
	return &t.Time, nil
}

// StartIngestionRun creates a new ingestion_log entry and returns its ID.
func StartIngestionRun(ctx context.Context, ex Executor, runType string, rangeStart, rangeEnd *int) (int, error) {
	var id int
	err := ex.QueryRowContext(ctx,
		`INSERT INTO ingestion_log (run_type, id_range_start, id_range_end)
		 VALUES ($1, $2, $3) RETURNING id`,
		runType, rangeStart, rangeEnd,
	).Scan(&id)
	return id, err
}

// CompleteIngestionRun updates an ingestion_log entry with final stats.
func CompleteIngestionRun(ctx context.Context, ex Executor, id int, status string, found, inserted, scanned, errCount, durationMs int, notes string) error {
	_, err := ex.ExecContext(ctx,
		`UPDATE ingestion_log
		 SET completed_at = current_timestamp, status = $1,
		     reports_found = $2, reports_inserted = $3, ids_scanned = $4,
		     errors = $5, duration_ms = $6, notes = $7
		 WHERE id = $8`,
		status, found, inserted, scanned, errCount, durationMs, notes, id,
	)
	return err
}

// --- Stats Queries (read-only, use pool) ---

// QueryFactionStats returns faction win/pick rates.
func QueryFactionStats(ctx context.Context, pool *sql.DB, f QueryFilters) ([]models.FactionStat, int, error) {
	rows, err := pool.QueryContext(ctx, `
		SELECT
			gp.faction_slug,
			gp.base_faction,
			COALESCE(fl.display_name, gp.faction_slug),
			COALESCE(fl.is_variant, gp.faction_slug != gp.base_faction),
			COUNT(*) FILTER (WHERE gp.result = 'win'),
			COUNT(*) FILTER (WHERE gp.result = 'loss'),
			COUNT(*) FILTER (WHERE gp.result = 'draw'),
			COUNT(*),
			ROUND(COUNT(*) FILTER (WHERE gp.result = 'win') * 1.0 / COUNT(*), 4),
			ROUND(COUNT(*) * 1.0 / SUM(COUNT(*)) OVER (), 4),
			ROUND(AVG(gp.warband_ducats))
		FROM game_participants gp
		JOIN games g ON g.game_report_id = gp.game_report_id
		LEFT JOIN faction_lookup fl ON fl.faction_slug = gp.faction_slug
		WHERE (NOT $1 OR g.is_ranked = true)
		  AND ($2::TIMESTAMP IS NULL OR g.report_date >= $2)
		  AND ($3::TIMESTAMP IS NULL OR g.report_date <= $3)
		  AND ($4::VARCHAR IS NULL OR g.scenario_id = $4)
		  AND ($5::VARCHAR IS NULL OR gp.base_faction = $5)
		GROUP BY gp.faction_slug, gp.base_faction, fl.display_name, fl.is_variant
		HAVING COUNT(*) >= $6
		ORDER BY gp.base_faction, COALESCE(fl.is_variant, true),
			ROUND(COUNT(*) FILTER (WHERE gp.result = 'win') * 1.0 / COUNT(*), 4) DESC
	`, f.RankedOnly, f.Since, f.Until, f.Scenario, f.Faction, f.MinGames)
	if err != nil {
		return nil, 0, fmt.Errorf("querying faction stats: %w", err)
	}
	defer rows.Close()

	stats := []models.FactionStat{} // empty slice, not nil (marshals to [] not null)
	for rows.Next() {
		var s models.FactionStat
		if err := rows.Scan(
			&s.FactionSlug, &s.BaseFaction, &s.DisplayName, &s.IsVariant,
			&s.Wins, &s.Losses, &s.Draws, &s.TotalGames,
			&s.WinRate, &s.PickRate, &s.AvgWarbandDucats,
		); err != nil {
			return nil, 0, fmt.Errorf("scanning faction stat: %w", err)
		}
		stats = append(stats, s)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterating faction stats: %w", err)
	}

	var totalGames int
	if err := pool.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM games g
		WHERE (NOT $1 OR g.is_ranked = true)
		  AND ($2::TIMESTAMP IS NULL OR g.report_date >= $2)
		  AND ($3::TIMESTAMP IS NULL OR g.report_date <= $3)
		  AND ($4::VARCHAR IS NULL OR g.scenario_id = $4)
	`, f.RankedOnly, f.Since, f.Until, f.Scenario).Scan(&totalGames); err != nil {
		return nil, 0, fmt.Errorf("counting total games: %w", err)
	}

	return stats, totalGames, nil
}

// QueryUnitStats returns unit pick/win rates.
func QueryUnitStats(ctx context.Context, pool *sql.DB, f QueryFilters) ([]models.UnitStat, error) {
	rows, err := pool.QueryContext(ctx, `
		WITH faction_totals AS (
			SELECT gp2.base_faction,
				COUNT(DISTINCT gp2.game_report_id) AS total
			FROM game_participants gp2
			JOIN games g2 ON g2.game_report_id = gp2.game_report_id
			WHERE (NOT $1 OR g2.is_ranked = true)
			  AND ($2::TIMESTAMP IS NULL OR g2.report_date >= $2)
			  AND ($3::TIMESTAMP IS NULL OR g2.report_date <= $3)
			  AND ($4::VARCHAR IS NULL OR g2.scenario_id = $4)
			GROUP BY gp2.base_faction
		)
		SELECT
			gu.base_faction,
			gu.unit_id,
			gu.unit_name,
			gu.unit_type,
			COUNT(DISTINCT gu.game_report_id) AS appearances,
			COUNT(DISTINCT gu.game_report_id) FILTER (WHERE gu.result = 'win') AS win_appearances,
			ROUND(COUNT(DISTINCT gu.game_report_id) FILTER (WHERE gu.result = 'win') * 1.0
				/ NULLIF(COUNT(DISTINCT gu.game_report_id), 0), 4) AS win_rate,
			ROUND(COUNT(DISTINCT gu.game_report_id) * 100.0 / NULLIF(ft.total, 0), 2) AS pick_pct
		FROM game_units gu
		JOIN games g ON g.game_report_id = gu.game_report_id
		LEFT JOIN faction_totals ft ON ft.base_faction = gu.base_faction
		WHERE (NOT $1 OR g.is_ranked = true)
		  AND ($2::TIMESTAMP IS NULL OR g.report_date >= $2)
		  AND ($3::TIMESTAMP IS NULL OR g.report_date <= $3)
		  AND ($4::VARCHAR IS NULL OR g.scenario_id = $4)
		  AND ($5::VARCHAR IS NULL OR gu.base_faction = $5)
		GROUP BY gu.base_faction, gu.unit_id, gu.unit_name, gu.unit_type, ft.total
		HAVING COUNT(DISTINCT gu.game_report_id) >= $6
		ORDER BY gu.base_faction, pick_pct DESC
	`, f.RankedOnly, f.Since, f.Until, f.Scenario, f.Faction, f.MinGames)
	if err != nil {
		return nil, fmt.Errorf("querying unit stats: %w", err)
	}
	defer rows.Close()

	stats := []models.UnitStat{}
	for rows.Next() {
		var s models.UnitStat
		if err := rows.Scan(
			&s.BaseFaction, &s.UnitID, &s.UnitName, &s.UnitType,
			&s.Appearances, &s.WinAppearances, &s.WinRate, &s.PickPct,
		); err != nil {
			return nil, fmt.Errorf("scanning unit stat: %w", err)
		}
		stats = append(stats, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating rows: %w", err)
	}
	return stats, nil
}

// QueryEquipmentStats returns equipment popularity for units in a faction.
func QueryEquipmentStats(ctx context.Context, pool *sql.DB, f QueryFilters, unitID string) ([]models.EquipmentStat, error) {
	rows, err := pool.QueryContext(ctx, `
		WITH unit_equipment AS (
			SELECT
				gu.unit_id, gu.unit_name, gu.result,
				json_extract_string(eq.value, '$.i') AS equipment_id,
				json_extract_string(eq.value, '$.n') AS equipment_name
			FROM game_units gu,
				LATERAL (SELECT unnest(from_json(gu.equipment, '["json"]')) AS value) eq
			JOIN games g ON g.game_report_id = gu.game_report_id
			WHERE ($1::VARCHAR IS NULL OR gu.base_faction = $1)
			  AND ($2::VARCHAR IS NULL OR gu.unit_id = $2)
			  AND (NOT $3 OR g.is_ranked = true)
			  AND ($4::TIMESTAMP IS NULL OR g.report_date >= $4)
			  AND ($5::TIMESTAMP IS NULL OR g.report_date <= $5)
			  AND ($6::VARCHAR IS NULL OR g.scenario_id = $6)
		)
		SELECT unit_id, unit_name, equipment_id, equipment_name,
			COUNT(*) AS times_taken,
			COUNT(*) FILTER (WHERE result = 'win') AS times_won
		FROM unit_equipment
		GROUP BY unit_id, unit_name, equipment_id, equipment_name
		HAVING COUNT(*) >= $7
		ORDER BY unit_id, times_taken DESC
	`, f.Faction, nilIfEmpty(unitID), f.RankedOnly, f.Since, f.Until, f.Scenario, f.MinGames)
	if err != nil {
		return nil, fmt.Errorf("querying equipment stats: %w", err)
	}
	defer rows.Close()

	stats := []models.EquipmentStat{}
	for rows.Next() {
		var s models.EquipmentStat
		if err := rows.Scan(&s.UnitID, &s.UnitName, &s.EquipmentID, &s.EquipmentName, &s.TimesTaken, &s.TimesWon); err != nil {
			return nil, fmt.Errorf("scanning equipment stat: %w", err)
		}
		stats = append(stats, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating rows: %w", err)
	}
	return stats, nil
}

// QueryScenarioStats returns win rates per faction per scenario.
func QueryScenarioStats(ctx context.Context, pool *sql.DB, f QueryFilters) ([]models.ScenarioStat, error) {
	rows, err := pool.QueryContext(ctx, `
		SELECT
			g.scenario_id, gp.base_faction,
			COUNT(*) FILTER (WHERE gp.result = 'win') AS wins,
			COUNT(*) AS total,
			ROUND(COUNT(*) FILTER (WHERE gp.result = 'win') * 1.0 / COUNT(*), 4) AS win_rate
		FROM game_participants gp
		JOIN games g ON g.game_report_id = gp.game_report_id
		WHERE (NOT $1 OR g.is_ranked = true)
		  AND ($2::TIMESTAMP IS NULL OR g.report_date >= $2)
		  AND ($3::TIMESTAMP IS NULL OR g.report_date <= $3)
		  AND ($4::VARCHAR IS NULL OR gp.base_faction = $4)
		GROUP BY g.scenario_id, gp.base_faction
		HAVING COUNT(*) >= $5
		ORDER BY g.scenario_id, win_rate DESC
	`, f.RankedOnly, f.Since, f.Until, f.Faction, f.MinGames)
	if err != nil {
		return nil, fmt.Errorf("querying scenario stats: %w", err)
	}
	defer rows.Close()

	stats := []models.ScenarioStat{}
	for rows.Next() {
		var s models.ScenarioStat
		if err := rows.Scan(&s.ScenarioID, &s.BaseFaction, &s.Wins, &s.Total, &s.WinRate); err != nil {
			return nil, fmt.Errorf("scanning scenario stat: %w", err)
		}
		stats = append(stats, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating rows: %w", err)
	}
	return stats, nil
}

// QueryWarbandCostStats returns ducat distribution by faction and result.
func QueryWarbandCostStats(ctx context.Context, pool *sql.DB, f QueryFilters) ([]models.WarbandCostStat, error) {
	rows, err := pool.QueryContext(ctx, `
		SELECT
			gp.base_faction, gp.result,
			ROUND(AVG(gp.warband_ducats)) AS avg_ducats,
			ROUND(MEDIAN(gp.warband_ducats)) AS median_ducats,
			MIN(gp.warband_ducats) AS min_ducats,
			MAX(gp.warband_ducats) AS max_ducats
		FROM game_participants gp
		JOIN games g ON g.game_report_id = gp.game_report_id
		WHERE (NOT $1 OR g.is_ranked = true)
		  AND ($2::TIMESTAMP IS NULL OR g.report_date >= $2)
		  AND ($3::TIMESTAMP IS NULL OR g.report_date <= $3)
		  AND ($4::VARCHAR IS NULL OR g.scenario_id = $4)
		  AND ($5::VARCHAR IS NULL OR gp.base_faction = $5)
		GROUP BY gp.base_faction, gp.result
		ORDER BY gp.base_faction, gp.result
	`, f.RankedOnly, f.Since, f.Until, f.Scenario, f.Faction)
	if err != nil {
		return nil, fmt.Errorf("querying warband cost stats: %w", err)
	}
	defer rows.Close()

	stats := []models.WarbandCostStat{}
	for rows.Next() {
		var s models.WarbandCostStat
		if err := rows.Scan(&s.BaseFaction, &s.Result, &s.AvgDucats, &s.MedianDucats, &s.MinDucats, &s.MaxDucats); err != nil {
			return nil, fmt.Errorf("scanning warband cost stat: %w", err)
		}
		stats = append(stats, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating rows: %w", err)
	}
	return stats, nil
}

// QueryDeedStats returns deed frequency stats.
func QueryDeedStats(ctx context.Context, pool *sql.DB, f QueryFilters) ([]models.DeedStat, error) {
	rows, err := pool.QueryContext(ctx, `
		SELECT
			gd.deed_id, gd.deed_name,
			COUNT(*) AS total_occurrences,
			COUNT(gd.warband_id) AS attributed_occurrences,
			COUNT(DISTINCT gd.game_report_id) AS games_with_deed
		FROM game_deeds gd
		JOIN games g ON g.game_report_id = gd.game_report_id
		WHERE (NOT $1 OR g.is_ranked = true)
		  AND ($2::TIMESTAMP IS NULL OR g.report_date >= $2)
		  AND ($3::TIMESTAMP IS NULL OR g.report_date <= $3)
		  AND ($4::VARCHAR IS NULL OR g.scenario_id = $4)
		GROUP BY gd.deed_id, gd.deed_name
		ORDER BY total_occurrences DESC
	`, f.RankedOnly, f.Since, f.Until, f.Scenario)
	if err != nil {
		return nil, fmt.Errorf("querying deed stats: %w", err)
	}
	defer rows.Close()

	stats := []models.DeedStat{}
	for rows.Next() {
		var s models.DeedStat
		if err := rows.Scan(&s.DeedID, &s.DeedName, &s.TotalOccurrences, &s.AttributedOccurrences, &s.GamesWithDeed); err != nil {
			return nil, fmt.Errorf("scanning deed stat: %w", err)
		}
		stats = append(stats, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating rows: %w", err)
	}
	return stats, nil
}

// QuerySummary returns overall summary stats.
func QuerySummary(ctx context.Context, pool *sql.DB) (models.SummaryResponse, error) {
	var s models.SummaryResponse
	var earliest, latest, lastUpdated sql.NullTime

	err := pool.QueryRowContext(ctx, `
		SELECT
			COUNT(DISTINCT g.game_report_id),
			COUNT(DISTINCT g.game_report_id) FILTER (WHERE g.is_ranked),
			COUNT(DISTINCT g.game_report_id) FILTER (WHERE NOT g.is_ranked),
			MIN(g.report_date),
			MAX(g.report_date),
			COUNT(DISTINCT gp.base_faction),
			(SELECT MAX(il.completed_at) FROM ingestion_log il WHERE il.status = 'completed')
		FROM games g
		JOIN game_participants gp ON g.game_report_id = gp.game_report_id
	`).Scan(&s.TotalGames, &s.RankedGames, &s.UnrankedGames, &earliest, &latest, &s.FactionCount, &lastUpdated)
	if err != nil {
		return s, fmt.Errorf("querying summary: %w", err)
	}

	if earliest.Valid {
		s.EarliestGame = earliest.Time.Format(time.RFC3339)
	}
	if latest.Valid {
		s.LatestGame = latest.Time.Format(time.RFC3339)
	}
	if lastUpdated.Valid {
		s.LastUpdated = lastUpdated.Time.Format(time.RFC3339)
	}
	return s, nil
}

// QueryMeta returns available filter values.
func QueryMeta(ctx context.Context, pool *sql.DB) (models.MetaResponse, error) {
	meta := models.MetaResponse{
		Factions:  []models.MetaFaction{},
		Scenarios: []string{},
	}

	rows, err := pool.QueryContext(ctx, `
		SELECT DISTINCT gp.faction_slug, gp.base_faction,
			COALESCE(fl.display_name, gp.faction_slug),
			COALESCE(fl.is_variant, gp.faction_slug != gp.base_faction)
		FROM game_participants gp
		LEFT JOIN faction_lookup fl ON fl.faction_slug = gp.faction_slug
		ORDER BY gp.base_faction, gp.faction_slug
	`)
	if err != nil {
		return meta, fmt.Errorf("querying meta factions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var f models.MetaFaction
		if err := rows.Scan(&f.FactionSlug, &f.BaseFaction, &f.DisplayName, &f.IsVariant); err != nil {
			return meta, err
		}
		meta.Factions = append(meta.Factions, f)
	}
	if err := rows.Err(); err != nil {
		return meta, err
	}

	srows, err := pool.QueryContext(ctx, "SELECT DISTINCT scenario_id FROM games ORDER BY scenario_id")
	if err != nil {
		return meta, fmt.Errorf("querying meta scenarios: %w", err)
	}
	defer srows.Close()
	for srows.Next() {
		var s string
		if err := srows.Scan(&s); err != nil {
			return meta, err
		}
		meta.Scenarios = append(meta.Scenarios, s)
	}
	if err := srows.Err(); err != nil {
		return meta, err
	}

	var earliest, latest sql.NullTime
	if err := pool.QueryRowContext(ctx, "SELECT MIN(report_date), MAX(report_date) FROM games").Scan(&earliest, &latest); err != nil {
		return meta, fmt.Errorf("querying meta date range: %w", err)
	}
	if earliest.Valid {
		meta.DateRange.Earliest = earliest.Time.Format(time.RFC3339)
	}
	if latest.Valid {
		meta.DateRange.Latest = latest.Time.Format(time.RFC3339)
	}

	return meta, nil
}

// QueryLatestIngestion returns the most recent ingestion run, or nil if none.
func QueryLatestIngestion(ctx context.Context, pool *sql.DB) (*models.IngestionRun, error) {
	var r models.IngestionRun
	var runAt, completedAt sql.NullTime

	err := pool.QueryRowContext(ctx, `
		SELECT id, run_at, completed_at, status, run_type,
		       reports_found, reports_inserted, ids_scanned, errors
		FROM ingestion_log ORDER BY run_at DESC LIMIT 1
	`).Scan(&r.ID, &runAt, &completedAt, &r.Status, &r.RunType,
		&r.ReportsFound, &r.ReportsInserted, &r.IDsScanned, &r.Errors)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying latest ingestion: %w", err)
	}
	if runAt.Valid {
		r.RunAt = runAt.Time
	}
	if completedAt.Valid {
		r.CompletedAt = &completedAt.Time
	}
	return &r, nil
}

// QueryPreviousCompletedRun returns the most recent completed run before the given run ID.
func QueryPreviousCompletedRun(ctx context.Context, pool *sql.DB, currentID int) (*models.IngestionRun, error) {
	var r models.IngestionRun
	var runAt, completedAt sql.NullTime

	err := pool.QueryRowContext(ctx, `
		SELECT id, run_at, completed_at, status, run_type,
		       reports_found, reports_inserted, ids_scanned, errors
		FROM ingestion_log
		WHERE id < $1 AND status = 'completed'
		ORDER BY run_at DESC LIMIT 1
	`, currentID).Scan(&r.ID, &runAt, &completedAt, &r.Status, &r.RunType,
		&r.ReportsFound, &r.ReportsInserted, &r.IDsScanned, &r.Errors)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying previous completed run: %w", err)
	}
	if runAt.Valid {
		r.RunAt = runAt.Time
	}
	if completedAt.Valid {
		r.CompletedAt = &completedAt.Time
	}
	return &r, nil
}

// nullJSON converts a json.RawMessage to a nullable string for DuckDB.
func nullJSON(data json.RawMessage) any {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	return string(data)
}

// nilIfEmpty returns nil if s is empty, otherwise &s.
func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
