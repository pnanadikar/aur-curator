// Package trace provides a lightweight structured trace-logging facility.
//
// Trace IDs are minted by Start and carried via context. Log calls with
// a context that carries no trace ID are no-op (never panic or error).
// The package is binary on/off: when tracing is disabled, Log returns
// immediately with no allocation or formatting.
//
// Output is plain text in a logfmt-inspired key=value format, with a
// relative timestamp since the individual trace's Start() call. Example
// line shape:
//
//	t7  00:00.012  key  action=refresh
//
// Start lines also carry a wall-clock timestamp for correlation with
// other log sources.
package trace

import (
	"context"
	"fmt"
	"io"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// ctxKey is an unexported type used as the key for trace sessions in context values.
type ctxKey struct{}

// traceSession carries a trace ID and its own per-trace start time.
// Each Start() call creates a new session so that elapsed times are
// measured relative to that specific Start() invocation, not to
// process launch or the first-ever trace.
type traceSession struct {
	id    string
	start time.Time
}

// globalCounter is the shared atomic counter for trace IDs.
var globalCounter uint64

// newTraceID generates the next incrementing trace ID.
// IDs are short identifiers like t1, t2, t3.
func newTraceID() string {
	n := atomic.AddUint64(&globalCounter, 1) - 1
	return fmt.Sprintf("t%d", n)
}

// sink is the optional output destination. Set via SetSink in Stage 2.
var (
	sinkMu  sync.Mutex
	sink    io.Writer
	enabled atomic.Bool
)

// SetSink configures the output destination for trace lines.
// Call this once during startup when --trace is enabled.
func SetSink(w io.Writer) {
	sinkMu.Lock()
	defer sinkMu.Unlock()
	sink = w
	if w != nil {
		enabled.Store(true)
	} else {
		enabled.Store(false)
	}
}

// Start mints a new trace ID, stores a traceSession (id + per-trace start
// time) in a new context derived from ctx, and writes a "start" line to
// the sink if one is configured.
// The action parameter describes what is being traced.
//
// The start line carries a wall-clock timestamp (wall=HH:MM:SS) so that
// this trace can be correlated against other log sources such as
// --debug-log. Subsequent Log lines for the same trace use only
// relative timing.
func Start(ctx context.Context, action string) context.Context {
	tid := newTraceID()
	ts := traceSession{
		id:    tid,
		start: time.Now(),
	}

	ctx = context.WithValue(ctx, ctxKey{}, ts)

	if enabled.Load() {
		rel := formatElapsed(ts.start)
		wall := ts.start.Format("15:04:05")
		sinkMu.Lock()
		defer sinkMu.Unlock()
		if sink != nil {
			_, _ = fmt.Fprintf(sink, "%-4s %-12s %-5s %s wall=%s\n", tid, rel, "start", "action="+action, wall)
		}
	}

	return ctx
}

// Log records one trace line tagged with whatever trace ID is on ctx.
// No-op-safe: if ctx carries no trace ID, Log does nothing.
// When tracing is disabled globally (no sink set), Log returns immediately.
//
// kv is a list of alternating key, value arguments (key must be string).
// Values are formatted via fmt.Sprintf("%v", v). Keys are sorted for
// deterministic output.
func Log(ctx context.Context, event string, kv ...any) {
	if !enabled.Load() || ctx == nil {
		return
	}

	ts, ok := ctx.Value(ctxKey{}).(traceSession)
	if !ok {
		return
	}

	tidStr := ts.id
	if tidStr == "" {
		return
	}

	// Parse kv pairs and sort keys for deterministic output.
	type kvPair struct {
		key   string
		value string
	}
	var pairs []kvPair
	for i := 0; i+1 < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if !ok {
			continue
		}
		pairs = append(pairs, kvPair{key: k, value: fmt.Sprintf("%v", kv[i+1])})
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].key < pairs[j].key
	})

	var sb string
	for _, p := range pairs {
		sb += p.key + "=" + p.value + " "
	}
	if len(sb) > 0 {
		sb = sb[:len(sb)-1]
	}

	rel := formatElapsed(ts.start)

	sinkMu.Lock()
	defer sinkMu.Unlock()
	if sink != nil {
		_, _ = fmt.Fprintf(sink, "%-4s %-12s %-5s %s\n", tidStr, rel, event, sb)
	}
}

// IDFromContext returns the trace ID (e.g. "t3") stored in ctx by Start().
// It returns ("", false) if the context carries no trace session.
func IDFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	ts, ok := ctx.Value(ctxKey{}).(traceSession)
	if !ok {
		return "", false
	}
	return ts.id, true
}

// formatElapsed returns a human-readable relative duration since t,
// formatted as mm:ss.mmm.
func formatElapsed(t time.Time) string {
	if t.IsZero() {
		return "00:00.000"
	}
	elapsed := time.Since(t)
	secs := int(elapsed.Seconds())
	mins := secs / 60
	secs = secs % 60
	msec := elapsed.Milliseconds() % 1000
	return fmt.Sprintf("%02d:%02d.%03d", mins, secs, msec)
}
