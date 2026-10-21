package trace

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

// reset clears global trace state for test isolation.
func reset() {
	sinkMu.Lock()
	defer sinkMu.Unlock()
	sink = nil
	enabled.Store(false)
	globalCounter = 0
}

func TestLogIsNoOpWithUntaggedContext(t *testing.T) {
	reset()

	var buf bytes.Buffer
	SetSink(&buf)

	Log(context.Background(), "test", "key", "val")

	if got := buf.Len(); got != 0 {
		t.Errorf("expected no output with untagged context, got %d bytes", got)
	}
}

func TestStartProducesContextThatLogCanTag(t *testing.T) {
	reset()

	var buf bytes.Buffer
	SetSink(&buf)

	ctx := Start(context.Background(), "test-action")
	Log(ctx, "event-one", "foo", "bar")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines, got %d", len(lines))
	}

	if !strings.Contains(lines[0], "start") || !strings.Contains(lines[0], "action=test-action") {
		t.Errorf("first line should be start line: %q", lines[0])
	}

	if !strings.Contains(lines[1], "event-one") || !strings.Contains(lines[1], "foo=bar") {
		t.Errorf("second line should be Log event: %q", lines[1])
	}

	id1 := strings.Fields(lines[0])[0]
	id2 := strings.Fields(lines[1])[0]
	if id1 != id2 {
		t.Errorf("trace IDs should match: %q vs %q", id1, id2)
	}
}

func TestLogWithDisabledSink(t *testing.T) {
	reset()

	Log(context.Background(), "should-not-appear", "key", "val")
}

func TestLogKeyValueFormatting(t *testing.T) {
	reset()

	var buf bytes.Buffer
	SetSink(&buf)

	ctx := Start(context.Background(), "kv-test")
	Log(ctx, "kv-event", "z_key", "z_val", "a_key", "a_val", "m_key", "m_val")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	var kvLine string
	for _, line := range lines {
		if strings.Contains(line, "kv-event") {
			kvLine = line
			break
		}
	}
	if kvLine == "" {
		t.Fatal("kv-event line not found")
	}

	aIdx := strings.Index(kvLine, "a_key=a_val")
	mIdx := strings.Index(kvLine, "m_key=m_val")
	zIdx := strings.Index(kvLine, "z_key=z_val")
	if aIdx < 0 || mIdx < 0 || zIdx < 0 {
		t.Errorf("expected all key=value pairs in line: %q", kvLine)
	}
	if aIdx > mIdx || mIdx > zIdx {
		t.Errorf("keys should be sorted alphabetically: %q", kvLine)
	}
}

func TestMultipleStartsProduceDistinctIDs(t *testing.T) {
	reset()

	var buf bytes.Buffer
	SetSink(&buf)

	ctx1 := Start(context.Background(), "action-1")
	Log(ctx1, "msg", "n", "1")

	ctx2 := Start(context.Background(), "action-2")
	Log(ctx2, "msg", "n", "2")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) < 4 {
		t.Fatalf("expected 4 lines, got %d", len(lines))
	}

	id1 := strings.Fields(lines[0])[0]
	id2 := strings.Fields(lines[2])[0]
	if id1 == id2 {
		t.Errorf("different Start calls should produce different IDs: %q == %q", id1, id2)
	}
}

func TestLogNonStringValue(t *testing.T) {
	reset()

	var buf bytes.Buffer
	SetSink(&buf)

	ctx := Start(context.Background(), "numeric-test")
	Log(ctx, "num-event", "count", 42, "ratio", 3.14)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	var eventLine string
	for _, line := range lines {
		if strings.Contains(line, "num-event") {
			eventLine = line
			break
		}
	}
	if eventLine == "" {
		t.Fatal("num-event line not found")
	}
	if !strings.Contains(eventLine, "count=42") {
		t.Errorf("expected count=42 in line: %q", eventLine)
	}
	if !strings.Contains(eventLine, "ratio=3.14") {
		t.Errorf("expected ratio=3.14 in line: %q", eventLine)
	}
}

// TestPerTraceRelativeClock verifies that two traces started at different
// times each measure elapsed time relative to their own Start() call,
// not relative to each other or to process launch.
func TestPerTraceRelativeClock(t *testing.T) {
	reset()

	var buf bytes.Buffer
	SetSink(&buf)

	// First trace: starts and logs immediately.
	ctx1 := Start(context.Background(), "slow-action")
	time.Sleep(20 * time.Millisecond)
	Log(ctx1, "delayed", "trace", "1")

	// Second trace: starts after the first's delayed log.
	ctx2 := Start(context.Background(), "fast-action")
	Log(ctx2, "immediate", "trace", "2")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) < 4 {
		t.Fatalf("expected 4 lines, got %d", len(lines))
	}

	// line 0: start for trace 1 (rel ~0.000)
	// line 1: Log for trace 1 (rel ~20ms)
	// line 2: start for trace 2 (rel ~0.000)
	// line 3: Log for trace 2 (rel ~0.000 — no sleep between Start and Log)

	// The second trace's start line should have elapsed ~0.000
	// since its own Start() was called just before logging.
	secondStart := lines[2]
	if !strings.Contains(secondStart, "00:00.000") && !strings.Contains(secondStart, "00:00.00") {
		t.Errorf("second trace start should show ~0 elapsed, got: %q", secondStart)
	}

	// The second trace's Log line should also show ~0 elapsed.
	secondLog := lines[3]
	secondStartFields := strings.Fields(secondStart)
	if len(secondStartFields) > 1 {
		firstElapsed := secondStartFields[1]
		secondLogFields := strings.Fields(secondLog)
		if len(secondLogFields) > 1 {
			secondElapsed := secondLogFields[1]
			// The second trace's Log elapsed should be very close to its start elapsed
			// (both ~0 since no sleep between start and log).
			t.Logf("second start elapsed: %s, second log elapsed: %s", firstElapsed, secondElapsed)
		}
	}

	// The first trace's Log line should show a larger elapsed than the
	// second trace's Log line (because of the 20ms sleep).
	// line 1 = trace1 delayed log, line 3 = trace2 immediate log
	trace1Log := lines[1]
	trace1LogFields := strings.Fields(trace1Log)
	if len(trace1LogFields) > 1 {
		trace1Elapsed := trace1LogFields[1]
		secondLogFields := strings.Fields(secondLog)
		if len(secondLogFields) > 1 {
			trace2Elapsed := secondLogFields[1]
			// trace1Elapsed should be >= 00:00.020 while trace2Elapsed should be ~00:00.000
			t.Logf("trace1 delayed elapsed: %s, trace2 immediate elapsed: %s", trace1Elapsed, trace2Elapsed)
		}
	}
}

func TestIDFromContextReturnsIDForStartedContext(t *testing.T) {
	reset()

	var buf bytes.Buffer
	SetSink(&buf)

	ctx := Start(context.Background(), "id-test")

	id, ok := IDFromContext(ctx)
	if !ok {
		t.Fatal("expected IDFromContext to return ok=true for started context")
	}
	if id == "" {
		t.Fatal("expected non-empty trace ID")
	}
	if !strings.HasPrefix(id, "t") {
		t.Errorf("expected trace ID to start with 't', got %q", id)
	}

	// Verify the returned ID matches what appears in the trace output.
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) < 1 {
		t.Fatal("expected at least one trace line")
	}
	startID := strings.Fields(lines[0])[0]
	if id != startID {
		t.Errorf("IDFromContext returned %q but trace output shows %q", id, startID)
	}
}

func TestIDFromContextReturnsFalseForUntaggedContext(t *testing.T) {
	reset()

	_, ok := IDFromContext(context.Background())
	if ok {
		t.Error("expected IDFromContext to return ok=false for untagged context")
	}
}

func TestIDFromContextPreservedThroughWithCancel(t *testing.T) {
	reset()

	var buf bytes.Buffer
	SetSink(&buf)

	parent := Start(context.Background(), "fetch")
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	id, ok := IDFromContext(ctx)
	if !ok {
		t.Fatal("expected IDFromContext to return ok=true for WithCancel-wrapped context")
	}
	if id == "" {
		t.Fatal("expected non-empty trace ID from WithCancel-wrapped context")
	}

	// Verify it matches the parent's ID.
	parentID, parentOK := IDFromContext(parent)
	if !parentOK {
		t.Fatal("expected parent context to have trace ID")
	}
	if id != parentID {
		t.Errorf("WithCancel context ID %q != parent ID %q", id, parentID)
	}
}

func TestIDFromContextReturnsFalseForNilContext(t *testing.T) {
	reset()

	_, ok := IDFromContext(nil) //nolint:staticcheck // intentionally testing nil handling
	if ok {
		t.Error("expected IDFromContext to return ok=false for nil context")
	}
}

// TestStartLineHasWallClock verifies that the start line carries a
// wall=HH:MM:SS field while subsequent Log lines for the same trace
// do not contain a wall= field.
func TestStartLineHasWallClock(t *testing.T) {
	reset()

	var buf bytes.Buffer
	SetSink(&buf)

	ctx := Start(context.Background(), "wall-test")
	Log(ctx, "first-event", "foo", "bar")
	Log(ctx, "second-event", "baz", "qux")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}

	// Start line should contain wall=HH:MM:SS.
	startLine := lines[0]
	if !strings.Contains(startLine, "wall=") {
		t.Errorf("start line should contain wall= field: %q", startLine)
	}

	// Log lines should NOT contain wall=.
	for i, line := range lines {
		if i == 0 {
			continue // skip start line
		}
		if strings.Contains(line, "wall=") {
			t.Errorf("Log line %d should not contain wall= field: %q", i, line)
		}
	}
}
