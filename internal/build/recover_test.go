package build

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/store"
	jsonstore "github.com/pramod/aur-curator/internal/store/json"
)

// #311 — startup recovery of builds left Pending/Running by an earlier run.
// Recovery uses no git and no builder, so both are nil here.

type recoverFixture struct {
	paths  *store.StorePaths
	bstore domain.BuildStore
	engine *Engine
}

func newRecoverFixture(t *testing.T) recoverFixture {
	t.Helper()
	paths := store.NewStorePaths(t.TempDir())
	bstore := jsonstore.NewBuildStore(paths)
	return recoverFixture{paths: paths, bstore: bstore, engine: NewEngine(nil, bstore, jsonstore.NewArtifactStore(paths), nil, paths)}
}

// buildDir creates a build's persistent directory with a log in it.
func (f recoverFixture) buildDir(t *testing.T, id domain.BuildID, log string) {
	t.Helper()
	if err := os.MkdirAll(f.paths.BuildArtifactsDir(id), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.paths.BuildLogPath(id), []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f recoverFixture) save(t *testing.T, pkg domain.PackageName, b *domain.Build) {
	t.Helper()
	if err := f.bstore.Save(pkg, b); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestRecoverInterruptedMarksRunningBuildFailed(t *testing.T) {
	for _, status := range []domain.BuildStatus{domain.BuildRunning, domain.BuildPending} {
		t.Run(string(status), func(t *testing.T) {
			f := newRecoverFixture(t)
			f.buildDir(t, "old-fail", "old failure log\n")
			f.buildDir(t, "interrupted", "partial makepkg output\n")
			f.save(t, "yay", &domain.Build{
				ID:                 "interrupted",
				PackageName:        "yay",
				Status:             status,
				StartTime:          time.Now().Add(-time.Hour),
				LastSuccessBuildID: "old-success",
				LastFailureBuildID: "old-fail",
			})

			recovered, err := f.engine.RecoverInterrupted(context.Background(), []domain.PackageName{"yay"})
			if err != nil {
				t.Fatal(err)
			}
			if len(recovered) != 1 || recovered[0] != "yay" {
				t.Fatalf("recovered = %v, want [yay]", recovered)
			}

			b, err := f.bstore.Get("yay")
			if err != nil {
				t.Fatal(err)
			}
			if b.Status != domain.BuildFailed || b.EndTime == nil {
				t.Fatalf("want Failed with EndTime, got %s / %v", b.Status, b.EndTime)
			}
			if b.LastFailureBuildID != "interrupted" {
				t.Fatalf("LastFailureBuildID = %q, want the interrupted build", b.LastFailureBuildID)
			}
			if b.LastSuccessBuildID != "old-success" {
				t.Fatalf("LastSuccessBuildID changed to %q", b.LastSuccessBuildID)
			}
			if b.LogLocation != f.paths.BuildLogPath("interrupted") {
				t.Fatalf("LogLocation = %q", b.LogLocation)
			}

			log, err := os.ReadFile(f.paths.BuildLogPath("interrupted"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(string(log), "partial makepkg output\n") {
				t.Fatalf("original log content lost: %q", log)
			}
			if !strings.Contains(string(log), "marked failed (interrupted)") {
				t.Fatalf("log lacks the interrupted note: %q", log)
			}

			if exists(f.paths.BuildArtifactsDir("old-fail")) {
				t.Fatal("previous failure's directory should be purged")
			}
			if !exists(f.paths.BuildArtifactsDir("interrupted")) {
				t.Fatal("interrupted build's directory must be kept (it is now the last failure)")
			}
		})
	}
}

func TestRecoverInterruptedLeavesFinishedBuildsAlone(t *testing.T) {
	f := newRecoverFixture(t)
	end := time.Now()
	f.save(t, "ok", &domain.Build{ID: "s1", PackageName: "ok", Status: domain.BuildSuccess, EndTime: &end})
	f.save(t, "bad", &domain.Build{ID: "f1", PackageName: "bad", Status: domain.BuildFailed, EndTime: &end, LastFailureBuildID: "f1"})
	before := map[string][]byte{}
	for _, pkg := range []domain.PackageName{"ok", "bad"} {
		data, err := os.ReadFile(f.paths.BuildPath(pkg))
		if err != nil {
			t.Fatal(err)
		}
		before[string(pkg)] = data
	}

	// "never" has no build record at all.
	recovered, err := f.engine.RecoverInterrupted(context.Background(), []domain.PackageName{"ok", "bad", "never"})
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered) != 0 {
		t.Fatalf("recovered = %v, want none", recovered)
	}
	for pkg, data := range before {
		after, err := os.ReadFile(f.paths.BuildPath(domain.PackageName(pkg)))
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != string(data) {
			t.Fatalf("%s: finished record was rewritten", pkg)
		}
	}
}

func TestRecoverInterruptedWithoutLog(t *testing.T) {
	// Interrupted before the builder created anything: the note becomes
	// the whole log, so the failure is still explained.
	f := newRecoverFixture(t)
	f.save(t, "yay", &domain.Build{ID: "early", PackageName: "yay", Status: domain.BuildRunning})

	if _, err := f.engine.RecoverInterrupted(context.Background(), []domain.PackageName{"yay"}); err != nil {
		t.Fatal(err)
	}
	log, err := os.ReadFile(f.paths.BuildLogPath("early"))
	if err != nil {
		t.Fatalf("log not created: %v", err)
	}
	if !strings.Contains(string(log), "marked failed (interrupted)") {
		t.Fatalf("log = %q", log)
	}
}

func TestRecoverInterruptedIsIdempotent(t *testing.T) {
	f := newRecoverFixture(t)
	f.save(t, "yay", &domain.Build{ID: "b1", PackageName: "yay", Status: domain.BuildRunning})
	ctx := context.Background()
	if r, _ := f.engine.RecoverInterrupted(ctx, []domain.PackageName{"yay"}); len(r) != 1 {
		t.Fatalf("first run recovered %v", r)
	}
	if r, err := f.engine.RecoverInterrupted(ctx, []domain.PackageName{"yay"}); err != nil || len(r) != 0 {
		t.Fatalf("second run: recovered %v, err %v — want nothing", r, err)
	}
}

// saveFailingStore fails every Save; everything else is the real store.
type saveFailingStore struct{ domain.BuildStore }

func (saveFailingStore) Save(domain.PackageName, *domain.Build) error {
	return errors.New("disk full")
}

func TestRecoverInterruptedSaveFailure(t *testing.T) {
	f := newRecoverFixture(t)
	f.buildDir(t, "old-fail", "old failure log\n")
	f.save(t, "yay", &domain.Build{ID: "b1", PackageName: "yay", Status: domain.BuildRunning, LastFailureBuildID: "old-fail"})
	engine := NewEngine(nil, saveFailingStore{f.bstore}, jsonstore.NewArtifactStore(f.paths), nil, f.paths)

	recovered, err := engine.RecoverInterrupted(context.Background(), []domain.PackageName{"yay"})
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("want the save error, got %v", err)
	}
	if len(recovered) != 0 {
		t.Fatalf("a package whose record was not saved must not be reported: %v", recovered)
	}
	// The record on disk still points at old-fail, so it must survive.
	if !exists(f.paths.BuildArtifactsDir("old-fail")) {
		t.Fatal("previous failure's directory was purged although the save failed")
	}
}
