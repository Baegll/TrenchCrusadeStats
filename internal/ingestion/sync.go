package ingestion

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/natalie-johanek/trench-analytics/internal/db"
	"github.com/natalie-johanek/trench-analytics/internal/models"
)

// Syncer orchestrates ingestion runs (backfill and incremental).
type Syncer struct {
	client *Client
	store  *db.DB
}

// NewSyncer creates a new ingestion syncer.
func NewSyncer(client *Client, store *db.DB) *Syncer {
	return &Syncer{client: client, store: store}
}

// Backfill scans IDs downward from start for count IDs.
// Releases the write lock between batches of 50 to avoid blocking syncs.
func (s *Syncer) Backfill(ctx context.Context, start, count int) error {
	rangeEnd := start - count + 1
	if rangeEnd < 1 {
		rangeEnd = 1
	}

	// Start ingestion log entry
	conn, release, err := s.store.AcquireWriter(ctx)
	if err != nil {
		return fmt.Errorf("acquiring writer: %w", err)
	}
	runID, err := db.StartIngestionRun(ctx, conn, "backfill", &rangeEnd, &start)
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

	batchSize := 50
	for batchStart := start; batchStart >= rangeEnd; batchStart -= batchSize {
		if ctx.Err() != nil {
			tracker.notes = "cancelled"
			return ctx.Err()
		}

		batchEnd := batchStart - batchSize + 1
		if batchEnd < rangeEnd {
			batchEnd = rangeEnd
		}

		conn, release, err := s.store.AcquireWriter(ctx)
		if err != nil {
			return fmt.Errorf("acquiring writer for batch: %w", err)
		}

		for id := batchStart; id >= batchEnd; id-- {
			if ctx.Err() != nil {
				release()
				tracker.notes = "cancelled"
				return ctx.Err()
			}
			s.processID(ctx, conn, id, tracker)
		}

		release()

		if tracker.inserted > 0 && tracker.inserted%200 == 0 {
			slog.Info("backfill progress", "scanned", tracker.scanned, "found", tracker.found, "inserted", tracker.inserted)
		}
	}
	return nil
}

// IncrementalSync scans upward from the highest known ID.
// Returns an error containing "sync already in progress" if the write lock is held.
func (s *Syncer) IncrementalSync(ctx context.Context) error {
	conn, release, locked, err := s.store.TryAcquireWriter(ctx)
	if err != nil {
		return fmt.Errorf("acquiring writer: %w", err)
	}
	if !locked {
		return fmt.Errorf("sync already in progress")
	}
	defer release()

	maxID, err := db.MaxGameReportID(ctx, conn)
	if err != nil {
		return fmt.Errorf("getting max ID: %w", err)
	}

	start := maxID + 1
	runID, err := db.StartIngestionRun(ctx, conn, "incremental", &start, nil)
	if err != nil {
		return fmt.Errorf("starting ingestion run: %w", err)
	}

	tracker := newRunTracker(runID)
	defer tracker.finish(conn)

	consecutive404s := 0
	for id := start; consecutive404s < 500; id++ {
		if ctx.Err() != nil {
			tracker.notes = "cancelled"
			return ctx.Err()
		}

		wasFound := s.processID(ctx, conn, id, tracker)
		if wasFound {
			consecutive404s = 0
		} else {
			consecutive404s++
		}
	}
	return nil
}

// processID fetches, transforms, and inserts a single ID. Returns true if a report was found.
func (s *Syncer) processID(ctx context.Context, conn *sql.Conn, id int, t *runTracker) bool {
	t.scanned++

	report, rawJSON, err := s.client.FetchReport(ctx, id)
	if err != nil {
		slog.Warn("fetch error", "id", id, "err", err)
		t.errors++
		return false
	}
	if report == nil {
		return false // 404
	}

	t.found++
	if err := ingestOne(ctx, conn, report, rawJSON); err != nil {
		slog.Warn("ingest error", "id", id, "err", err)
		t.errors++
		return true // found but failed to ingest
	}
	t.inserted++

	if t.inserted%50 == 0 {
		slog.Info("progress", "scanned", t.scanned, "found", t.found, "inserted", t.inserted)
	}
	return true
}

// ingestOne transforms and inserts a single report inside a transaction with panic recovery.
func ingestOne(ctx context.Context, conn *sql.Conn, report *models.SynodReport, rawJSON json.RawMessage) (retErr error) {
	defer func() {
		if r := recover(); r != nil {
			retErr = fmt.Errorf("panic processing report %d: %v", report.GameReportID, r)
		}
	}()

	// Transform first (before any DB writes) — fail fast on bad data
	result, err := Transform(report)
	if err != nil {
		// Store raw even if transform fails (for debugging), but outside transaction
		if _, insertErr := db.InsertRawReport(ctx, conn, report.GameReportID, rawJSON); insertErr != nil {
			slog.Warn("failed to store raw report for failed transform", "id", report.GameReportID, "err", insertErr)
		}
		return fmt.Errorf("transform report %d: %w", report.GameReportID, err)
	}

	// Insert everything in a single transaction — all or nothing
	return db.ExecTx(ctx, conn, func(tx *sql.Tx) error {
		isNew, err := db.InsertRawReport(ctx, tx, report.GameReportID, rawJSON)
		if err != nil {
			return err
		}
		if !isNew {
			return nil // already fully ingested
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

// finish writes the final ingestion stats using a background context (not the caller's).
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
	slog.Info("run complete", "status", status, "found", t.found, "inserted", t.inserted, "scanned", t.scanned, "errors", t.errors)
}
