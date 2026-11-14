package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pramod/aur-curator/internal/catalog"
	"github.com/pramod/aur-curator/internal/pkgname"
)

// check reports a directory with an invalid name, in the workspace root
// and in the store, and deletes neither: before #504 the store directory
// was removed as an orphan.
func TestCheckInvalidNames504(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "ws")
	repo := filepath.Join(dir, "repo")
	badStore := filepath.Join(root, ".aur-curator", "packages", "Bad")
	for _, d := range []string{filepath.Join(root, "Foo"), badStore, repo} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	cfg := filepath.Join(dir, "cfg.json")
	body := fmt.Sprintf(`{"rootPath": %q, "repositoryPath": %q, "giteaBaseURL": "https://gitea.invalid", "giteaOrg": "o", "giteaToken": "t"}`, root, repo)
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runMainOut(t, cfg, "check")
	if code != 1 {
		t.Errorf("exit %d, want 1\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	for _, want := range []string{
		"Check 1 — invalid package names (report-only): 2\n",
		`workspace directory "Foo": invalid package name "Foo": the AUR allows`,
		`store directory "Bad": store: invalid package name "Bad": the AUR allows`,
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	if _, err := os.Stat(badStore); err != nil {
		t.Errorf("store directory with an invalid name was deleted: %v", err)
	}
	if strings.Contains(stderr, `removed orphaned package dir "Bad"`) {
		t.Errorf("check removed it as an orphan:\n%s", stderr)
	}
}

// update lists a rejected directory as not checked, with the rule (#504).
func TestRejectedReasons504(t *testing.T) {
	got := rejectedReasons([]catalog.Rejected{{Name: "Foo", Err: pkgname.Check("Foo")}})
	if len(got) != 1 || got[0].pkg != "Foo" || !strings.HasPrefix(got[0].reason, `invalid package name "Foo": the AUR allows`) {
		t.Errorf("rejectedReasons = %+v", got)
	}
}

// writeConfig504 writes a config whose rootPath holds only the directory
// "Foo", an invalid name, and returns its path. With no valid package,
// update and outdated stop before any AUR request.
func writeConfig504(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, "ws")
	repo := filepath.Join(dir, "repo")
	for _, d := range []string{filepath.Join(root, "Foo"), filepath.Join(root, ".aur-curator"), repo} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	cfg := filepath.Join(dir, "cfg.json")
	body := fmt.Sprintf(`{"rootPath": %q, "repositoryPath": %q, "giteaBaseURL": "https://gitea.invalid", "giteaOrg": "o", "giteaToken": "t"}`, root, repo)
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestUpdateNamesRejected504(t *testing.T) {
	stdout, stderr, code := runMainOut(t, writeConfig504(t), "update")
	want := "Not checked: Foo: invalid package name \"Foo\": the AUR allows lowercase letters, digits and . + _ -, starting with a letter or digit.\nNo curated packages found.\n"
	if code != 0 || stdout != want {
		t.Errorf("exit %d, stdout:\n%s\nwant:\n%s\nstderr:\n%s", code, stdout, want, stderr)
	}
}

func TestOutdatedNamesRejected504(t *testing.T) {
	stdout, stderr, code := runMainOut(t, writeConfig504(t), "outdated")
	if code != exitOutdatedOK || stdout != "" || !strings.HasPrefix(stderr, `Foo: invalid package name "Foo": the AUR allows`) {
		t.Errorf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}
