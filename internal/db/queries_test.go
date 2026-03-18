package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/natalie-johanek/trench-analytics/internal/models"
)

// testDB opens an in-memory DuckDB with migrations applied.
func testDB(t *testing.T) *DB {
	t.Helper()
	ctx := context.Background()
	d, err := Open(ctx, "", Migrations{1: Migration001, 2: Migration002})
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// seedGames inserts test data: 4 games across 3 factions.
func seedGames(t *testing.T, d *DB) {
	t.Helper()
	ctx := context.Background()
	conn, release, err := d.AcquireWriter(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	games := []struct {
		id       int
		date     time.Time
		scenario string
		ranked   bool
		winner   *int
	}{
		{1, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), "sc_fieldsofglory", true, intP(100)},
		{2, time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC), "sc_fieldsofglory", true, intP(100)},
		{3, time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC), "sc_ambush", false, intP(300)},
		{4, time.Date(2025, 2, 2, 0, 0, 0, 0, time.UTC), "sc_ambush", false, intP(100)},
	}

	participants := []models.GameParticipant{
		// Game 1: A(100) wins vs B(200)
		{GameReportID: 1, WarbandID: 100, PlayerID: 1, FactionSlug: "fc_newantioch", BaseFaction: "fc_newantioch", Result: "win", Kills: 3, VP: 1, WarbandName: "WA", WarbandDucats: 500, RosterHash: "h1"},
		{GameReportID: 1, WarbandID: 200, PlayerID: 2, FactionSlug: "fc_hereticlegion", BaseFaction: "fc_hereticlegion", Result: "loss", Kills: 1, VP: 0, WarbandName: "WB", WarbandDucats: 450, RosterHash: "h2"},
		// Game 2: A(100) wins vs B(200)
		{GameReportID: 2, WarbandID: 100, PlayerID: 1, FactionSlug: "fc_newantioch", BaseFaction: "fc_newantioch", Result: "win", Kills: 4, VP: 2, WarbandName: "WA", WarbandDucats: 520, RosterHash: "h1"},
		{GameReportID: 2, WarbandID: 200, PlayerID: 2, FactionSlug: "fc_hereticlegion", BaseFaction: "fc_hereticlegion", Result: "loss", Kills: 2, VP: 0, WarbandName: "WB", WarbandDucats: 460, RosterHash: "h2"},
		// Game 3: C(300) wins vs A(100) — unranked
		{GameReportID: 3, WarbandID: 300, PlayerID: 3, FactionSlug: "fc_trenchpilgrim", BaseFaction: "fc_trenchpilgrim", Result: "win", Kills: 5, VP: 3, WarbandName: "WC", WarbandDucats: 600, RosterHash: "h3"},
		{GameReportID: 3, WarbandID: 100, PlayerID: 1, FactionSlug: "fc_newantioch", BaseFaction: "fc_newantioch", Result: "loss", Kills: 2, VP: 1, WarbandName: "WA", WarbandDucats: 510, RosterHash: "h1"},
		// Game 4: A(100) wins vs C(300) — unranked
		{GameReportID: 4, WarbandID: 100, PlayerID: 1, FactionSlug: "fc_newantioch", BaseFaction: "fc_newantioch", Result: "win", Kills: 3, VP: 2, WarbandName: "WA", WarbandDucats: 530, RosterHash: "h1"},
		{GameReportID: 4, WarbandID: 300, PlayerID: 3, FactionSlug: "fc_trenchpilgrim", BaseFaction: "fc_trenchpilgrim", Result: "loss", Kills: 1, VP: 0, WarbandName: "WC", WarbandDucats: 580, RosterHash: "h3"},
	}

	units := []models.GameUnit{
		{GameReportID: 1, WarbandID: 100, RowIndex: 0, FactionSlug: "fc_newantioch", BaseFaction: "fc_newantioch", UnitID: "md_leader", UnitName: "Leader", UnitType: "elite", CostDucats: 100, Result: "win", Equipment: json.RawMessage(`[{"n":"Sword","i":"eq_sword"}]`)},
		{GameReportID: 1, WarbandID: 100, RowIndex: 1, FactionSlug: "fc_newantioch", BaseFaction: "fc_newantioch", UnitID: "md_trooper", UnitName: "Trooper", UnitType: "standard", CostDucats: 50, Result: "win"},
		{GameReportID: 1, WarbandID: 200, RowIndex: 0, FactionSlug: "fc_hereticlegion", BaseFaction: "fc_hereticlegion", UnitID: "md_heretic", UnitName: "Heretic", UnitType: "standard", CostDucats: 40, Result: "loss"},
	}

	deeds := []models.GameDeed{
		{GameReportID: 1, RowIndex: 0, DeedID: "gd_sniper", DeedName: "Sniper", WarbandID: intP(100)},
		{GameReportID: 1, RowIndex: 1, DeedID: "gd_brave", DeedName: "Brave"},
		{GameReportID: 2, RowIndex: 0, DeedID: "gd_sniper", DeedName: "Sniper", WarbandID: intP(100)},
	}

	for _, g := range games {
		if _, err := conn.ExecContext(ctx, `INSERT INTO raw_game_reports (game_report_id) VALUES ($1)`, g.id); err != nil {
			t.Fatal(err)
		}
		isDraw := g.winner == nil
		if err := InsertGame(ctx, conn, models.Game{GameReportID: g.id, ReportDate: g.date, ScenarioID: g.scenario, IsRanked: g.ranked, WinnerWarbandID: g.winner, IsDraw: isDraw}); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range participants {
		if err := InsertParticipant(ctx, conn, p); err != nil {
			t.Fatal(err)
		}
	}
	for _, u := range units {
		if err := InsertUnit(ctx, conn, u); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range deeds {
		if err := InsertDeed(ctx, conn, d); err != nil {
			t.Fatal(err)
		}
	}
}

func intP(v int) *int { return &v }

func TestOpen_AndMigrate(t *testing.T) {
	d := testDB(t)
	// Verify tables exist
	var count int
	if err := d.Pool().QueryRow("SELECT COUNT(*) FROM faction_lookup").Scan(&count); err != nil {
		t.Fatalf("faction_lookup query failed: %v", err)
	}
	if count == 0 {
		t.Error("faction_lookup should be seeded")
	}
}

func TestOpen_MigrationIdempotent(t *testing.T) {
	ctx := context.Background()
	d, err := Open(ctx, "", Migrations{1: Migration001, 2: Migration002})
	if err != nil {
		t.Fatal(err)
	}
	d.Close()
	// Open again — should not fail on re-apply
	d2, err := Open(ctx, "", Migrations{1: Migration001, 2: Migration002})
	if err != nil {
		t.Fatalf("second open failed: %v", err)
	}
	d2.Close()
}

func TestInsertRawReport_Idempotent(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	conn, release, err := d.AcquireWriter(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	isNew, err := InsertRawReport(ctx, conn, 999)
	if err != nil {
		t.Fatal(err)
	}
	if !isNew {
		t.Error("first insert should be new")
	}

	isNew2, err := InsertRawReport(ctx, conn, 999)
	if err != nil {
		t.Fatal(err)
	}
	if isNew2 {
		t.Error("second insert should not be new")
	}
}

func TestExecTx_Rollback(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	conn, release, err := d.AcquireWriter(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	// Insert raw first
	InsertRawReport(ctx, conn, 888)

	// Try a tx that fails partway
	txErr := ExecTx(ctx, conn, func(tx *sql.Tx) error {
		if err := InsertGame(ctx, tx, models.Game{GameReportID: 888, ReportDate: time.Now(), ScenarioID: "sc_test", IsRanked: true}); err != nil {
			return err
		}
		// This should fail — invalid result value
		_, err := tx.ExecContext(ctx, `INSERT INTO game_participants (game_report_id, warband_id, player_id, faction_slug, base_faction, result, roster_hash, warband_snapshot) VALUES (888, 1, 1, 'fc_test', 'fc_test', 'INVALID', 'h', '{}')`)
		return err
	})
	if txErr == nil {
		t.Fatal("expected tx error")
	}

	// Game should NOT exist (rolled back)
	var count int
	d.Pool().QueryRow("SELECT COUNT(*) FROM games WHERE game_report_id = 888").Scan(&count)
	if count != 0 {
		t.Error("game should have been rolled back")
	}
}

func TestQueryFactionStats(t *testing.T) {
	d := testDB(t)
	seedGames(t, d)

	// Default: ranked_only=true, min_games=1
	stats, total, err := QueryFactionStats(context.Background(), d.Pool(), QueryFilters{RankedOnly: true, MinGames: 1})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Errorf("total games = %d, want 2 (ranked only)", total)
	}
	if len(stats) != 2 {
		t.Fatalf("factions = %d, want 2", len(stats))
	}

	// All games
	stats2, total2, err := QueryFactionStats(context.Background(), d.Pool(), QueryFilters{RankedOnly: false, MinGames: 1})
	if err != nil {
		t.Fatal(err)
	}
	if total2 != 4 {
		t.Errorf("total games = %d, want 4", total2)
	}
	if len(stats2) != 3 {
		t.Fatalf("factions = %d, want 3", len(stats2))
	}
}

func TestQueryFactionStats_Empty(t *testing.T) {
	d := testDB(t)
	stats, total, err := QueryFactionStats(context.Background(), d.Pool(), QueryFilters{RankedOnly: false, MinGames: 1})
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 {
		t.Errorf("total = %d, want 0", total)
	}
	if stats == nil {
		t.Error("stats should be empty slice, not nil")
	}
}

func TestQueryFactionStats_MinGames(t *testing.T) {
	d := testDB(t)
	seedGames(t, d)
	stats, _, err := QueryFactionStats(context.Background(), d.Pool(), QueryFilters{RankedOnly: false, MinGames: 3})
	if err != nil {
		t.Fatal(err)
	}
	// Only fc_newantioch has 4 appearances, others have 2
	if len(stats) != 1 {
		t.Errorf("factions with >=3 games = %d, want 1", len(stats))
	}
}

func TestQueryUnitStats(t *testing.T) {
	d := testDB(t)
	seedGames(t, d)
	stats, err := QueryUnitStats(context.Background(), d.Pool(), QueryFilters{RankedOnly: true, MinGames: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) == 0 {
		t.Error("expected unit stats")
	}
}

func TestQueryScenarioStats(t *testing.T) {
	d := testDB(t)
	seedGames(t, d)
	stats, err := QueryScenarioStats(context.Background(), d.Pool(), QueryFilters{RankedOnly: false, MinGames: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) == 0 {
		t.Error("expected scenario stats")
	}
	// Should have entries for both scenarios
	scenarios := map[string]bool{}
	for _, s := range stats {
		scenarios[s.ScenarioID] = true
	}
	if !scenarios["sc_fieldsofglory"] || !scenarios["sc_ambush"] {
		t.Errorf("missing scenarios: %v", scenarios)
	}
}

func TestQueryWarbandCostStats(t *testing.T) {
	d := testDB(t)
	seedGames(t, d)
	stats, err := QueryWarbandCostStats(context.Background(), d.Pool(), QueryFilters{RankedOnly: false, MinGames: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) == 0 {
		t.Error("expected warband cost stats")
	}
}

func TestQueryDeedStats(t *testing.T) {
	d := testDB(t)
	seedGames(t, d)
	stats, err := QueryDeedStats(context.Background(), d.Pool(), QueryFilters{RankedOnly: false})
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 2 {
		t.Errorf("deed types = %d, want 2", len(stats))
	}
}

func TestQuerySummary(t *testing.T) {
	d := testDB(t)
	seedGames(t, d)
	s, err := QuerySummary(context.Background(), d.Pool())
	if err != nil {
		t.Fatal(err)
	}
	if s.TotalGames != 4 {
		t.Errorf("total = %d, want 4", s.TotalGames)
	}
	if s.RankedGames != 2 {
		t.Errorf("ranked = %d, want 2", s.RankedGames)
	}
	if s.FactionCount != 3 {
		t.Errorf("factions = %d, want 3", s.FactionCount)
	}
}

func TestQueryMeta(t *testing.T) {
	d := testDB(t)
	seedGames(t, d)
	m, err := QueryMeta(context.Background(), d.Pool())
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Factions) == 0 {
		t.Error("expected factions in meta")
	}
	if len(m.Scenarios) != 2 {
		t.Errorf("scenarios = %d, want 2", len(m.Scenarios))
	}
	if m.Factions == nil || m.Scenarios == nil {
		t.Error("nil slices in meta response")
	}
}

func TestQueryLatestIngestion_Empty(t *testing.T) {
	d := testDB(t)
	run, err := QueryLatestIngestion(context.Background(), d.Pool())
	if err != nil {
		t.Fatal(err)
	}
	if run != nil {
		t.Error("expected nil for empty ingestion log")
	}
}

func TestMaxGameReportID_Empty(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	conn, release, err := d.AcquireWriter(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	id, err := MaxGameReportID(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	if id != 0 {
		t.Errorf("max id = %d, want 0", id)
	}
}

func TestLastCompletedRunTime_Empty(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	conn, release, err := d.AcquireWriter(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ts, err := LastCompletedRunTime(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	if ts != nil {
		t.Errorf("expected nil, got %v", ts)
	}
}

func TestLastCompletedRunTime_WithData(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	conn, release, err := d.AcquireWriter(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	start := 1
	runID, err := StartIngestionRun(ctx, conn, "backfill", &start, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := CompleteIngestionRun(ctx, conn, runID, "completed", 5, 3, 10, 0, 1000, ""); err != nil {
		t.Fatal(err)
	}

	ts, err := LastCompletedRunTime(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	if ts == nil {
		t.Fatal("expected non-nil timestamp")
	}
	if ts.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

func TestLastCompletedRunTime_IgnoresRunning(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	conn, release, err := d.AcquireWriter(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	// Start a run but don't complete it
	start := 1
	_, err = StartIngestionRun(ctx, conn, "incremental", &start, nil)
	if err != nil {
		t.Fatal(err)
	}

	ts, err := LastCompletedRunTime(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	if ts != nil {
		t.Errorf("expected nil for running-only runs, got %v", ts)
	}
}

func TestAcquireWriter_TryAcquire(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()

	// Acquire first
	_, release, err := d.AcquireWriter(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Try should fail
	_, _, locked, _ := d.TryAcquireWriter(ctx)
	if locked {
		t.Error("TryAcquireWriter should return locked=false when mutex is held")
	}

	release()

	// Now should succeed
	_, release2, locked2, err := d.TryAcquireWriter(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !locked2 {
		t.Error("TryAcquireWriter should succeed after release")
	}
	release2()
}

func TestBackgroundCtx(t *testing.T) {
	ctx, cancel := BackgroundCtx(5 * time.Second)
	defer cancel()
	if ctx.Err() != nil {
		t.Error("context should not be cancelled yet")
	}
}

func TestNullJSON(t *testing.T) {
	if nullJSON(nil) != nil {
		t.Error("nil should return nil")
	}
	if nullJSON(json.RawMessage("null")) != nil {
		t.Error("\"null\" should return nil")
	}
	if nullJSON(json.RawMessage(`[1]`)) != `[1]` {
		t.Error("valid json should pass through")
	}
}

func TestNilIfEmpty(t *testing.T) {
	if nilIfEmpty("") != nil {
		t.Error("empty should return nil")
	}
	v := nilIfEmpty("test")
	if v == nil || *v != "test" {
		t.Error("non-empty should return pointer")
	}
}

func TestQueryFactionStats_DateFilter(t *testing.T) {
	d := testDB(t)
	seedGames(t, d)
	jan15 := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	stats, total, err := QueryFactionStats(context.Background(), d.Pool(), QueryFilters{RankedOnly: false, MinGames: 1, Since: &jan15})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Errorf("total after jan 15 = %d, want 2", total)
	}
	_ = stats
}

func TestQueryEquipmentStats(t *testing.T) {
	d := testDB(t)
	seedGames(t, d)
	stats, err := QueryEquipmentStats(context.Background(), d.Pool(), QueryFilters{RankedOnly: false}, "")
	if err != nil {
		t.Fatal(err)
	}
	// We seeded one unit with equipment
	if len(stats) == 0 {
		t.Error("expected equipment stats")
	}
	if stats[0].EquipmentID != "eq_sword" {
		t.Errorf("equipment_id = %q, want eq_sword", stats[0].EquipmentID)
	}
}

func TestQueryEquipmentStats_UnitFilter(t *testing.T) {
	d := testDB(t)
	seedGames(t, d)
	stats, err := QueryEquipmentStats(context.Background(), d.Pool(), QueryFilters{RankedOnly: false}, "md_heretic")
	if err != nil {
		t.Fatal(err)
	}
	// md_heretic has no equipment in seed data
	if len(stats) != 0 {
		t.Errorf("expected 0 equipment for md_heretic, got %d", len(stats))
	}
}

func TestStartAndCompleteIngestionRun(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	conn, release, err := d.AcquireWriter(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	start := 100
	runID, err := StartIngestionRun(ctx, conn, "backfill", &start, nil)
	if err != nil {
		t.Fatal(err)
	}
	if runID == 0 {
		t.Error("expected non-zero run ID")
	}

	err = CompleteIngestionRun(ctx, conn, runID, "completed", 10, 8, 100, 2, 5000, "test notes")
	if err != nil {
		t.Fatal(err)
	}

	run, err := QueryLatestIngestion(ctx, d.Pool())
	if err != nil {
		t.Fatal(err)
	}
	if run == nil {
		t.Fatal("expected ingestion run")
	}
	if run.Status != "completed" {
		t.Errorf("status = %q, want completed", run.Status)
	}
	if run.ReportsFound != 10 {
		t.Errorf("found = %d, want 10", run.ReportsFound)
	}
}

func TestQueryFactionStats_ScenarioFilter(t *testing.T) {
	d := testDB(t)
	seedGames(t, d)
	scenario := "sc_fieldsofglory"
	stats, total, err := QueryFactionStats(context.Background(), d.Pool(), QueryFilters{RankedOnly: false, MinGames: 1, Scenario: &scenario})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2 (only fieldsofglory)", total)
	}
	_ = stats
}

func TestQueryFactionStats_FactionFilter(t *testing.T) {
	d := testDB(t)
	seedGames(t, d)
	faction := "fc_newantioch"
	stats, _, err := QueryFactionStats(context.Background(), d.Pool(), QueryFilters{RankedOnly: false, MinGames: 1, Faction: &faction})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range stats {
		if s.BaseFaction != "fc_newantioch" {
			t.Errorf("unexpected faction %q in filtered results", s.BaseFaction)
		}
	}
}

func TestQueryFactionStats_UntilFilter(t *testing.T) {
	d := testDB(t)
	seedGames(t, d)
	jan15 := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	stats, total, err := QueryFactionStats(context.Background(), d.Pool(), QueryFilters{RankedOnly: false, MinGames: 1, Until: &jan15})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Errorf("total before jan 15 = %d, want 2", total)
	}
	_ = stats
}

func TestQueryUnitStats_FactionFilter(t *testing.T) {
	d := testDB(t)
	seedGames(t, d)
	faction := "fc_newantioch"
	stats, err := QueryUnitStats(context.Background(), d.Pool(), QueryFilters{RankedOnly: false, MinGames: 1, Faction: &faction})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range stats {
		if s.BaseFaction != "fc_newantioch" {
			t.Errorf("unexpected faction %q", s.BaseFaction)
		}
	}
}

func TestQueryScenarioStats_FactionFilter(t *testing.T) {
	d := testDB(t)
	seedGames(t, d)
	faction := "fc_newantioch"
	stats, err := QueryScenarioStats(context.Background(), d.Pool(), QueryFilters{RankedOnly: false, MinGames: 1, Faction: &faction})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range stats {
		if s.BaseFaction != "fc_newantioch" {
			t.Errorf("unexpected faction %q", s.BaseFaction)
		}
	}
}

func TestQueryWarbandCostStats_FactionFilter(t *testing.T) {
	d := testDB(t)
	seedGames(t, d)
	faction := "fc_newantioch"
	stats, err := QueryWarbandCostStats(context.Background(), d.Pool(), QueryFilters{RankedOnly: false, Faction: &faction})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range stats {
		if s.BaseFaction != "fc_newantioch" {
			t.Errorf("unexpected faction %q", s.BaseFaction)
		}
	}
}

func TestQuerySummary_Empty(t *testing.T) {
	d := testDB(t)
	s, err := QuerySummary(context.Background(), d.Pool())
	if err != nil {
		t.Fatal(err)
	}
	if s.TotalGames != 0 {
		t.Errorf("total = %d, want 0", s.TotalGames)
	}
	if s.EarliestGame != "" {
		t.Errorf("earliest = %q, want empty", s.EarliestGame)
	}
}

func TestQueryMeta_Empty(t *testing.T) {
	d := testDB(t)
	m, err := QueryMeta(context.Background(), d.Pool())
	if err != nil {
		t.Fatal(err)
	}
	if m.Factions == nil {
		t.Error("factions should be empty slice, not nil")
	}
	if m.Scenarios == nil {
		t.Error("scenarios should be empty slice, not nil")
	}
	if m.DateRange.Earliest != "" {
		t.Errorf("earliest = %q, want empty", m.DateRange.Earliest)
	}
}

func TestExecTx_CommitSuccess(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	conn, release, err := d.AcquireWriter(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	InsertRawReport(ctx, conn, 555)

	err = ExecTx(ctx, conn, func(tx *sql.Tx) error {
		return InsertGame(ctx, tx, models.Game{GameReportID: 555, ReportDate: time.Now(), ScenarioID: "sc_test", IsRanked: true, IsDraw: true})
	})
	if err != nil {
		t.Fatalf("ExecTx: %v", err)
	}

	var count int
	d.Pool().QueryRow("SELECT COUNT(*) FROM games WHERE game_report_id = 555").Scan(&count)
	if count != 1 {
		t.Errorf("game count = %d, want 1", count)
	}
}

func TestQueryFactionStats_WithData(t *testing.T) {
	d := testDB(t)
	seedGames(t, d)
	// Test with all filters set
	since := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
	scenario := "sc_fieldsofglory"
	faction := "fc_newantioch"
	stats, total, err := QueryFactionStats(context.Background(), d.Pool(), QueryFilters{
		RankedOnly: false, MinGames: 1,
		Since: &since, Until: &until, Scenario: &scenario, Faction: &faction,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = total
	for _, s := range stats {
		if s.BaseFaction != "fc_newantioch" {
			t.Errorf("unexpected faction %q", s.BaseFaction)
		}
	}
	_ = stats
}

func TestQueryUnitStats_Empty(t *testing.T) {
	d := testDB(t)
	stats, err := QueryUnitStats(context.Background(), d.Pool(), QueryFilters{RankedOnly: false, MinGames: 1})
	if err != nil {
		t.Fatal(err)
	}
	if stats == nil {
		t.Error("expected empty slice, not nil")
	}
}

func TestQueryEquipmentStats_Empty(t *testing.T) {
	d := testDB(t)
	stats, err := QueryEquipmentStats(context.Background(), d.Pool(), QueryFilters{RankedOnly: false}, "")
	if err != nil {
		t.Fatal(err)
	}
	if stats == nil {
		t.Error("expected empty slice, not nil")
	}
}

func TestQueryScenarioStats_Empty(t *testing.T) {
	d := testDB(t)
	stats, err := QueryScenarioStats(context.Background(), d.Pool(), QueryFilters{RankedOnly: false, MinGames: 1})
	if err != nil {
		t.Fatal(err)
	}
	if stats == nil {
		t.Error("expected empty slice, not nil")
	}
}

func TestQueryWarbandCostStats_Empty(t *testing.T) {
	d := testDB(t)
	stats, err := QueryWarbandCostStats(context.Background(), d.Pool(), QueryFilters{RankedOnly: false})
	if err != nil {
		t.Fatal(err)
	}
	if stats == nil {
		t.Error("expected empty slice, not nil")
	}
}

func TestQueryDeedStats_Empty(t *testing.T) {
	d := testDB(t)
	stats, err := QueryDeedStats(context.Background(), d.Pool(), QueryFilters{RankedOnly: false})
	if err != nil {
		t.Fatal(err)
	}
	if stats == nil {
		t.Error("expected empty slice, not nil")
	}
}

func TestQueryDeedStats_WithScenarioFilter(t *testing.T) {
	d := testDB(t)
	seedGames(t, d)
	scenario := "sc_fieldsofglory"
	stats, err := QueryDeedStats(context.Background(), d.Pool(), QueryFilters{RankedOnly: false, Scenario: &scenario})
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) == 0 {
		t.Error("expected deed stats for fieldsofglory")
	}
}

func TestQueryLatestIngestion_WithData(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	conn, release, err := d.AcquireWriter(ctx)
	if err != nil {
		t.Fatal(err)
	}
	start := 1
	runID, _ := StartIngestionRun(ctx, conn, "backfill", &start, nil)
	CompleteIngestionRun(ctx, conn, runID, "completed", 5, 3, 10, 0, 1000, "")
	release()

	run, err := QueryLatestIngestion(ctx, d.Pool())
	if err != nil {
		t.Fatal(err)
	}
	if run == nil {
		t.Fatal("expected run")
	}
	if run.CompletedAt == nil {
		t.Error("expected completed_at")
	}
}

func TestOpen_BadPath(t *testing.T) {
	_, err := Open(context.Background(), "/nonexistent/path/db.duckdb", Migrations{1: Migration001, 2: Migration002})
	if err == nil {
		t.Error("expected error for bad path")
	}
}

func TestMigrate_BadSQL(t *testing.T) {
	ctx := context.Background()
	d, err := Open(ctx, "", Migrations{1: Migration001, 2: Migration002})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	// Try to apply a bad migration (version 3 with invalid SQL)
	err = d.migrate(ctx, Migrations{3: "INVALID SQL STATEMENT !!!"})
	if err == nil {
		t.Error("expected error for bad migration SQL")
	}
}

func TestQueryOnClosedDB(t *testing.T) {
	d := testDB(t)
	pool := d.Pool()
	d.Close()

	_, _, err := QueryFactionStats(context.Background(), pool, QueryFilters{RankedOnly: false, MinGames: 1})
	if err == nil {
		t.Error("expected error on closed DB")
	}

	_, err = QueryUnitStats(context.Background(), pool, QueryFilters{RankedOnly: false, MinGames: 1})
	if err == nil {
		t.Error("expected error on closed DB")
	}

	_, err = QueryEquipmentStats(context.Background(), pool, QueryFilters{RankedOnly: false}, "")
	if err == nil {
		t.Error("expected error on closed DB")
	}

	_, err = QueryScenarioStats(context.Background(), pool, QueryFilters{RankedOnly: false, MinGames: 1})
	if err == nil {
		t.Error("expected error on closed DB")
	}

	_, err = QueryWarbandCostStats(context.Background(), pool, QueryFilters{RankedOnly: false})
	if err == nil {
		t.Error("expected error on closed DB")
	}

	_, err = QueryDeedStats(context.Background(), pool, QueryFilters{RankedOnly: false})
	if err == nil {
		t.Error("expected error on closed DB")
	}

	_, err = QuerySummary(context.Background(), pool)
	if err == nil {
		t.Error("expected error on closed DB")
	}

	_, err = QueryMeta(context.Background(), pool)
	if err == nil {
		t.Error("expected error on closed DB")
	}

	_, err = QueryLatestIngestion(context.Background(), pool)
	if err == nil {
		t.Error("expected error on closed DB")
	}
}
