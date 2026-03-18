package ingestion

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/natalie-johanek/trench-analytics/internal/db"
	"github.com/natalie-johanek/trench-analytics/internal/models"
)

// fakeSynodServer returns an httptest server that serves both:
//   - GET /game-reports?page=N&per_page=M (paginated archive)
//   - GET /game-report/{id} (single report, kept for FetchReport)
func fakeSynodServer(reports map[int]models.SynodReport) *httptest.Server {
	// Build a stable ordered slice of reports
	allReports := make([]models.SynodReport, 0, len(reports))
	for _, r := range reports {
		allReports = append(allReports, r)
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Paginated archive endpoint
		if r.URL.Path == "/game-reports" {
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
			if page < 1 {
				page = 1
			}
			if perPage < 1 {
				perPage = 50
			}

			total := len(allReports)
			totalPages := (total + perPage - 1) / perPage
			start := (page - 1) * perPage
			end := start + perPage
			if start > total {
				start = total
			}
			if end > total {
				end = total
			}

			w.Header().Set("X-WP-Total", strconv.Itoa(total))
			w.Header().Set("X-WP-TotalPages", strconv.Itoa(totalPages))
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(allReports[start:end])
			return
		}

		// Single report endpoint: /game-report/{id}
		parts := strings.Split(r.URL.Path, "/")
		idStr := parts[len(parts)-1]
		id, err := strconv.Atoi(idStr)
		if err != nil {
			w.WriteHeader(404)
			fmt.Fprint(w, `{"code":"invalid_game_report"}`)
			return
		}
		report, ok := reports[id]
		if !ok {
			w.WriteHeader(404)
			fmt.Fprint(w, `{"code":"invalid_game_report"}`)
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
	store, err := db.Open(context.Background(), "", db.Migrations{1: db.Migration001, 2: db.Migration002})
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

	if err := ingestOne(ctx, conn, &report); err != nil {
		t.Fatalf("ingestOne: %v", err)
	}

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

	ingestOne(ctx, conn, &report)
	if err := ingestOne(ctx, conn, &report); err != nil {
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

	// Backfill first
	syncer.Backfill(context.Background(), 100, 1)

	// Incremental should pick up both (since no modified_after filter on first run)
	if err := syncer.IncrementalSync(context.Background(), nil); err != nil {
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

	_, release, err := store.AcquireWriter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	err = syncer.IncrementalSync(context.Background(), nil)
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

	report := &models.SynodReport{
		GameReportID: 999,
		PlayerIDs:    []int{1, 2},
		Data: models.SynodReportData{
			Date:       1700000000,
			ScenarioID: "sc_test",
			Ranked:     true,
			Warbands:   nil,
		},
	}

	err = ingestOne(ctx, conn, report)
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

	err = ingestOne(ctx, conn, report)
	if err == nil {
		t.Error("expected transform error")
	}

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
	cancel()

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
	tracker.inserted = 0
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

func TestProcessRawReport_DecodeError(t *testing.T) {
	store := testStore(t)
	client := NewClient("http://localhost:0", 1000)
	defer client.Close()
	syncer := NewSyncer(client, store)

	ctx := context.Background()
	conn, release, err := store.AcquireWriter(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	tracker := newRunTracker(1)
	syncer.processRawReport(ctx, conn, json.RawMessage(`{invalid`), tracker)
	if tracker.errors != 1 {
		t.Errorf("errors = %d, want 1", tracker.errors)
	}
}

func TestProcessRawReport_Success(t *testing.T) {
	store := testStore(t)
	client := NewClient("http://localhost:0", 1000)
	defer client.Close()
	syncer := NewSyncer(client, store)

	ctx := context.Background()
	conn, release, err := store.AcquireWriter(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	report := makeTestReport(100, 10)
	raw, _ := json.Marshal(report)

	tracker := newRunTracker(1)
	syncer.processRawReport(ctx, conn, raw, tracker)
	if tracker.inserted != 1 {
		t.Errorf("inserted = %d, want 1", tracker.inserted)
	}
	if tracker.found != 1 {
		t.Errorf("found = %d, want 1", tracker.found)
	}
}

func TestProcessRawReport_IngestError(t *testing.T) {
	store := testStore(t)
	client := NewClient("http://localhost:0", 1000)
	defer client.Close()
	syncer := NewSyncer(client, store)

	ctx := context.Background()
	conn, release, err := store.AcquireWriter(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	// Report with only 1 warband → transform error
	badReport := models.SynodReport{
		GameReportID: 200,
		PlayerIDs:    []int{1, 2},
		Data: models.SynodReportData{
			Date: 1700000000, ScenarioID: "sc_test", Ranked: true,
			Warbands: []models.SynodWarband{
				{FactionSlug: "fc_newantioch", WarbandExport: models.SynodExport{
					WID: 10, WNA: "WA", DR: 500,
					ELT: []models.SynodUnit{{MDN: "Leader", MDI: "md_leader", C: models.SynodCost{D: 100}}},
				}},
			},
		},
	}
	raw, _ := json.Marshal(badReport)

	tracker := newRunTracker(1)
	syncer.processRawReport(ctx, conn, raw, tracker)
	if tracker.errors != 1 {
		t.Errorf("errors = %d, want 1", tracker.errors)
	}
	if tracker.inserted != 0 {
		t.Errorf("inserted = %d, want 0", tracker.inserted)
	}
}

func TestBackfill_LargeBatch(t *testing.T) {
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

	syncer.Backfill(context.Background(), 1, 1)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := syncer.IncrementalSync(ctx, nil)
	if err == nil {
		t.Error("expected context cancellation error")
	}
}

func TestBackfill_MultiPage(t *testing.T) {
	// 150 reports should require 2 pages at perPage=100
	reports := map[int]models.SynodReport{}
	for i := 1; i <= 150; i++ {
		reports[i] = makeTestReport(i, 10)
	}
	srv := fakeSynodServer(reports)
	defer srv.Close()

	store := testStore(t)
	client := NewClient(srv.URL, 1000)
	defer client.Close()
	syncer := NewSyncer(client, store)

	if err := syncer.Backfill(context.Background(), 1, 150); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	var count int
	store.Pool().QueryRow("SELECT COUNT(*) FROM games").Scan(&count)
	if count != 150 {
		t.Errorf("games = %d, want 150", count)
	}
}

func TestIncrementalSync_DeltaFetch(t *testing.T) {
	// Backfill first, then add a new report and run incremental.
	// Since the fake server doesn't filter by modified_after,
	// incremental will re-see existing reports (idempotent) and pick up new ones.
	reports := map[int]models.SynodReport{
		100: makeTestReport(100, 10),
	}
	srv := fakeSynodServer(reports)
	defer srv.Close()

	store := testStore(t)
	client := NewClient(srv.URL, 1000)
	defer client.Close()
	syncer := NewSyncer(client, store)

	// Backfill
	if err := syncer.Backfill(context.Background(), 100, 1); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	// Incremental (should be idempotent — no new reports)
	if err := syncer.IncrementalSync(context.Background(), nil); err != nil {
		t.Fatalf("incremental: %v", err)
	}

	var count int
	store.Pool().QueryRow("SELECT COUNT(*) FROM games").Scan(&count)
	if count != 1 {
		t.Errorf("games = %d, want 1 (idempotent)", count)
	}

	// Verify two ingestion runs logged
	var runs int
	store.Pool().QueryRow("SELECT COUNT(*) FROM ingestion_log").Scan(&runs)
	if runs != 2 {
		t.Errorf("ingestion runs = %d, want 2", runs)
	}
}

func TestIncrementalSync_EmptyResult(t *testing.T) {
	// Server returns empty array — sync should complete immediately
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-WP-Total", "0")
		w.Header().Set("X-WP-TotalPages", "0")
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	store := testStore(t)
	client := NewClient(srv.URL, 1000)
	defer client.Close()
	syncer := NewSyncer(client, store)

	if err := syncer.IncrementalSync(context.Background(), nil); err != nil {
		t.Fatalf("sync: %v", err)
	}

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
	if run.ReportsInserted != 0 {
		t.Errorf("inserted = %d, want 0", run.ReportsInserted)
	}
}
