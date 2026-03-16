// Package logging implements the wide event / canonical log line pattern.
// Instead of scattered log statements, each request or operation builds
// a single structured event with all context, emitted once at completion.
package logging

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

type ctxKey struct{}

// Event is a wide event that accumulates context throughout a request or operation.
type Event struct {
	mu     sync.Mutex
	fields map[string]any
	start  time.Time
}

// Buffer is a thread-safe ring buffer that captures emitted events for the /admin/logs endpoint.
type Buffer struct {
	mu     sync.Mutex
	events []map[string]any
	size   int
	cursor int
	count  int
}

var globalBuf *Buffer

// NewBuffer creates a ring buffer that holds the last n events.
func NewBuffer(n int) *Buffer {
	return &Buffer{events: make([]map[string]any, n), size: n}
}

// SetBuffer configures the global event buffer. Call once at startup.
func SetBuffer(b *Buffer) { globalBuf = b }

func (b *Buffer) append(fields map[string]any) {
	b.mu.Lock()
	defer b.mu.Unlock()
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

// New creates a new wide event with the given initial key-value pairs.
func New(initial ...any) *Event {
	e := &Event{fields: make(map[string]any), start: time.Now()}
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
