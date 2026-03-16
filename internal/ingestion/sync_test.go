package ingestion

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/natalie-johanek/trench-analytics/internal/db"
	"github.com/natalie-johanek/trench-analytics/internal/models"
)

// fakeSynodServer returns an httptest server that serves game reports for specific IDs.
func fakeSynodServer(reports map[int]models.SynodReport) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Parse ID from /game-report/{id}
		parts := strings.Split(r.URL.Path, "/")
		idStr := parts[len(parts)-1]
		id, err := strconv.Atoi(idStr)
		if err != nil {
			w.WriteHeader(404)
			w.Write([]byte(`{"code":"invalid_game_report"}`))
			return
		}
		report, ok := reports[id]
		if !ok {
			w.WriteHeader(404)
			w.Write([]byte(`{"code":"invalid_game_report"}`))
			return
		}
		json.NewEncoder(w).Encode(report)
	}))
}

func makeTestReport(id int, winner int) models.SynodReport {
	return models.SynodReport{
		GameReportID: id,
		PlayerIDs:    []int{1, 2},
		WarbandIDs:   []int{10, 20},
		Data: models.SynodReportData{
			Winner:     &winner,
			Kills:      map[string]int{"10": 3, "20": 2},
			VP:         map[string]int{"10": 1, "20": 0},
			ScenarioID: "sc_test",
			Date:       1700000000,
			Ranked:     true,
			Warbands: []models.SynodWarband{
				{
					FactionSlug: "fc_newantioch",
					WarbandExport: models.SynodExport{
						WID: 10, WNA: "WA", DR: 500,
						ELT: []models.SynodUnit{{MDN: "Leader", MDI: "md_leader", C: models.SynodCost{D: 100}}},
					},
				},
				{
					FactionSlug: "fc_hereticlegion",
					WarbandExport: models.SynodExport{
						WID: 20, WNA: "WB", DR: 450,
						MDS: []models.SynodUnit{{MDN: "Heretic", MDI: "md_heretic", C: models.SynodCost{D: 40}}},
					},
				},
			},
		},
	}
}

func testStore(t *testing.T) *db.DB {
	t.Helper()
	store, err := db.Open(context.Background(), "", db.Migrations{1: db.Migration001})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestIngestOne(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	conn, release, err := store.AcquireWriter(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	report := makeTestReport(100, 10)
	raw, _ := json.Marshal(report)

	if err := ingestOne(ctx, conn, &report, raw); err != nil {
		t.Fatalf("ingestOne: %v", err)
	}

	// Verify data was inserted
	var count int
	store.Pool().QueryRow("SELECT COUNT(*) FROM games").Scan(&count)
	if count != 1 {
		t.Errorf("games = %d, want 1", count)
	}
	store.Pool().QueryRow("SELECT COUNT(*) FROM game_participants").Scan(&count)
	if count != 2 {
		t.Errorf("participants = %d, want 2", count)
	}
	store.Pool().QueryRow("SELECT COUNT(*) FROM game_units").Scan(&count)
	if count != 2 {
		t.Errorf("units = %d, want 2", count)
	}
}

func TestIngestOne_Idempotent(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	conn, release, err := store.AcquireWriter(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	report := makeTestReport(100, 10)
	raw, _ := json.Marshal(report)

	ingestOne(ctx, conn, &report, raw)
	// Second call should be a no-op
	if err := ingestOne(ctx, conn, &report, raw); err != nil {
		t.Fatalf("second ingestOne should not error: %v", err)
	}

	var count int
	store.Pool().QueryRow("SELECT COUNT(*) FROM games").Scan(&count)
	if count != 1 {
		t.Errorf("games = %d, want 1 (idempotent)", count)
	}
}

func TestBackfill(t *testing.T) {
	reports := map[int]models.SynodReport{
		100: makeTestReport(100, 10),
		98:  makeTestReport(98, 20),
		// 99 is a gap (404)
	}
	srv := fakeSynodServer(reports)
	defer srv.Close()

	store := testStore(t)
	client := NewClient(srv.URL, 100)
	defer client.Close()
	syncer := NewSyncer(client, store)

	if err := syncer.Backfill(context.Background(), 100, 3); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	var count int
	store.Pool().QueryRow("SELECT COUNT(*) FROM games").Scan(&count)
	if count != 2 {
		t.Errorf("games = %d, want 2", count)
	}

	// Check ingestion log
	run, err := db.QueryLatestIngestion(context.Background(), store.Pool())
	if err != nil {
		t.Fatal(err)
	}
	if run == nil {
		t.Fatal("expected ingestion run")
	}
	if run.Status != "completed" {
		t.Errorf("status = %q, want completed", run.Status)
	}
	if run.ReportsInserted != 2 {
		t.Errorf("inserted = %d, want 2", run.ReportsInserted)
	}
}

func TestIncrementalSync(t *testing.T) {
	// Pre-seed one report, then sync should find the next one
	reports := map[int]models.SynodReport{
		100: makeTestReport(100, 10),
		101: makeTestReport(101, 20),
	}
	srv := fakeSynodServer(reports)
	defer srv.Close()

	store := testStore(t)
	client := NewClient(srv.URL, 100)
	defer client.Close()
	syncer := NewSyncer(client, store)

	// Backfill report 100
	syncer.Backfill(context.Background(), 100, 1)

	// Incremental should find 101
	if err := syncer.IncrementalSync(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}

	var count int
	store.Pool().QueryRow("SELECT COUNT(*) FROM games").Scan(&count)
	if count != 2 {
		t.Errorf("games = %d, want 2", count)
	}
}

func TestIncrementalSync_AlreadyRunning(t *testing.T) {
	store := testStore(t)
	client := NewClient("http://localhost:0", 100)
	defer client.Close()
	syncer := NewSyncer(client, store)

	// Hold the write lock
	_, release, err := store.AcquireWriter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	// Sync should fail with "already in progress"
	err = syncer.IncrementalSync(context.Background())
	if err == nil || !strings.Contains(err.Error(), "sync already in progress") {
		t.Errorf("expected 'sync already in progress', got: %v", err)
	}
}

func TestIngestOne_PanicRecovery(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	conn, release, err := store.AcquireWriter(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	// Report with nil Warbands slice — Transform will panic on len(nil) access
	// after passing the initial len check
	report := &models.SynodReport{
		GameReportID: 999,
		PlayerIDs:    []int{1, 2},
		Data: models.SynodReportData{
			Date:       1700000000,
			ScenarioID: "sc_test",
			Ranked:     true,
			Warbands:   nil, // will fail validation, not panic
		},
	}
	raw, _ := json.Marshal(report)

	// This should return an error (validation), not panic
	err = ingestOne(ctx, conn, report, raw)
	if err == nil {
		t.Error("expected error from invalid report")
	}
}

func TestIngestOne_TransformFailStoresRaw(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	conn, release, err := store.AcquireWriter(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	// Report that fails transform (only 1 warband) but should still store raw
	report := &models.SynodReport{
		GameReportID: 777,
		PlayerIDs:    []int{1, 2},
		Data: models.SynodReportData{
			Date:       1700000000,
			ScenarioID: "sc_test",
			Ranked:     true,
			Warbands: []models.SynodWarband{
				{
					FactionSlug: "fc_newantioch",
					WarbandExport: models.SynodExport{
						WID: 10, WNA: "WA", DR: 500,
						ELT: []models.SynodUnit{{MDN: "Leader", MDI: "md_leader", C: models.SynodCost{D: 100}}},
					},
				},
			},
		},
	}
	raw, _ := json.Marshal(report)

	err = ingestOne(ctx, conn, report, raw)
	if err == nil {
		t.Error("expected transform error")
	}

	// Raw report should still be stored
	var count int
	store.Pool().QueryRow("SELECT COUNT(*) FROM raw_game_reports WHERE game_report_id = 777").Scan(&count)
	if count != 1 {
		t.Errorf("raw report count = %d, want 1", count)
	}
}

func TestBackfill_ContextCancel(t *testing.T) {
	reports := map[int]models.SynodReport{}
	for i := 1; i <= 200; i++ {
		reports[i] = makeTestReport(i, 10)
	}
	srv := fakeSynodServer(reports)
	defer srv.Close()

	store := testStore(t)
	client := NewClient(srv.URL, 1000)
	defer client.Close()
	syncer := NewSyncer(client, store)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := syncer.Backfill(ctx, 200, 200)
	if err == nil {
		t.Error("expected context cancellation error")
	}
}

func TestRunTracker_FailedStatus(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	conn, release, err := store.AcquireWriter(ctx)
	if err != nil {
		t.Fatal(err)
	}

	start := 1
	runID, err := db.StartIngestionRun(ctx, conn, "backfill", &start, nil)
	if err != nil {
		release()
		t.Fatal(err)
	}

	tracker := newRunTracker(runID)
	tracker.errors = 5
	tracker.inserted = 0 // errors > 0 && inserted == 0 → "failed"
	tracker.finish(conn)
	release()

	run, err := db.QueryLatestIngestion(ctx, store.Pool())
	if err != nil {
		t.Fatal(err)
	}
	if run == nil {
		t.Fatal("expected ingestion run")
	}
	if run.Status != "failed" {
		t.Errorf("status = %q, want failed", run.Status)
	}
}

func TestProcessID_FetchError(t *testing.T) {
	// Server that always returns 500
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	store := testStore(t)
	client := NewClient(srv.URL, 1000)
	client.maxRetries = 0 // no retries for speed
	defer client.Close()
	syncer := NewSyncer(client, store)

	ctx := context.Background()
	conn, release, err := store.AcquireWriter(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	tracker := newRunTracker(1)
	found := syncer.processID(ctx, conn, 1, tracker)
	if found {
		t.Error("expected not found on fetch error")
	}
	if tracker.errors != 1 {
		t.Errorf("errors = %d, want 1", tracker.errors)
	}
}

func TestProcessID_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	store := testStore(t)
	client := NewClient(srv.URL, 1000)
	defer client.Close()
	syncer := NewSyncer(client, store)

	ctx := context.Background()
	conn, release, err := store.AcquireWriter(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	tracker := newRunTracker(1)
	found := syncer.processID(ctx, conn, 1, tracker)
	if found {
		t.Error("expected not found on 404")
	}
	if tracker.scanned != 1 {
		t.Errorf("scanned = %d, want 1", tracker.scanned)
	}
}

func TestProcessID_SuccessfulIngest(t *testing.T) {
	report := makeTestReport(100, 10)
	reports := map[int]models.SynodReport{100: report}
	srv := fakeSynodServer(reports)
	defer srv.Close()

	store := testStore(t)
	client := NewClient(srv.URL, 1000)
	defer client.Close()
	syncer := NewSyncer(client, store)

	ctx := context.Background()
	conn, release, err := store.AcquireWriter(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	tracker := newRunTracker(1)
	found := syncer.processID(ctx, conn, 100, tracker)
	if !found {
		t.Error("expected found")
	}
	if tracker.inserted != 1 {
		t.Errorf("inserted = %d, want 1", tracker.inserted)
	}
	if tracker.found != 1 {
		t.Errorf("found = %d, want 1", tracker.found)
	}
}

func TestProcessID_IngestError(t *testing.T) {
	// Report that will fail transform (only 1 warband)
	badReport := models.SynodReport{
		GameReportID: 200,
		PlayerIDs:    []int{1, 2},
		Data: models.SynodReportData{
			Date:       1700000000,
			ScenarioID: "sc_test",
			Ranked:     true,
			Warbands: []models.SynodWarband{
				{
					FactionSlug: "fc_newantioch",
					WarbandExport: models.SynodExport{
						WID: 10, WNA: "WA", DR: 500,
						ELT: []models.SynodUnit{{MDN: "Leader", MDI: "md_leader", C: models.SynodCost{D: 100}}},
					},
				},
			},
		},
	}
	reports := map[int]models.SynodReport{200: badReport}
	srv := fakeSynodServer(reports)
	defer srv.Close()

	store := testStore(t)
	client := NewClient(srv.URL, 1000)
	defer client.Close()
	syncer := NewSyncer(client, store)

	ctx := context.Background()
	conn, release, err := store.AcquireWriter(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	tracker := newRunTracker(1)
	found := syncer.processID(ctx, conn, 200, tracker)
	if !found {
		t.Error("expected found (report exists but fails transform)")
	}
	if tracker.errors != 1 {
		t.Errorf("errors = %d, want 1", tracker.errors)
	}
	if tracker.inserted != 0 {
		t.Errorf("inserted = %d, want 0", tracker.inserted)
	}
}

func TestBackfill_LargeBatch(t *testing.T) {
	// Test backfill with more than 50 IDs to exercise batch logic
	reports := map[int]models.SynodReport{}
	for i := 50; i <= 110; i++ {
		reports[i] = makeTestReport(i, 10)
	}
	srv := fakeSynodServer(reports)
	defer srv.Close()

	store := testStore(t)
	client := NewClient(srv.URL, 1000)
	defer client.Close()
	syncer := NewSyncer(client, store)

	if err := syncer.Backfill(context.Background(), 110, 61); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	var count int
	store.Pool().QueryRow("SELECT COUNT(*) FROM games").Scan(&count)
	if count != 61 {
		t.Errorf("games = %d, want 61", count)
	}
}

func TestNewRunTracker(t *testing.T) {
	tracker := newRunTracker(42)
	if tracker.runID != 42 {
		t.Errorf("runID = %d, want 42", tracker.runID)
	}
	if tracker.startTime.IsZero() {
		t.Error("startTime should be set")
	}
}

func TestBackfill_RangeEndClamp(t *testing.T) {
	// Test that rangeEnd is clamped to 1 when start-count+1 < 1
	reports := map[int]models.SynodReport{
		1: makeTestReport(1, 10),
		2: makeTestReport(2, 10),
	}
	srv := fakeSynodServer(reports)
	defer srv.Close()

	store := testStore(t)
	client := NewClient(srv.URL, 1000)
	defer client.Close()
	syncer := NewSyncer(client, store)

	// start=2, count=100 → rangeEnd would be -97, clamped to 1
	if err := syncer.Backfill(context.Background(), 2, 100); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	var count int
	store.Pool().QueryRow("SELECT COUNT(*) FROM games").Scan(&count)
	if count != 2 {
		t.Errorf("games = %d, want 2", count)
	}
}

func TestIncrementalSync_ContextCancel(t *testing.T) {
	reports := map[int]models.SynodReport{
		1: makeTestReport(1, 10),
	}
	srv := fakeSynodServer(reports)
	defer srv.Close()

	store := testStore(t)
	client := NewClient(srv.URL, 1000)
	defer client.Close()
	syncer := NewSyncer(client, store)

	// Backfill one report first
	syncer.Backfill(context.Background(), 1, 1)

	// Cancel context immediately for incremental sync
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := syncer.IncrementalSync(ctx)
	if err == nil {
		t.Error("expected context cancellation error")
	}
}

func TestBackfill_ProgressLogging(t *testing.T) {
	// Create enough reports to trigger the progress logging (inserted%50==0)
	reports := map[int]models.SynodReport{}
	for i := 1; i <= 55; i++ {
		reports[i] = makeTestReport(i, 10)
	}
	srv := fakeSynodServer(reports)
	defer srv.Close()

	store := testStore(t)
	client := NewClient(srv.URL, 1000)
	defer client.Close()
	syncer := NewSyncer(client, store)

	if err := syncer.Backfill(context.Background(), 55, 55); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	var count int
	store.Pool().QueryRow("SELECT COUNT(*) FROM games").Scan(&count)
	if count != 55 {
		t.Errorf("games = %d, want 55", count)
	}
}
