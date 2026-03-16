// Package tests contains integration and end-to-end tests that exercise
// the full system: HTTP server, DuckDB, ingestion pipeline, and stats API.
//
// Unit tests remain alongside source files per Go convention.
// These tests spin up a real server and hit real endpoints.
package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/natalie-johanek/trench-analytics/internal/api"
	"github.com/natalie-johanek/trench-analytics/internal/db"
	"github.com/natalie-johanek/trench-analytics/internal/ingestion"
	"github.com/natalie-johanek/trench-analytics/internal/models"
)

const testAPIKey = "integration-test-key"
const testAdminUser = "admin"
const testAdminPass = "testpass"

// testEnv holds a fully wired test environment.
type testEnv struct {
	Server  *httptest.Server
	Store   *db.DB
	Synod   *httptest.Server
	cleanup func()
}

// setupEnv creates a full integration test environment with a fake Synod API
// serving the provided reports, backed by an in-memory DuckDB.
func setupEnv(t *testing.T, reports map[int]models.SynodReport) *testEnv {
	t.Helper()

	synod := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		idStr := parts[len(parts)-1]
		id, err := strconv.Atoi(idStr)
		if err != nil {
			w.WriteHeader(404)
			json.NewEncoder(w).Encode(models.SynodError{Code: "invalid_game_report", Message: "not found"})
			return
		}
		report, ok := reports[id]
		if !ok {
			w.WriteHeader(404)
			json.NewEncoder(w).Encode(models.SynodError{Code: "invalid_game_report", Message: "not found"})
			return
		}
		json.NewEncoder(w).Encode(report)
	}))

	store, err := db.Open(context.Background(), "", db.Migrations{1: db.Migration001})
	if err != nil {
		synod.Close()
		t.Fatal(err)
	}

	client := ingestion.NewClient(synod.URL, 100)
	syncer := ingestion.NewSyncer(client, store)
	srv := api.NewServer(store, syncer, testAPIKey, testAdminUser, testAdminPass, 1000)
	ts := httptest.NewServer(srv.Router)

	return &testEnv{
		Server: ts,
		Store:  store,
		Synod:  synod,
		cleanup: func() {
			ts.Close()
			srv.Close()
			client.Close()
			synod.Close()
			store.Close()
		},
	}
}

func (e *testEnv) Close() { e.cleanup() }

func (e *testEnv) get(t *testing.T, path string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("GET", e.Server.URL+path, nil)
	req.Header.Set("X-Api-Key", testAPIKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func (e *testEnv) adminGet(t *testing.T, path string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("GET", e.Server.URL+path, nil)
	req.SetBasicAuth(testAdminUser, testAdminPass)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func (e *testEnv) adminPost(t *testing.T, path string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("POST", e.Server.URL+path, nil)
	req.SetBasicAuth(testAdminUser, testAdminPass)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func makeReport(id, winner int, factionA, factionB string) models.SynodReport {
	return models.SynodReport{
		GameReportID: id,
		PlayerIDs:    []int{1, 2},
		WarbandIDs:   []int{10, 20},
		Data: models.SynodReportData{
			Winner:     &winner,
			Kills:      map[string]int{"10": 3, "20": 2},
			VP:         map[string]int{"10": 1, "20": 0},
			ScenarioID: "sc_fieldsofglory",
			Date:       1700000000,
			Ranked:     true,
			Deeds:      []models.SynodDeed{{ID: "gd_sniper", Name: "Sniper", Warband: intPtr(10)}},
			Warbands: []models.SynodWarband{
				{
					FactionSlug: factionA,
					WarbandExport: models.SynodExport{
						WID: 10, WNA: "Warband A", DR: 500,
						ELT: []models.SynodUnit{{MDN: "Leader", MDI: "md_leader", C: models.SynodCost{D: 100}, EQ: []models.SynodItem{{N: "Sword", I: "eq_sword"}}}},
						MDS: []models.SynodUnit{{MDN: "Trooper", MDI: "md_trooper", C: models.SynodCost{D: 50}}},
					},
				},
				{
					FactionSlug: factionB,
					WarbandExport: models.SynodExport{
						WID: 20, WNA: "Warband B", DR: 450,
						MDS: []models.SynodUnit{{MDN: "Heretic", MDI: "md_heretic", C: models.SynodCost{D: 40}}},
					},
				},
			},
		},
	}
}

func intPtr(v int) *int { return &v }

// --- E2E Tests ---

func TestE2E_FullPipeline(t *testing.T) {
	reports := map[int]models.SynodReport{
		100: makeReport(100, 10, "fc_newantioch", "fc_hereticlegion"),
		101: makeReport(101, 20, "fc_newantioch", "fc_hereticlegion"),
		102: makeReport(102, 10, "fc_newantioch_fv_redbrigade", "fc_trenchpilgrim"),
	}
	env := setupEnv(t, reports)
	defer env.Close()

	// Step 1: Backfill
	client := ingestion.NewClient(env.Synod.URL, 100)
	defer client.Close()
	syncer := ingestion.NewSyncer(client, env.Store)
	if err := syncer.Backfill(context.Background(), 102, 3); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	// Step 2: Verify summary
	resp := env.get(t, "/api/v1/stats/summary")
	defer resp.Body.Close()
	var summary models.SummaryResponse
	json.NewDecoder(resp.Body).Decode(&summary)
	if summary.TotalGames != 3 {
		t.Errorf("total_games = %d, want 3", summary.TotalGames)
	}

	// Step 3: Verify faction stats
	resp2 := env.get(t, "/api/v1/stats/factions?ranked_only=true&min_games=1")
	defer resp2.Body.Close()
	var factionResp models.FactionStatsResponse
	json.NewDecoder(resp2.Body).Decode(&factionResp)
	if len(factionResp.Factions) == 0 {
		t.Fatal("expected faction stats")
	}

	// Step 4: Verify unit stats
	resp3 := env.get(t, "/api/v1/stats/units?min_games=1")
	defer resp3.Body.Close()
	var unitResp models.UnitStatsResponse
	json.NewDecoder(resp3.Body).Decode(&unitResp)
	if len(unitResp.Units) == 0 {
		t.Fatal("expected unit stats")
	}

	// Step 5: Verify equipment stats
	resp4 := env.get(t, "/api/v1/stats/equipment?faction=fc_newantioch&min_games=1")
	defer resp4.Body.Close()
	var eqResp models.EquipmentStatsResponse
	json.NewDecoder(resp4.Body).Decode(&eqResp)
	if len(eqResp.Equipment) == 0 {
		t.Fatal("expected equipment stats")
	}

	// Step 6: Verify scenarios
	resp5 := env.get(t, "/api/v1/stats/scenarios?min_games=1")
	defer resp5.Body.Close()
	var scenResp models.ScenarioStatsResponse
	json.NewDecoder(resp5.Body).Decode(&scenResp)
	if len(scenResp.Scenarios) == 0 {
		t.Fatal("expected scenario stats")
	}

	// Step 7: Verify deeds
	resp6 := env.get(t, "/api/v1/stats/deeds")
	defer resp6.Body.Close()
	var deedResp models.DeedStatsResponse
	json.NewDecoder(resp6.Body).Decode(&deedResp)
	if len(deedResp.Deeds) == 0 {
		t.Fatal("expected deed stats")
	}

	// Step 8: Verify meta
	resp7 := env.get(t, "/api/v1/stats/meta")
	defer resp7.Body.Close()
	var meta models.MetaResponse
	json.NewDecoder(resp7.Body).Decode(&meta)
	if len(meta.Factions) == 0 {
		t.Fatal("expected factions in meta")
	}
	if len(meta.Scenarios) == 0 {
		t.Fatal("expected scenarios in meta")
	}

	// Step 9: Verify sync status shows completed backfill
	resp8 := env.adminGet(t, "/admin/sync/status")
	defer resp8.Body.Close()
	var syncStatus models.SyncStatusResponse
	json.NewDecoder(resp8.Body).Decode(&syncStatus)
	if syncStatus.Status != "completed" {
		t.Errorf("sync status = %q, want completed", syncStatus.Status)
	}
}

func TestE2E_IncrementalSync(t *testing.T) {
	reports := map[int]models.SynodReport{
		100: makeReport(100, 10, "fc_newantioch", "fc_hereticlegion"),
		101: makeReport(101, 20, "fc_newantioch", "fc_hereticlegion"),
	}
	env := setupEnv(t, reports)
	defer env.Close()

	// Backfill only report 100
	client := ingestion.NewClient(env.Synod.URL, 100)
	defer client.Close()
	syncer := ingestion.NewSyncer(client, env.Store)
	syncer.Backfill(context.Background(), 100, 1)

	// Trigger incremental sync via HTTP
	resp := env.adminPost(t, "/admin/sync")
	resp.Body.Close()
	if resp.StatusCode != 202 {
		t.Fatalf("sync trigger status = %d, want 202", resp.StatusCode)
	}

	// Wait for sync to complete
	for i := 0; i < 100; i++ {
		time.Sleep(100 * time.Millisecond)
		r := env.adminGet(t, "/admin/sync/status")
		var status models.SyncStatusResponse
		json.NewDecoder(r.Body).Decode(&status)
		r.Body.Close()
		if status.Status == "completed" {
			break
		}
	}

	// Should now have 2 games
	resp2 := env.get(t, "/api/v1/stats/summary")
	defer resp2.Body.Close()
	var summary models.SummaryResponse
	json.NewDecoder(resp2.Body).Decode(&summary)
	if summary.TotalGames != 2 {
		t.Errorf("total_games = %d, want 2", summary.TotalGames)
	}
}

func TestE2E_AuthEnforcement(t *testing.T) {
	env := setupEnv(t, nil)
	defer env.Close()

	tests := []struct {
		name   string
		method string
		path   string
		key    string
		user   string
		pass   string
		want   int
	}{
		{"stats no key", "GET", "/api/v1/stats/factions", "", "", "", 401},
		{"stats bad key", "GET", "/api/v1/stats/factions", "wrong", "", "", 401},
		{"stats valid key", "GET", "/api/v1/stats/factions", testAPIKey, "", "", 200},
		{"admin no auth", "POST", "/admin/sync", "", "", "", 401},
		{"admin bad pass", "POST", "/admin/sync", "", testAdminUser, "wrong", 401},
		{"admin valid", "GET", "/admin/sync/status", "", testAdminUser, testAdminPass, 200},
		{"health public", "GET", "/api/v1/health", "", "", "", 200},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest(tt.method, env.Server.URL+tt.path, nil)
			if tt.key != "" {
				req.Header.Set("X-Api-Key", tt.key)
			}
			if tt.user != "" {
				req.SetBasicAuth(tt.user, tt.pass)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != tt.want {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.want)
			}
		})
	}
}

func TestE2E_EmptyDB_ReturnsEmptyArrays(t *testing.T) {
	env := setupEnv(t, nil)
	defer env.Close()

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
			resp := env.get(t, ep)
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				t.Fatalf("status = %d", resp.StatusCode)
			}
			body := map[string]json.RawMessage{}
			json.NewDecoder(resp.Body).Decode(&body)
			// Find the array field and verify it's [] not null
			for key, val := range body {
				if key == "generated_at" || key == "filters" || key == "total_games" {
					continue
				}
				s := string(val)
				if s == "null" {
					t.Errorf("%s.%s is null, want []", ep, key)
				}
			}
		})
	}
}

func TestE2E_FilterParams(t *testing.T) {
	reports := map[int]models.SynodReport{
		100: makeReport(100, 10, "fc_newantioch", "fc_hereticlegion"),
	}
	env := setupEnv(t, reports)
	defer env.Close()

	client := ingestion.NewClient(env.Synod.URL, 100)
	defer client.Close()
	ingestion.NewSyncer(client, env.Store).Backfill(context.Background(), 100, 1)

	// Valid filters
	resp := env.get(t, "/api/v1/stats/factions?ranked_only=true&min_games=1&since=2020-01-01&until=2030-01-01")
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("valid filters: status = %d", resp.StatusCode)
	}

	// Invalid min_games
	resp2 := env.get(t, fmt.Sprintf("/api/v1/stats/factions?min_games=abc"))
	defer resp2.Body.Close()
	if resp2.StatusCode != 400 {
		t.Errorf("invalid min_games: status = %d, want 400", resp2.StatusCode)
	}

	// Invalid date
	resp3 := env.get(t, "/api/v1/stats/factions?since=not-a-date")
	defer resp3.Body.Close()
	if resp3.StatusCode != 400 {
		t.Errorf("invalid since: status = %d, want 400", resp3.StatusCode)
	}
}
