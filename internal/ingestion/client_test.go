package ingestion

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/natalie-johanek/trench-analytics/internal/models"
)

func TestFetchReport_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(models.SynodReport{GameReportID: 42})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 100)
	defer c.Close()

	report, raw, err := c.FetchReport(context.Background(), 42)
	if err != nil {
		t.Fatal(err)
	}
	if report == nil {
		t.Fatal("expected report")
	}
	if report.GameReportID != 42 {
		t.Errorf("id = %d, want 42", report.GameReportID)
	}
	if len(raw) == 0 {
		t.Error("expected raw json")
	}
}

func TestFetchReport_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte(`{"code":"invalid_game_report"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 100)
	defer c.Close()

	report, raw, err := c.FetchReport(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report != nil || raw != nil {
		t.Error("expected nil for 404")
	}
}

func TestFetchReport_RetryOn500(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls <= 2 {
			w.WriteHeader(500)
			return
		}
		json.NewEncoder(w).Encode(models.SynodReport{GameReportID: 1})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 100)
	defer c.Close()

	report, _, err := c.FetchReport(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report == nil {
		t.Fatal("expected report after retries")
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestFetchReport_ContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block forever — context should cancel
		select {}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 100)
	defer c.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, _, err := c.FetchReport(ctx, 1)
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestFetchReport_UserAgent(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		json.NewEncoder(w).Encode(models.SynodReport{GameReportID: 1})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 100)
	defer c.Close()
	c.FetchReport(context.Background(), 1)

	if gotUA != "TrenchAnalytics/1.0" {
		t.Errorf("User-Agent = %q, want TrenchAnalytics/1.0", gotUA)
	}
}

func TestFetchReport_UnexpectedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 100)
	defer c.Close()

	_, _, err := c.FetchReport(context.Background(), 1)
	if err == nil {
		t.Error("expected error for 403 status")
	}
}

func TestFetchReport_AllRetriesExhausted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 100)
	c.maxRetries = 1
	defer c.Close()

	_, _, err := c.FetchReport(context.Background(), 1)
	if err == nil {
		t.Error("expected error after all retries exhausted")
	}
}

func TestFetchReport_ContextCancelDuringRetry(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(500)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 100)
	defer c.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for atomic.LoadInt32(&calls) < 1 {
		}
		cancel()
	}()

	_, _, err := c.FetchReport(ctx, 1)
	if err == nil {
		t.Error("expected error from cancelled context during retry")
	}
}

func TestTransform_RealReport(t *testing.T) {
	data, err := os.ReadFile("testdata/valid_report_raw.json")
	if err != nil {
		t.Skip("testdata/valid_report_raw.json not found")
	}

	var report models.SynodReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result, err := Transform(&report)
	if err != nil {
		t.Fatalf("transform: %v", err)
	}

	if result.Game.GameReportID != 198409 {
		t.Errorf("game_report_id = %d, want 198409", result.Game.GameReportID)
	}
	if len(result.Participants) != 2 {
		t.Errorf("participants = %d, want 2", len(result.Participants))
	}
	if len(result.Units) == 0 {
		t.Error("expected units")
	}

	// Verify winner
	hasWin, hasLoss := false, false
	for _, p := range result.Participants {
		if p.Result == "win" {
			hasWin = true
		}
		if p.Result == "loss" {
			hasLoss = true
		}
	}
	if !hasWin || !hasLoss {
		t.Error("expected one win and one loss")
	}

	// Verify row_index is sequential per warband
	for _, p := range result.Participants {
		maxIdx := -1
		for _, u := range result.Units {
			if u.WarbandID == p.WarbandID {
				if u.RowIndex != maxIdx+1 {
					t.Errorf("warband %d: row_index gap at %d (expected %d)", p.WarbandID, u.RowIndex, maxIdx+1)
				}
				maxIdx = u.RowIndex
			}
		}
	}
}

func TestFetchReportsPage_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/game-reports" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("page") != "1" || r.URL.Query().Get("per_page") != "50" {
			t.Errorf("unexpected params: %s", r.URL.RawQuery)
		}
		w.Header().Set("X-WP-Total", "120")
		w.Header().Set("X-WP-TotalPages", "3")
		w.Write([]byte(`[{"game_report_id":1},{"game_report_id":2}]`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 100)
	defer c.Close()

	result, err := c.FetchReportsPage(context.Background(), 1, 50, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Reports) != 2 {
		t.Errorf("reports = %d, want 2", len(result.Reports))
	}
	if result.TotalItems != 120 {
		t.Errorf("total = %d, want 120", result.TotalItems)
	}
	if result.TotalPages != 3 {
		t.Errorf("pages = %d, want 3", result.TotalPages)
	}
}

func TestFetchReportsPage_ModifiedAfter(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("X-WP-Total", "0")
		w.Header().Set("X-WP-TotalPages", "0")
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 100)
	defer c.Close()

	ts := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	_, err := c.FetchReportsPage(context.Background(), 1, 100, &ts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotQuery, "modified_after=") {
		t.Errorf("expected modified_after param, got: %s", gotQuery)
	}
}

func TestFetchReportsPage_RetryOn500(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls <= 2 {
			w.WriteHeader(500)
			return
		}
		w.Header().Set("X-WP-Total", "1")
		w.Header().Set("X-WP-TotalPages", "1")
		w.Write([]byte(`[{"game_report_id":1}]`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 100)
	defer c.Close()

	result, err := c.FetchReportsPage(context.Background(), 1, 100, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Reports) != 1 {
		t.Errorf("reports = %d, want 1", len(result.Reports))
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestFetchReportsPage_AllRetriesExhausted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 100)
	c.maxRetries = 1
	defer c.Close()

	_, err := c.FetchReportsPage(context.Background(), 1, 100, nil)
	if err == nil {
		t.Error("expected error after retries exhausted")
	}
}

func TestFetchReportsPage_UnexpectedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 100)
	defer c.Close()

	_, err := c.FetchReportsPage(context.Background(), 1, 100, nil)
	if err == nil {
		t.Error("expected error for 403")
	}
}

func TestFetchReportsPage_ContextCancel(t *testing.T) {
	c := NewClient("http://localhost:0", 100)
	defer c.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.FetchReportsPage(ctx, 1, 100, nil)
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}
