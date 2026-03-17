package ingestion

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/natalie-johanek/trench-analytics/internal/db"
	"github.com/natalie-johanek/trench-analytics/internal/logging"
	"github.com/natalie-johanek/trench-analytics/internal/models"
)

const perPage = 100

// Syncer orchestrates ingestion runs (backfill and incremental).
type Syncer struct {
	client *Client
	store  *db.DB
}

// NewSyncer creates a new ingestion syncer.
func NewSyncer(client *Client, store *db.DB) *Syncer {
	return &Syncer{client: client, store: store}
}

// Backfill fetches all reports using the paginated archive endpoint.
func (s *Syncer) Backfill(ctx context.Context, start, count int) error {
	conn, release, err := s.store.AcquireWriter(ctx)
	if err != nil {
		return fmt.Errorf("acquiring writer: %w", err)
	}
	runID, err := db.StartIngestionRun(ctx, conn, "backfill", &start, &count)
	release()
	if err != nil {
		return fmt.Errorf("starting ingestion run: %w", err)
	}

	tracker := newRunTracker(runID)
	defer func() {
		bgCtx, cancel := db.BackgroundCtx(10 * time.Second)
		defer cancel()
		conn, release, err := s.store.AcquireWriter(bgCtx)
		if err != nil {
			slog.Error("failed to acquire writer for backfill finish", "err", err)
			return
		}
		defer release()
		tracker.finish(conn)
	}()

	for page := 1; ; page++ {
		if ctx.Err() != nil {
			tracker.notes = "cancelled"
			return ctx.Err()
		}

		result, err := s.client.FetchReportsPage(ctx, page, perPage, nil)
		if err != nil {
			return fmt.Errorf("fetching page %d: %w", page, err)
		}

		conn, release, err := s.store.AcquireWriter(ctx)
		if err != nil {
			return fmt.Errorf("acquiring writer for page %d: %w", page, err)
		}

		for _, rawJSON := range result.Reports {
			tracker.scanned++
			s.processRawReport(ctx, conn, rawJSON, tracker)
		}

		release()

		slog.Info("backfill progress",
			"page", page, "total_pages", result.TotalPages,
			"found", tracker.found, "inserted", tracker.inserted)

		if page >= result.TotalPages {
			break
		}
	}
	return nil
}

// ProgressFunc is called with (scanned, totalItems) during sync.
type ProgressFunc func(scanned, total int)

// IncrementalSync fetches only reports modified since the last completed sync.
func (s *Syncer) IncrementalSync(ctx context.Context, onProgress ProgressFunc) error {
	conn, release, locked, err := s.store.TryAcquireWriter(ctx)
	if err != nil {
		return fmt.Errorf("acquiring writer: %w", err)
	}
	if !locked {
		return fmt.Errorf("sync already in progress")
	}

	// Determine modified_after from last completed run
	modifiedAfter, err := db.LastCompletedRunTime(ctx, conn)
	if err != nil {
		release()
		return fmt.Errorf("getting last run time: %w", err)
	}

	runID, err := db.StartIngestionRun(ctx, conn, "incremental", nil, nil)
	if err != nil {
		release()
		return fmt.Errorf("starting ingestion run: %w", err)
	}
	release()

	tracker := newRunTracker(runID)
	defer func() {
		bgCtx, cancel := db.BackgroundCtx(10 * time.Second)
		defer cancel()
		conn, release, err := s.store.AcquireWriter(bgCtx)
		if err != nil {
			slog.Error("failed to acquire writer for sync finish", "err", err)
			return
		}
		defer release()
		tracker.finish(conn)
	}()

	for page := 1; ; page++ {
		if ctx.Err() != nil {
			tracker.notes = "cancelled"
			return ctx.Err()
		}

		result, err := s.client.FetchReportsPage(ctx, page, perPage, modifiedAfter)
		if err != nil {
			return fmt.Errorf("fetching page %d: %w", page, err)
		}

		if len(result.Reports) == 0 {
			break
		}

		conn, release, err := s.store.AcquireWriter(ctx)
		if err != nil {
			return fmt.Errorf("acquiring writer for page %d: %w", page, err)
		}

		for _, rawJSON := range result.Reports {
			tracker.scanned++
			s.processRawReport(ctx, conn, rawJSON, tracker)
		}

		release()

		slog.Info("incremental sync progress",
			"page", page, "total_pages", result.TotalPages,
			"total_items", result.TotalItems,
			"found", tracker.found, "inserted", tracker.inserted)

		if onProgress != nil {
			onProgress(tracker.scanned, result.TotalItems)
		}

		if page >= result.TotalPages {
			break
		}
	}
	return nil
}

// paginatedReport is the wrapper shape returned by the paginated archive endpoint.
type paginatedReport struct {
	ReportData models.SynodReport `json:"report_data"`
}

// processRawReport unmarshals, transforms, and inserts a single report from raw JSON.
// Handles both the paginated wrapper shape (report_data) and the direct single-report shape.
func (s *Syncer) processRawReport(ctx context.Context, conn *sql.Conn, rawJSON json.RawMessage, t *runTracker) {
	var wrapper paginatedReport
	if err := json.Unmarshal(rawJSON, &wrapper); err != nil {
		slog.Warn("decode error", "err", err)
		t.errors++
		return
	}

	report := &wrapper.ReportData
	if report.GameReportID == 0 {
		// Fall back to direct shape (single-report endpoint)
		if err := json.Unmarshal(rawJSON, report); err != nil {
			slog.Warn("decode error", "err", err)
			t.errors++
			return
		}
	}

	t.found++
	if err := ingestOne(ctx, conn, report, rawJSON); err != nil {
		slog.Warn("ingest error", "id", report.GameReportID, "err", err)
		t.errors++
		return
	}
	t.inserted++
}

// ingestOne transforms and inserts a single report inside a transaction with panic recovery.
func ingestOne(ctx context.Context, conn *sql.Conn, report *models.SynodReport, rawJSON json.RawMessage) (retErr error) {
	defer func() {
		if r := recover(); r != nil {
			retErr = fmt.Errorf("panic processing report %d: %v", report.GameReportID, r)
		}
	}()

	result, err := Transform(report)
	if err != nil {
		if _, insertErr := db.InsertRawReport(ctx, conn, report.GameReportID, rawJSON); insertErr != nil {
			slog.Warn("failed to store raw report for failed transform", "id", report.GameReportID, "err", insertErr)
		}
		return fmt.Errorf("transform report %d: %w", report.GameReportID, err)
	}

	return db.ExecTx(ctx, conn, func(tx *sql.Tx) error {
		isNew, err := db.InsertRawReport(ctx, tx, report.GameReportID, rawJSON)
		if err != nil {
			return err
		}
		if !isNew {
			return nil
		}

		if err := db.InsertGame(ctx, tx, result.Game); err != nil {
			return fmt.Errorf("inserting game: %w", err)
		}
		for _, p := range result.Participants {
			if err := db.InsertParticipant(ctx, tx, p); err != nil {
				return fmt.Errorf("inserting participant: %w", err)
			}
		}
		for _, u := range result.Units {
			if err := db.InsertUnit(ctx, tx, u); err != nil {
				return fmt.Errorf("inserting unit: %w", err)
			}
		}
		for _, d := range result.Deeds {
			if err := db.InsertDeed(ctx, tx, d); err != nil {
				return fmt.Errorf("inserting deed: %w", err)
			}
		}
		return nil
	})
}

// runTracker accumulates stats for an ingestion run.
type runTracker struct {
	runID     int
	startTime time.Time
	found     int
	inserted  int
	scanned   int
	errors    int
	notes     string
}

func newRunTracker(runID int) *runTracker {
	return &runTracker{runID: runID, startTime: time.Now()}
}

// finish writes the final ingestion stats and emits a wide event.
func (t *runTracker) finish(conn *sql.Conn) {
	ctx, cancel := db.BackgroundCtx(10 * time.Second)
	defer cancel()

	status := "completed"
	if t.errors > 0 && t.inserted == 0 {
		status = "failed"
	}
	ms := int(time.Since(t.startTime).Milliseconds())
	if err := db.CompleteIngestionRun(ctx, conn, t.runID, status, t.found, t.inserted, t.scanned, t.errors, ms, t.notes); err != nil {
		slog.Error("failed to complete ingestion run", "err", err)
	}

	event := logging.New(
		"operation", "ingestion",
		"run_id", t.runID,
		"status", status,
		"reports_found", t.found,
		"reports_inserted", t.inserted,
		"ids_scanned", t.scanned,
		"errors", t.errors,
		"notes", t.notes,
	)
	level := slog.LevelInfo
	if status == "failed" {
		level = slog.LevelError
	}
	event.Emit(level)
}
