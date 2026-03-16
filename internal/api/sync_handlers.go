package api

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/natalie-johanek/trench-analytics/internal/db"
	"github.com/natalie-johanek/trench-analytics/internal/ingestion"
	"github.com/natalie-johanek/trench-analytics/internal/models"
)

// SyncHandlers holds dependencies for sync-related endpoints.
type SyncHandlers struct {
	Syncer   *ingestion.Syncer
	DB       *db.DB
	wg       sync.WaitGroup
	syncBusy int32 // 0=idle, 1=running; accessed via atomic
}

// Wait blocks until all background sync goroutines complete.
func (sh *SyncHandlers) Wait() {
	sh.wg.Wait()
}

// TriggerSync handles POST /admin/sync.
// Returns 409 if a sync is already running, 202 otherwise.
func (sh *SyncHandlers) TriggerSync(w http.ResponseWriter, r *http.Request) {
	if !atomic.CompareAndSwapInt32(&sh.syncBusy, 0, 1) {
		writeError(w, http.StatusConflict, "sync_in_progress", "a sync is already running")
		return
	}

	sh.wg.Add(1)
	go func() {
		defer sh.wg.Done()
		defer atomic.StoreInt32(&sh.syncBusy, 0)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		if err := sh.Syncer.IncrementalSync(ctx); err != nil {
			slog.Error("incremental sync failed", "err", err)
		}
	}()

	writeJSON(w, http.StatusAccepted, models.SyncStatusResponse{
		Status: "accepted",
	})
}

// SyncStatus handles GET /admin/sync/status.
func (sh *SyncHandlers) SyncStatus(w http.ResponseWriter, r *http.Request) {
	run, err := db.QueryLatestIngestion(r.Context(), sh.DB.Pool())
	if err != nil {
		slog.Error("query sync status", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to query sync status")
		return
	}

	if run == nil {
		writeJSON(w, http.StatusOK, models.SyncStatusResponse{Status: "no_runs"})
		return
	}

	resp := models.SyncStatusResponse{
		Status:          run.Status,
		RunType:         run.RunType,
		StartedAt:       run.RunAt.Format(time.RFC3339),
		ReportsFound:    run.ReportsFound,
		ReportsInserted: run.ReportsInserted,
		IDsScanned:      run.IDsScanned,
		Errors:          run.Errors,
	}
	if run.CompletedAt != nil {
		s := run.CompletedAt.Format(time.RFC3339)
		resp.CompletedAt = &s
	}

	writeJSON(w, http.StatusOK, resp)
}
