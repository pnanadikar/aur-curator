package ui

import (
	"context"
	"fmt"
	"time"

	"github.com/pramod/aur-curator/internal/domain"
)

// OperationKind identifies the kind of work an Operation represents.
type OperationKind int

const (
	OpFetch OperationKind = iota
	OpAnalyze
	OpBuild
	OpConfirm
	OpMerge
	OpPublish
)

// String returns the human-readable name of kind.
func (k OperationKind) String() string {
	switch k {
	case OpFetch:
		return "fetch"
	case OpAnalyze:
		return "analyze"
	case OpBuild:
		return "build"
	case OpConfirm:
		return "confirm"
	case OpMerge:
		return "merge"
	case OpPublish:
		return "publish"
	default:
		return fmt.Sprintf("kind(%d)", int(k))
	}
}

// Lane identifies one of the four fixed lanes in the Operations Panel.
type Lane string

const (
	LaneFetch   Lane = "fetch"
	LaneAnalyze Lane = "analyze"
	LaneBuild   Lane = "build"
	LanePublish Lane = "publish" // OpConfirm and OpPublish report here
)

// Lane maps an OperationKind to the lane it reports status to. There is
// no Install operation: aur-curator does not install packages
// (workflow.md § Installation, #351).
func (k OperationKind) Lane() Lane {
	switch k {
	case OpFetch, OpMerge:
		return LaneFetch
	case OpAnalyze:
		return LaneAnalyze
	case OpBuild:
		return LaneBuild
	case OpConfirm, OpPublish:
		return LanePublish
	}
	panic("unhandled OperationKind")
}

// Operation represents a single unit of work queued for execution.
type Operation struct {
	ID   string
	Kind OperationKind
	Pkg  domain.PackageName
	// ReviewID is the review an Analyze operation acts on; empty for
	// other kinds (#399).
	ReviewID domain.ReviewID
	TraceCtx context.Context
	Cancel   context.CancelFunc
}

// OperationStatus is the visible status of a single lane.
type OperationStatus string

const (
	StatusIdle      OperationStatus = "idle"
	StatusRunning   OperationStatus = "running"
	StatusCompleted OperationStatus = "completed"
	StatusFailed    OperationStatus = "failed"
	StatusCancelled OperationStatus = "cancelled"
)

// LogEntry is one append-only line in a lane's log.
type LogEntry struct {
	Timestamp time.Time
	Message   string
}

// LaneState is one lane's visible state in the Operations Panel. Only
// one lane can be StatusRunning at a time — this is a read-side view
// over the same single-active-Operation FIFO queue, not a second
// execution model. Log is append-only; entries are never mutated or
// truncated in place. QueueDepth is computed at read time from q.pending,
// never stored as a mutable counter. Pkg records the package name of the
// last operation that ran on this lane, carried forward through
// transitions so the Operations Panel can always identify the lane's
// most recent operation.
//
// NOTE: Live intra-operation streaming (progress ticks mid-flight) is
// out of scope for CMD-056. Only Start and Result/Error lines are
// logged per operation. True streaming build logs belong to Sprint 7's
// dedicated Build Log tab.
type LaneState struct {
	Lane       Lane
	Status     OperationStatus
	QueueDepth int
	Log        []LogEntry
	Pkg        domain.PackageName
	LastKind   string // operation kind label for display; empty when no operation has run
}

// OperationsQueue is a strict FIFO queue. Only one Operation may be
// active at a time.
type OperationsQueue struct {
	pending []Operation
	active  *Operation
	lanes   map[Lane]*LaneState
}

// Enqueue appends op to the back of the queue, unless an Operation
// with the same ID is already pending or active — refusing to enqueue
// a duplicate rather than silently permitting the same package to be
// queued twice, which can cause genuinely repeated load against a
// rate-limited external service (e.g. the AUR git host).
//
// Returns (true, depth) on success where depth is the number of
// operations ahead of the enqueued operation in the queue
// (i.e., len(q.pending) - 1 after appending).  Returns (false, 0)
// if op was a duplicate and therefore not enqueued.
func (q *OperationsQueue) Enqueue(op Operation) (bool, int) {
	if q.active != nil && q.active.ID == op.ID {
		return false, 0
	}
	for _, p := range q.pending {
		if p.ID == op.ID {
			return false, 0
		}
	}
	q.pending = append(q.pending, op)
	depth := len(q.pending) - 1
	if q.active != nil {
		depth++
	}
	return true, depth
}

// IsIdle reports whether there is no active or pending work.
func (q *OperationsQueue) IsIdle() bool {
	return q.active == nil && len(q.pending) == 0
}

// NextPending returns the head operation without removing it, or nil if
// the queue is empty.
func (q *OperationsQueue) NextPending() *Operation {
	if len(q.pending) == 0 {
		return nil
	}
	return &q.pending[0]
}

// LenPending returns the number of items waiting in the queue.
func (q *OperationsQueue) LenPending() int {
	return len(q.pending)
}

// Dequeue moves the head operation from pending to active and returns it.
// Returns the operation and true on success, or a zero-value and false if empty.
func (q *OperationsQueue) Dequeue() (Operation, bool) {
	if q.active != nil {
		return Operation{}, false
	}
	if len(q.pending) == 0 {
		return Operation{}, false
	}
	op := q.pending[0]
	q.pending = q.pending[1:]
	q.active = &op
	return op, true
}

// Complete clears the active operation, returning it to a nil state.
// The caller must have already consumed the result.
func (q *OperationsQueue) Complete() {
	q.active = nil
}

// ensureLane lazily initializes the lane map and the requested lane if
// they have not yet been touched.
func (q *OperationsQueue) ensureLane(l Lane) *LaneState {
	if q.lanes == nil {
		q.lanes = make(map[Lane]*LaneState)
	}
	if _, ok := q.lanes[l]; !ok {
		q.lanes[l] = &LaneState{
			Lane:   l,
			Status: StatusIdle,
			Log:    nil,
		}
	}
	return q.lanes[l]
}

// MarkStarted transitions op's lane to Running and appends a log entry.
func (q *OperationsQueue) MarkStarted(op Operation) {
	ls := q.ensureLane(op.Kind.Lane())
	ls.Status = StatusRunning
	ls.Pkg = op.Pkg
	ls.LastKind = op.Kind.String()
	ls.Log = append(ls.Log, LogEntry{
		Timestamp: time.Now(),
		Message:   op.Kind.String(),
	})
}

// AppendLog appends a log entry to the lane owning op's Kind without
// changing Status. Used for both OperationLogMsg and OperationProgressMsg.
func (q *OperationsQueue) AppendLog(op Operation, message string) {
	ls := q.ensureLane(op.Kind.Lane())
	ls.Log = append(ls.Log, LogEntry{
		Timestamp: time.Now(),
		Message:   message,
	})
}

// MarkCompleted transitions op's lane to Completed and appends a final
// log entry.
func (q *OperationsQueue) MarkCompleted(op Operation, message string) {
	ls := q.ensureLane(op.Kind.Lane())
	ls.Status = StatusCompleted
	ls.LastKind = op.Kind.String()
	ls.Log = append(ls.Log, LogEntry{
		Timestamp: time.Now(),
		Message:   message,
	})
}

// MarkFailed transitions op's lane to Failed and appends the error as
// a log entry.
func (q *OperationsQueue) MarkFailed(op Operation, err error) {
	ls := q.ensureLane(op.Kind.Lane())
	ls.Status = StatusFailed
	ls.LastKind = op.Kind.String()
	ls.Log = append(ls.Log, LogEntry{
		Timestamp: time.Now(),
		Message:   err.Error(),
	})
}

// MarkCancelled transitions op's lane to Cancelled and appends a log entry.
// Unlike MarkFailed, cancellation carries no error detail — the user
// intentionally aborted the operation and stderr is deliberately discarded
// on this path.
func (q *OperationsQueue) MarkCancelled(op Operation) {
	ls := q.ensureLane(op.Kind.Lane())
	ls.Status = StatusCancelled
	ls.LastKind = op.Kind.String()
	ls.Log = append(ls.Log, LogEntry{
		Timestamp: time.Now(),
		Message:   "cancelled by user",
	})
}

// CancelActive signals the currently active operation to stop by
// calling its Cancel function. Returns true if an active operation
// was cancelled, false if there was nothing to cancel. The operation's
// goroutine will detect the cancelled context and return with a
// cancelled-context error, which is handled as OperationCancelledMsg.
func (q *OperationsQueue) CancelActive() bool {
	if q.active == nil || q.active.Cancel == nil {
		return false
	}
	q.active.Cancel()
	return true
}

// ClearPending removes all pending (not-yet-started) operations from
// the queue and returns the list of package names that were removed.
func (q *OperationsQueue) ClearPending() []domain.PackageName {
	if len(q.pending) == 0 {
		return nil
	}
	var pkgs []domain.PackageName
	for _, p := range q.pending {
		pkgs = append(pkgs, p.Pkg)
	}
	q.pending = nil
	return pkgs
}

// LaneState returns the current state of a lane. Never nil — a lane
// not yet touched lazily initializes to StatusIdle with an empty Log.
// QueueDepth is computed at read time from q.pending.
func (q *OperationsQueue) LaneState(l Lane) LaneState {
	ls := *q.ensureLane(l)
	// If an operation is actively running and its lane matches, report running.
	if q.active != nil && q.active.Kind.Lane() == l {
		ls.Status = StatusRunning
	}
	// Compute QueueDepth from the live pending slice — never a stale counter.
	depth := 0
	for _, p := range q.pending {
		if p.Kind.Lane() == l {
			depth++
		}
	}
	ls.QueueDepth = depth
	return ls
}
