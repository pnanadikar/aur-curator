package widgets

import (
	"fmt"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
)

// pagedList returns a list of n packages, pkg00 … pkg(n-1), with the given
// page size. Names sort in index order.
func pagedList(n, pageSize int) LifecycleList {
	entries := make([]LifecycleEntry, n)
	for i := range entries {
		entries[i] = LifecycleEntry{Name: domain.PackageName(fmt.Sprintf("pkg%02d", i))}
	}
	l := NewLifecycleList(entries)
	l.SetPageSize(pageSize)
	return l
}

// selected returns the name under the cursor.
func selected(t *testing.T, l LifecycleList) domain.PackageName {
	t.Helper()
	visible := l.VisibleEntries()
	if l.Cursor() >= len(visible) {
		t.Fatalf("cursor %d outside the %d visible rows", l.Cursor(), len(visible))
	}
	return visible[l.Cursor()].Name
}

// #447 — Down walks every package across pages, and Up walks back to the
// first one: Up on the first row of a page returns to the previous page's
// last row.
func TestCursorUpDown447_WalksAcrossPages(t *testing.T) {
	l := pagedList(7, 3) // pages: 0-2, 3-5, 6
	for i := 1; i < 7; i++ {
		l.CursorDown()
		if got, want := selected(t, l), domain.PackageName(fmt.Sprintf("pkg%02d", i)); got != want {
			t.Fatalf("after %d Down: selected %s, want %s", i, got, want)
		}
	}
	for i := 5; i >= 0; i-- {
		l.CursorUp()
		if got, want := selected(t, l), domain.PackageName(fmt.Sprintf("pkg%02d", i)); got != want {
			t.Fatalf("Up to index %d: selected %s, want %s", i, got, want)
		}
	}
}

// #447 — the ends stay put: Up on the very first row and Down on the very
// last row (a short last page, and a full one) change nothing.
func TestCursorUpDown447_Ends(t *testing.T) {
	tests := []struct {
		name string
		n    int
	}{
		{"short last page", 7},
		{"full last page", 6},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := pagedList(tt.n, 3)
			l.CursorUp()
			if got := selected(t, l); got != "pkg00" {
				t.Errorf("Up on the first row: selected %s, want pkg00", got)
			}
			for i := 0; i < tt.n+2; i++ {
				l.CursorDown()
			}
			want := domain.PackageName(fmt.Sprintf("pkg%02d", tt.n-1))
			if got := selected(t, l); got != want {
				t.Errorf("Down past the last row: selected %s, want %s", got, want)
			}
		})
	}
}
