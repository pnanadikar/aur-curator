package catalog

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/pramod/aur-curator/internal/domain"
)

func TestPackages(t *testing.T) {
	tests := []struct {
		name         string
		entries      []entry // file/dir names to create (dir=true means directory)
		wantPackages []string
		wantErr      bool
	}{
		{
			name: "mix of directories and files returns only directories",
			entries: []entry{
				{name: "yay", isDir: true},
				{name: "README.md", isDir: false},
				{name: "base", isDir: true},
				{name: "config.json", isDir: false},
			},
			wantPackages: []string{"base", "yay"},
			wantErr:      false,
		},
		{
			name:         "empty directory returns nil slice",
			entries:      nil,
			wantPackages: nil,
			wantErr:      false,
		},
		{
			name:         "only files returns nil slice",
			entries:      []entry{{name: "README.md", isDir: false}, {name: "Makefile", isDir: false}},
			wantPackages: nil,
			wantErr:      false,
		},
		{
			name: "dot directories are skipped",
			entries: []entry{
				{name: "yay", isDir: true},
				{name: ".aur-curator", isDir: true},
				{name: ".git", isDir: true},
				{name: ".hidden", isDir: true},
				{name: "base", isDir: true},
			},
			wantPackages: []string{"base", "yay"},
			wantErr:      false,
		},
		{
			name:         "nonexistent root path returns error",
			entries:      nil,
			wantPackages: nil,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()

			// If the test expects a nonexistent path, use a path outside the temp dir.
			if tt.name == "nonexistent root path returns error" {
				catalog := NewFilesystemCatalog(filepath.Join(root, "does-not-exist"))
				ctx := context.Background()
				pkgs, err := catalog.Packages(ctx)
				if (err != nil) != tt.wantErr {
					t.Fatalf("Packages() error = %v, wantErr = %v", err, tt.wantErr)
				}
				if err == nil && len(pkgs) != 0 {
					t.Errorf("Packages() returned %d packages, want 0 on error", len(pkgs))
				}
				return
			}

			// Create the test entries.
			for _, e := range tt.entries {
				path := filepath.Join(root, e.name)
				if e.isDir {
					if err := os.MkdirAll(path, 0o755); err != nil {
						t.Fatalf("failed to create directory %q: %v", e.name, err)
					}
				} else {
					if err := os.WriteFile(path, nil, 0o644); err != nil {
						t.Fatalf("failed to create file %q: %v", e.name, err)
					}
				}
			}

			catalog := NewFilesystemCatalog(root)
			ctx := context.Background()
			pkgs, err := catalog.Packages(ctx)

			if (err != nil) != tt.wantErr {
				t.Fatalf("Packages() error = %v, wantErr = %v", err, tt.wantErr)
			}
			if len(pkgs) != len(tt.wantPackages) {
				t.Fatalf("Packages() returned %d packages, want %d", len(pkgs), len(tt.wantPackages))
			}
			for i, want := range tt.wantPackages {
				if string(pkgs[i]) != want {
					t.Errorf("Packages()[%d] = %q, want %q", i, pkgs[i], want)
				}
			}
		})
	}
}

type entry struct {
	name  string
	isDir bool
}

func TestPackages_ContextCancelled(t *testing.T) {
	root := t.TempDir()
	// Create a directory so ReadDir wouldn't fail.
	if err := os.MkdirAll(filepath.Join(root, "yay"), 0o755); err != nil {
		t.Fatalf("failed to create test directory: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before calling Packages

	catalog := NewFilesystemCatalog(root)
	pkgs, err := catalog.Packages(ctx)
	if err == nil {
		t.Fatal("Packages() expected context error, got nil")
	}
	if pkgs != nil {
		t.Errorf("Packages() returned non-nil packages on cancelled context: %v", pkgs)
	}
}

func TestRepositoryPath(t *testing.T) {
	tests := []struct {
		name    string
		pkgName string
		root    string
		want    string
		wantErr bool
	}{
		{
			name:    "valid package name returns joined path",
			pkgName: "yay",
			root:    "/tmp/ws",
			want:    "/tmp/ws/yay",
			wantErr: false,
		},
		{
			name:    "valid package name does not require directory to exist",
			pkgName: "nonexistent-pkg",
			root:    "/tmp/ws",
			want:    "/tmp/ws/nonexistent-pkg",
			wantErr: false,
		},
		{
			name:    "empty package name returns error",
			pkgName: "",
			root:    "/tmp/ws",
			want:    "",
			wantErr: true,
		},
		{
			name:    "path traversal returns error",
			pkgName: "../escape",
			root:    "/tmp/ws",
			want:    "",
			wantErr: true,
		},
		{
			name:    "path with separator returns error",
			pkgName: "sub/dir",
			root:    "/tmp/ws",
			want:    "",
			wantErr: true,
		},
		{
			name:    "double-dot in middle returns error",
			pkgName: "foo/../bar",
			root:    "/tmp/ws",
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			catalog := NewFilesystemCatalog(tt.root)
			got, err := catalog.RepositoryPath(domain.PackageName(tt.pkgName))

			if (err != nil) != tt.wantErr {
				t.Fatalf("RepositoryPath() error = %v, wantErr = %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("RepositoryPath() = %q, want %q", got, tt.want)
			}
		})
	}
}
