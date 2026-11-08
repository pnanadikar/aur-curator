package policy

import (
	"sync"
	"testing"

	"github.com/pramod/aur-curator/internal/analyzer"
	"github.com/pramod/aur-curator/internal/domain"
)

// TestEveryAnalyzerCategoryMapped fails if a category an analyzer can
// emit is missing from the default severity table (#475).
func TestEveryAnalyzerCategoryMapped(t *testing.T) {
	if len(analyzer.Categories) == 0 {
		t.Fatal("analyzer.Categories is empty")
	}
	for _, c := range analyzer.Categories {
		if _, ok := defaultSeverity[c]; !ok {
			t.Errorf("analyzer category %q has no entry in defaultSeverity", c)
		}
	}
}

// TestDefaultSeverityHasNoStrayEntries fails if the table holds a
// category no analyzer emits: a stale or misspelled key.
func TestDefaultSeverityHasNoStrayEntries(t *testing.T) {
	emitted := map[string]bool{}
	for _, c := range analyzer.Categories {
		emitted[c] = true
	}
	for c := range defaultSeverity {
		if !emitted[c] {
			t.Errorf("defaultSeverity has %q, which is not in analyzer.Categories", c)
		}
	}
}

// hookRecorder counts unmapped-hook calls per category.
type hookRecorder struct {
	mu    sync.Mutex
	calls map[string]int
}

func newHookRecorder() *hookRecorder { return &hookRecorder{calls: map[string]int{}} }

func (r *hookRecorder) hook(category string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls[category]++
}

func (r *hookRecorder) count(category string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls[category]
}

func (r *hookRecorder) total() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, c := range r.calls {
		n += c
	}
	return n
}

func TestUnmappedCategoryIsHigh(t *testing.T) {
	e := NewEngine(domain.PolicyConfig{Version: "v1"})
	if got := e.Severity("retired_category"); got != domain.SeverityHigh {
		t.Errorf("Severity(unmapped) = %q, want high", got)
	}
}

func TestUnmappedHookOncePerCategory(t *testing.T) {
	rec := newHookRecorder()
	e := NewEngine(domain.PolicyConfig{Version: "v1"}, WithUnmappedHook(rec.hook))

	for range 5 {
		e.Severity("cat_a")
	}
	e.Severity("cat_b")
	e.Severity("cat_b")

	if n := rec.count("cat_a"); n != 1 {
		t.Errorf("hook called %d times for cat_a, want 1", n)
	}
	if n := rec.count("cat_b"); n != 1 {
		t.Errorf("hook called %d times for cat_b, want 1", n)
	}
}

func TestUnmappedHookNotCalledForMapped(t *testing.T) {
	rec := newHookRecorder()
	e := NewEngine(domain.PolicyConfig{Version: "v1"}, WithUnmappedHook(rec.hook))

	for _, c := range analyzer.Categories {
		e.Severity(c)
	}
	if n := rec.total(); n != 0 {
		t.Errorf("hook called %d times for mapped categories, want 0", n)
	}
}

// TestUnmappedHookConcurrent: Severity runs from the UI and from
// operations; the once-per-category guarantee must hold under -race.
func TestUnmappedHookConcurrent(t *testing.T) {
	rec := newHookRecorder()
	e := NewEngine(domain.PolicyConfig{Version: "v1"}, WithUnmappedHook(rec.hook))

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.Severity("cat_concurrent")
		}()
	}
	wg.Wait()

	if n := rec.count("cat_concurrent"); n != 1 {
		t.Errorf("hook called %d times, want 1", n)
	}
}

func TestMapped(t *testing.T) {
	e := NewEngine(domain.PolicyConfig{Version: "v1"})
	for _, c := range analyzer.Categories {
		if !e.Mapped(c) {
			t.Errorf("Mapped(%q) = false, want true", c)
		}
	}
	if e.Mapped("retired_category") {
		t.Error("Mapped(unmapped) = true, want false")
	}
}

func TestAssessUnmappedIsHigh(t *testing.T) {
	e := NewEngine(domain.PolicyConfig{Version: "v1"})
	ra := e.Assess([]domain.Finding{
		{Category: analyzer.CategoryVersionBump},
		{Category: "retired_category"},
	})
	if ra.HighestSeverity != domain.SeverityHigh {
		t.Errorf("HighestSeverity = %q, want high", ra.HighestSeverity)
	}
	if ra.CountBySeverity[domain.SeverityHigh] != 1 || ra.CountBySeverity[domain.SeverityLow] != 1 {
		t.Errorf("CountBySeverity = %v, want 1 high, 1 low", ra.CountBySeverity)
	}
}
