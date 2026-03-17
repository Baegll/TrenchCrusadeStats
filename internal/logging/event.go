// Package logging implements the wide event / canonical log line pattern.
// Instead of scattered log statements, each request or operation builds
// a single structured event with all context, emitted once at completion.
//
// The ring buffer uses tail sampling: errors, warnings, and slow requests
// are always kept; successful fast requests are sampled at a configurable rate.
package logging

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"
)

// Default tail sampling config.
const (
	DefaultSampleRate   = 0.05 // 5% of normal success events
	DefaultSlowThreshMs = 500  // requests slower than this are always kept
)

type ctxKey struct{}

// Event is a wide event that accumulates context throughout a request or operation.
type Event struct {
	mu     sync.Mutex
	fields map[string]any
	start  time.Time
}

// Buffer is a thread-safe ring buffer that captures emitted events for the /admin/logs endpoint.
// Uses tail sampling: always keeps errors, warnings, and slow requests;
// randomly samples successful fast requests.
type Buffer struct {
	mu             sync.Mutex
	events         []map[string]any
	size           int
	cursor         int
	count          int
	sampleRate     float64
	slowThresholdMs int64
}

var globalBuf *Buffer

// NewBuffer creates a ring buffer that holds the last n events with tail sampling.
func NewBuffer(n int) *Buffer {
	return &Buffer{
		events:          make([]map[string]any, n),
		size:            n,
		sampleRate:      DefaultSampleRate,
		slowThresholdMs: DefaultSlowThreshMs,
	}
}

// SetBuffer configures the global event buffer. Call once at startup.
func SetBuffer(b *Buffer) { globalBuf = b }

// shouldKeep implements tail sampling logic.
func (b *Buffer) shouldKeep(fields map[string]any) bool {
	// Always keep errors and warnings
	if lvl, ok := fields["level"].(string); ok {
		if lvl == "ERROR" || lvl == "WARN" {
			return true
		}
	}

	// Always keep slow requests
	if dur, ok := fields["duration_ms"].(int64); ok && dur >= b.slowThresholdMs {
		return true
	}

	// Always keep non-HTTP events (ingestion, operations)
	if _, hasOp := fields["operation"]; hasOp {
		return true
	}

	// Sample the rest
	return rand.Float64() < b.sampleRate
}

func (b *Buffer) append(fields map[string]any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.shouldKeep(fields) {
		return
	}
	b.events[b.cursor] = fields
	b.cursor = (b.cursor + 1) % b.size
	if b.count < b.size {
		b.count++
	}
}

// Recent returns up to the last n events, newest first.
func (b *Buffer) Recent(n int) []map[string]any {
	b.mu.Lock()
	defer b.mu.Unlock()
	if n > b.count {
		n = b.count
	}
	result := make([]map[string]any, n)
	for i := 0; i < n; i++ {
		idx := (b.cursor - 1 - i + b.size) % b.size
		result[i] = b.events[idx]
	}
	return result
}

// envFields are environment characteristics included in every wide event.
// Set once at startup via SetEnvFields.
var envFields []any

// SetEnvFields configures environment context for all wide events.
func SetEnvFields(kvs ...any) { envFields = kvs }

// New creates a new wide event with the given initial key-value pairs.
// Environment fields are automatically included.
func New(initial ...any) *Event {
	e := &Event{fields: make(map[string]any), start: time.Now()}
	e.Set(envFields...)
	e.Set(initial...)
	return e
}

// Set adds key-value pairs to the event. Keys must be strings. Safe for concurrent use.
func (e *Event) Set(kvs ...any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := 0; i+1 < len(kvs); i += 2 {
		if key, ok := kvs[i].(string); ok {
			e.fields[key] = kvs[i+1]
		}
	}
}

// Emit logs the wide event and captures it in the buffer.
func (e *Event) Emit(level slog.Level) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.fields["duration_ms"] = time.Since(e.start).Milliseconds()
	e.fields["level"] = level.String()
	e.fields["timestamp"] = e.start.UTC().Format(time.RFC3339Nano)

	attrs := make([]slog.Attr, 0, len(e.fields))
	for k, v := range e.fields {
		attrs = append(attrs, slog.Any(k, v))
	}
	slog.LogAttrs(context.Background(), level, "event", attrs...)

	if globalBuf != nil {
		snapshot := make(map[string]any, len(e.fields))
		for k, v := range e.fields {
			snapshot[k] = v
		}
		globalBuf.append(snapshot)
	}
}

// WithEvent attaches a wide event to a context.
func WithEvent(ctx context.Context, e *Event) context.Context {
	return context.WithValue(ctx, ctxKey{}, e)
}

// FromContext retrieves the wide event from context, or returns a no-op event.
func FromContext(ctx context.Context) *Event {
	if e, ok := ctx.Value(ctxKey{}).(*Event); ok {
		return e
	}
	return New()
}
