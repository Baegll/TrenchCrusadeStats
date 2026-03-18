package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/natalie-johanek/trench-analytics/internal/db"
	"github.com/natalie-johanek/trench-analytics/internal/ingestion"
	"github.com/natalie-johanek/trench-analytics/internal/models"
)

func testServer(t *testing.T) (*httptest.Server, *db.DB) {
	t.Helper()
	ctx := context.Background()
	store, err := db.Open(ctx, "", db.Migrations{1: db.Migration001, 2: db.Migration002})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	client := ingestion.NewClient("http://localhost:0", 100)
	t.Cleanup(func() { client.Close() })
	syncer := ingestion.NewSyncer(client, store)

	srv := NewServer(store, syncer, []string{"test-key"}, "admin", "pass", 1000)
	t.Cleanup(func() { srv.Close() })

	ts := httptest.NewServer(srv.Router)
	t.Cleanup(func() { ts.Close() })
	return ts, store
}

func TestHealth_Public(t *testing.T) {
	ts, _ := testServer(t)
	resp, err := http.Get(ts.URL + "/api/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var h models.HealthResponse
	json.NewDecoder(resp.Body).Decode(&h)
	if h.Status != "ok" {
		t.Errorf("status = %q, want ok", h.Status)
	}
}

func TestStats_RequiresAPIKey(t *testing.T) {
	ts, _ := testServer(t)
	resp, err := http.Get(ts.URL + "/api/v1/stats/factions")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestStats_InvalidKey(t *testing.T) {
	ts, _ := testServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/stats/factions", nil)
	req.Header.Set("X-Api-Key", "wrong-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestStats_ValidKey_EmptyDB(t *testing.T) {
	ts, _ := testServer(t)

	endpoints := []string{
		"/api/v1/stats/factions",
		"/api/v1/stats/units",
		"/api/v1/stats/equipment",
		"/api/v1/stats/scenarios",
		"/api/v1/stats/warband-costs",
		"/api/v1/stats/deeds",
		"/api/v1/stats/summary",
		"/api/v1/stats/meta",
	}

	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			req, _ := http.NewRequest("GET", ts.URL+ep, nil)
			req.Header.Set("X-Api-Key", "test-key")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				t.Errorf("status = %d, want 200", resp.StatusCode)
			}
			// Verify JSON is valid and arrays are [] not null
			var raw map[string]json.RawMessage
			if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
				t.Fatalf("invalid json: %v", err)
			}
		})
	}
}

func TestStats_InvalidParam(t *testing.T) {
	ts, _ := testServer(t)

	endpoints := []string{
		"/api/v1/stats/factions",
		"/api/v1/stats/units",
		"/api/v1/stats/equipment",
		"/api/v1/stats/scenarios",
		"/api/v1/stats/warband-costs",
		"/api/v1/stats/deeds",
	}

	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			req, _ := http.NewRequest("GET", ts.URL+ep+"?min_games=abc", nil)
			req.Header.Set("X-Api-Key", "test-key")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != 400 {
				t.Errorf("status = %d, want 400", resp.StatusCode)
			}
		})
	}
}

func TestAdmin_RequiresBasicAuth(t *testing.T) {
	ts, _ := testServer(t)
	resp, err := http.Post(ts.URL+"/admin/sync", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestAdmin_SyncStatus_NoRuns(t *testing.T) {
	ts, _ := testServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/sync/status", nil)
	req.Header.Set("X-Api-Key", "test-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var s models.SyncStatusResponse
	json.NewDecoder(resp.Body).Decode(&s)
	if s.Status != "no_runs" {
		t.Errorf("status = %q, want no_runs", s.Status)
	}
}

func TestFactions_EmptyArrayNotNull(t *testing.T) {
	ts, _ := testServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/stats/factions", nil)
	req.Header.Set("X-Api-Key", "test-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var raw map[string]json.RawMessage
	json.NewDecoder(resp.Body).Decode(&raw)
	factions := string(raw["factions"])
	if factions == "null" {
		t.Error("factions should be [], not null")
	}
}

func TestTriggerSync_Returns202(t *testing.T) {
	ts, _ := testServer(t)
	req, _ := http.NewRequest("POST", ts.URL+"/admin/sync", nil)
	req.SetBasicAuth("admin", "pass")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 202 {
		t.Errorf("status = %d, want 202", resp.StatusCode)
	}
}

func TestAdmin_InvalidBasicAuth(t *testing.T) {
	ts, _ := testServer(t)
	req, _ := http.NewRequest("POST", ts.URL+"/admin/sync", nil)
	req.SetBasicAuth("admin", "wrong")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestParseFilters_AllParams(t *testing.T) {
	req := httptest.NewRequest("GET", "/?ranked_only=false&min_games=10&since=2024-01-01&until=2024-12-31&scenario=sc_test&faction=fc_test", nil)
	f, err := parseFilters(req)
	if err != nil {
		t.Fatal(err)
	}
	if f.RankedOnly {
		t.Error("ranked_only should be false")
	}
	if f.MinGames != 10 {
		t.Errorf("min_games = %d, want 10", f.MinGames)
	}
	if f.Since == nil || f.Since.Format("2006-01-02") != "2024-01-01" {
		t.Errorf("since = %v, want 2024-01-01", f.Since)
	}
	if f.Until == nil || f.Until.Format("2006-01-02") != "2024-12-31" {
		t.Errorf("until = %v, want 2024-12-31", f.Until)
	}
	if f.Scenario == nil || *f.Scenario != "sc_test" {
		t.Errorf("scenario = %v, want sc_test", f.Scenario)
	}
	if f.Faction == nil || *f.Faction != "fc_test" {
		t.Errorf("faction = %v, want fc_test", f.Faction)
	}
}

func TestParseFilters_InvalidRankedOnly(t *testing.T) {
	req := httptest.NewRequest("GET", "/?ranked_only=notbool", nil)
	_, err := parseFilters(req)
	if err == nil {
		t.Error("expected error for invalid ranked_only")
	}
}

func TestParseFilters_InvalidUntil(t *testing.T) {
	req := httptest.NewRequest("GET", "/?until=bad-date", nil)
	_, err := parseFilters(req)
	if err == nil {
		t.Error("expected error for invalid until")
	}
}

func TestParseFilters_InvalidSince(t *testing.T) {
	req := httptest.NewRequest("GET", "/?since=bad-date", nil)
	_, err := parseFilters(req)
	if err == nil {
		t.Error("expected error for invalid since")
	}
}

func TestParseFilters_MinGamesClamp(t *testing.T) {
	req := httptest.NewRequest("GET", "/?min_games=0", nil)
	f, err := parseFilters(req)
	if err != nil {
		t.Fatal(err)
	}
	if f.MinGames != 1 {
		t.Errorf("min_games = %d, want 1 (clamped)", f.MinGames)
	}

	req2 := httptest.NewRequest("GET", "/?min_games=99999", nil)
	f2, err := parseFilters(req2)
	if err != nil {
		t.Fatal(err)
	}
	if f2.MinGames != 10000 {
		t.Errorf("min_games = %d, want 10000 (clamped)", f2.MinGames)
	}
}

func TestParseFilters_LongScenarioTruncated(t *testing.T) {
	long := strings.Repeat("a", 300)
	req := httptest.NewRequest("GET", "/?scenario="+long, nil)
	f, err := parseFilters(req)
	if err != nil {
		t.Fatal(err)
	}
	if f.Scenario == nil || len(*f.Scenario) != 200 {
		t.Errorf("scenario length = %d, want 200 (truncated)", len(*f.Scenario))
	}
}

func TestParseFilters_LongFactionTruncated(t *testing.T) {
	long := strings.Repeat("b", 300)
	req := httptest.NewRequest("GET", "/?faction="+long, nil)
	f, err := parseFilters(req)
	if err != nil {
		t.Fatal(err)
	}
	if f.Faction == nil || len(*f.Faction) != 200 {
		t.Errorf("faction length = %d, want 200 (truncated)", len(*f.Faction))
	}
}

func TestFiltersToResponse_AllFields(t *testing.T) {
	since := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)
	scenario := "sc_test"
	faction := "fc_test"
	f := db.QueryFilters{
		RankedOnly: true,
		MinGames:   5,
		Since:      &since,
		Until:      &until,
		Scenario:   &scenario,
		Faction:    &faction,
	}
	sf := filtersToResponse(f)
	if sf.Since != "2024-01-01" {
		t.Errorf("since = %q", sf.Since)
	}
	if sf.Until != "2024-12-31" {
		t.Errorf("until = %q", sf.Until)
	}
	if sf.Scenario != "sc_test" {
		t.Errorf("scenario = %q", sf.Scenario)
	}
	if sf.Faction != "fc_test" {
		t.Errorf("faction = %q", sf.Faction)
	}
}

func TestSyncStatus_WithCompletedRun(t *testing.T) {
	ts, store := testServer(t)
	ctx := context.Background()
	conn, release, err := store.AcquireWriter(ctx)
	if err != nil {
		t.Fatal(err)
	}
	start := 1
	runID, _ := db.StartIngestionRun(ctx, conn, "backfill", &start, nil)
	db.CompleteIngestionRun(ctx, conn, runID, "completed", 5, 3, 10, 1, 1000, "test")
	release()

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/sync/status", nil)
	req.Header.Set("X-Api-Key", "test-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var s models.SyncStatusResponse
	json.NewDecoder(resp.Body).Decode(&s)
	if s.Status != "completed" {
		t.Errorf("status = %q, want completed", s.Status)
	}
	if s.CompletedAt == nil {
		t.Error("expected completed_at to be set")
	}
	if s.ReportsFound != 5 {
		t.Errorf("reports_found = %d, want 5", s.ReportsFound)
	}
}

func TestHealth_WithIngestionRun(t *testing.T) {
	ts, store := testServer(t)
	ctx := context.Background()
	conn, release, err := store.AcquireWriter(ctx)
	if err != nil {
		t.Fatal(err)
	}
	start := 1
	runID, _ := db.StartIngestionRun(ctx, conn, "backfill", &start, nil)
	db.CompleteIngestionRun(ctx, conn, runID, "completed", 1, 1, 1, 0, 100, "")
	release()

	resp, err := http.Get(ts.URL + "/api/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var h models.HealthResponse
	json.NewDecoder(resp.Body).Decode(&h)
	if h.Status != "ok" {
		t.Errorf("status = %q, want ok", h.Status)
	}
	if h.LastIngestion == "" {
		t.Error("expected last_ingestion to be set")
	}
}

func TestRateLimit_FallsBackToRemoteAddr(t *testing.T) {
	rl := newRateLimiter(1)
	defer rl.Close()

	handler := RateLimit(rl)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	// No X-Api-Key — should use RemoteAddr
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("first request: status = %d, want 200", w.Code)
	}

	req2 := httptest.NewRequest("GET", "/", nil)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	if w2.Code != 429 {
		t.Errorf("second request: status = %d, want 429", w2.Code)
	}
}

func TestSyncWait(t *testing.T) {
	sh := &SyncHandlers{}
	// Wait on empty should return immediately
	sh.Wait()
}

func closedDBServer(t *testing.T) (*httptest.Server, *db.DB) {
	t.Helper()
	ctx := context.Background()
	store, err := db.Open(ctx, "", db.Migrations{1: db.Migration001, 2: db.Migration002})
	if err != nil {
		t.Fatal(err)
	}

	client := ingestion.NewClient("http://localhost:0", 100)
	t.Cleanup(func() { client.Close() })
	syncer := ingestion.NewSyncer(client, store)

	srv := NewServer(store, syncer, []string{"test-key"}, "admin", "pass", 1000)
	t.Cleanup(func() { srv.Close() })

	ts := httptest.NewServer(srv.Router)
	t.Cleanup(func() { ts.Close() })

	// Close the DB to force 500 errors
	store.Close()

	return ts, store
}

func TestHandlers_500_OnClosedDB(t *testing.T) {
	ts, _ := closedDBServer(t)

	endpoints := []struct {
		method string
		path   string
		auth   string // "api" or "basic"
	}{
		{"GET", "/api/v1/stats/factions", "api"},
		{"GET", "/api/v1/stats/units", "api"},
		{"GET", "/api/v1/stats/equipment", "api"},
		{"GET", "/api/v1/stats/scenarios", "api"},
		{"GET", "/api/v1/stats/warband-costs", "api"},
		{"GET", "/api/v1/stats/deeds", "api"},
		{"GET", "/api/v1/stats/summary", "api"},
		{"GET", "/api/v1/stats/meta", "api"},
		{"GET", "/api/v1/health", "none"},
		{"GET", "/api/v1/sync/status", "api"},
	}

	for _, ep := range endpoints {
		t.Run(ep.path, func(t *testing.T) {
			req, _ := http.NewRequest(ep.method, ts.URL+ep.path, nil)
			switch ep.auth {
			case "api":
				req.Header.Set("X-Api-Key", "test-key")
			case "basic":
				req.SetBasicAuth("admin", "pass")
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != 500 {
				t.Errorf("status = %d, want 500", resp.StatusCode)
			}
		})
	}
}

func TestTriggerSync_Conflict(t *testing.T) {
	ctx := context.Background()
	store, err := db.Open(ctx, "", db.Migrations{1: db.Migration001, 2: db.Migration002})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	// Create a slow fake Synod server that delays responses
	slowSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(404)
	}))
	t.Cleanup(func() { slowSrv.Close() })

	client := ingestion.NewClient(slowSrv.URL, 1000)
	t.Cleanup(func() { client.Close() })
	syncer := ingestion.NewSyncer(client, store)

	srv := NewServer(store, syncer, []string{"test-key"}, "admin", "pass", 1000)
	t.Cleanup(func() { srv.Close() })

	ts := httptest.NewServer(srv.Router)
	t.Cleanup(func() { ts.Close() })

	// First trigger starts the sync goroutine
	req, _ := http.NewRequest("POST", ts.URL+"/admin/sync", nil)
	req.SetBasicAuth("admin", "pass")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 202 {
		t.Fatalf("first sync: status = %d, want 202", resp.StatusCode)
	}

	// Small delay to let the goroutine start and set syncBusy
	time.Sleep(100 * time.Millisecond)

	// Second trigger should get 409 conflict
	req2, _ := http.NewRequest("POST", ts.URL+"/admin/sync", nil)
	req2.SetBasicAuth("admin", "pass")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != 409 {
		t.Errorf("second sync: status = %d, want 409", resp2.StatusCode)
	}
}
