package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	aurlog "github.com/pramod/aur-curator/internal/activitylog"
	"github.com/pramod/aur-curator/internal/aurinfo"
	"github.com/pramod/aur-curator/internal/build"
	"github.com/pramod/aur-curator/internal/catalog"
	"github.com/pramod/aur-curator/internal/config"
	"github.com/pramod/aur-curator/internal/debug"
	"github.com/pramod/aur-curator/internal/deps"
	"github.com/pramod/aur-curator/internal/domain"
	"github.com/pramod/aur-curator/internal/errorlog"
	"github.com/pramod/aur-curator/internal/git"
	"github.com/pramod/aur-curator/internal/help"
	"github.com/pramod/aur-curator/internal/instancelock"
	"github.com/pramod/aur-curator/internal/lifecycle"
	"github.com/pramod/aur-curator/internal/review"
	"github.com/pramod/aur-curator/internal/store"
	"github.com/pramod/aur-curator/internal/trace"
	"github.com/pramod/aur-curator/internal/ui"
	"github.com/pramod/aur-curator/internal/version"
)

const traceLogName = ".aur-curator" + string(filepath.Separator) + "trace.log"

func main() {
	if showHelp(os.Args[1:]) {
		os.Exit(0)
	}

	debugLogFile, traceFile, positional := extractFlags(os.Args[1:])

	if debugLogFile != "" {
		if f, err := os.OpenFile(debugLogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
			debug.Set(f)
		}
	} else if os.Getenv("AUR_CURATOR_DEBUG") != "" {
		debug.Set(os.Stderr)
	}

	if len(positional) < 1 {
		usage()
		os.Exit(2)
	}
	configPath := positional[0]

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	// Wire trace sink after config so we can resolve .aur-curator/
	// inside the workspace directory.
	tracePath := traceFile
	if tracePath == "" && os.Getenv("AUR_CURATOR_TRACE") != "" {
		tracePath = filepath.Join(cfg.RootPath, traceLogName)
	}
	if tracePath != "" {
		if f, err := os.OpenFile(tracePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
			trace.SetSink(f)
		} else {
			fmt.Fprintf(os.Stderr, "trace: cannot open %s: %v\n", tracePath, err)
			os.Exit(1)
		}
	}

	pkgCatalog := catalog.NewFilesystemCatalog(cfg.RootPath)
	forge := git.NewGiteaForge(cfg.GiteaBaseURL, cfg.GiteaOrg, cfg.GiteaToken.Value())
	provider := git.New(pkgCatalog, git.ExecRunner{}, forge, cfg.RootPath)

	switch {
	case len(positional) == 1:
		runTUI(cfg, pkgCatalog, provider)
	case len(positional) == 3 && positional[1] == "import":
		runImport(cfg, pkgCatalog, provider, positional[2])
	case len(positional) == 3 && positional[1] == "remove":
		runRemove(cfg, pkgCatalog, provider, forge, positional[2])
	case len(positional) == 2 && positional[1] == "check":
		runCheck(cfg, pkgCatalog, provider)
	case len(positional) == 2 && positional[1] == "update":
		runUpdate(cfg, pkgCatalog, provider)
	default:
		usage()
		os.Exit(2)
	}
}

// knownFlags lists flag names recognized by aur-curator.
var knownFlags = []string{"debug-log", "trace"}

func usage() {
	name := filepath.Base(os.Args[0])
	fmt.Fprintf(os.Stderr,
		"usage: %s [flags] <config-file>\n       %s [flags] <config-file> import <package-name>\n       %s [flags] <config-file> remove <package-name>\n       %s [flags] <config-file> check\n       %s [flags] <config-file> update\n",
		name, name, name, name, name)
}

// showHelp checks if --help, -h, --version, or -version appears in args
// and prints usage or version if so.
func showHelp(args []string) bool {
	for _, a := range args {
		if a == "--help" || a == "-h" {
			rendered, err := help.Render(80, false)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			fmt.Print(rendered)
			return true
		}
		if a == "--version" || a == "-version" {
			fmt.Println(version.Version)
			return true
		}
	}
	return false
}

// extractFlags scans args for known flags (--name value or --name=value)
// and returns the debug-log value, trace value, and the remaining positional arguments.
func extractFlags(args []string) (debugValue string, traceValue string, positional []string) {
	i := 0
	for i < len(args) {
		arg := args[i]
		isFlag := false

		// Check --name=value form
		for _, name := range knownFlags {
			prefix := "--" + name + "="
			if strings.HasPrefix(arg, prefix) {
				switch name {
				case "debug-log":
					debugValue = strings.TrimPrefix(arg, prefix)
				case "trace":
					traceValue = strings.TrimPrefix(arg, prefix)
				}
				isFlag = true
				break
			}
			prefix = "-" + name + "="
			if strings.HasPrefix(arg, prefix) {
				switch name {
				case "debug-log":
					debugValue = strings.TrimPrefix(arg, prefix)
				case "trace":
					traceValue = strings.TrimPrefix(arg, prefix)
				}
				isFlag = true
				break
			}
		}
		if isFlag {
			i++
			continue
		}

		// Check --name value form
		for _, name := range knownFlags {
			if arg == "--"+name || arg == "-"+name {
				if i+1 < len(args) {
					value := args[i+1]
					switch name {
					case "debug-log":
						debugValue = value
					case "trace":
						traceValue = value
					}
					i += 2
				} else {
					// No value provided — flag is present but empty
					switch name {
					case "trace":
						traceValue = ""
					case "debug-log":
						debugValue = ""
					}
					i++
				}
				isFlag = true
				break
			}
		}
		if isFlag {
			continue
		}

		positional = append(positional, arg)
		i++
	}
	return debugValue, traceValue, positional
}

// runImport performs a one-shot Import and exits — no TUI. After
// the git import succeeds it eagerly creates a Review record via
// EnsureReview so the package lands at Review Pending immediately
// rather than sitting at Not Reviewed until the Review screen is opened.
func runImport(
	cfg domain.WorkspaceConfig,
	pkgCatalog catalog.PackageCatalog,
	provider git.Provider,
	name string,
) {
	release := acquireWorkspaceLock(cfg, "aur-curator import")
	defer func() { _ = release() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if err := provider.Import(ctx, domain.PackageName(name)); err != nil {
		_ = aurlog.Log(ctx, cfg.ActivityLogPath, "import", name, "status", "failed", "error", err.Error())
		fmt.Fprintf(os.Stderr, "import: %v\n", err)
		os.Exit(1)
	}

	d := deps.NewDependencies(cfg, provider, pkgCatalog)

	if _, err := d.Reviews.EnsureReview(ctx, domain.PackageName(name)); err != nil {
		_ = aurlog.Log(ctx, cfg.ActivityLogPath, "import", name, "status", "failed", "error", err.Error())
		fmt.Fprintf(os.Stderr, "import: review creation failed: %v\n", err)
		os.Exit(1)
	}

	autoAnalyzeMsg := ""
	if cfg.AutoAnalyzeAfterImport {
		if analysis, err := d.Reviews.RunAnalyzers(ctx, domain.PackageName(name)); err != nil {
			_ = aurlog.Log(ctx, cfg.ActivityLogPath, "import", name, "status", "success", "auto_analyze", "failed", "error", err.Error())
			autoAnalyzeMsg = fmt.Sprintf(" Analysis failed: %v.", err)
		} else {
			_ = aurlog.Log(ctx, cfg.ActivityLogPath, "import", name, "status", "success", "auto_analyze", "complete", "findings", fmt.Sprintf("%d", len(analysis.Findings)))
			autoAnalyzeMsg = fmt.Sprintf(" Analysis complete (%d findings).", len(analysis.Findings))
		}
	}

	_ = aurlog.Log(ctx, cfg.ActivityLogPath, "import", name, "status", "success")

	fmt.Printf("Imported %q. Review Pending.%s\n", name, autoAnalyzeMsg)
}

// workspaceLockPath is the single-instance lock shared by the TUI and the
// CLI commands that change the workspace (#311, #334).
func workspaceLockPath(cfg domain.WorkspaceConfig) string {
	return filepath.Join(cfg.RootPath, ".aur-curator", "workspace.lock")
}

// acquireWorkspaceLock takes the workspace lock or exits with a message
// naming cmd. The lock is released by the kernel when the process exits,
// including through os.Exit, so callers need not release it on error paths.
func acquireWorkspaceLock(cfg domain.WorkspaceConfig, cmd string) func() error {
	release, err := instancelock.Acquire(workspaceLockPath(cfg))
	if err != nil {
		if errors.Is(err, instancelock.ErrHeld) {
			fmt.Fprintf(os.Stderr, "%s: %v — quit it first\n", cmd, err)
		} else {
			fmt.Fprintf(os.Stderr, "%s: %v\n", cmd, err)
		}
		os.Exit(1)
	}
	return release
}

func runTUI(cfg domain.WorkspaceConfig, pkgCatalog catalog.PackageCatalog, provider git.Provider) {
	// One TUI per workspace (#311): startup recovery of interrupted builds
	// is only safe when no other instance can be building.
	release := acquireWorkspaceLock(cfg, "aur-curator")
	defer func() { _ = release() }()

	d := deps.NewDependencies(cfg, provider, pkgCatalog)

	// Lock held, nothing started yet: builds left Pending/Running by an
	// earlier run cannot be live (#311).
	notice := ""
	if d.BuildEngine != nil {
		notice = recoverInterruptedBuilds(cfg, pkgCatalog, d.BuildEngine)
	}

	app := ui.NewApp(d).WithStartupNotice(notice)
	if _, err := tea.NewProgram(app, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "run app: %v\n", err)
		os.Exit(1)
	}
}

// interruptedBuildRecoverer is the part of build.Engine startup recovery uses.
type interruptedBuildRecoverer interface {
	RecoverInterrupted(ctx context.Context, pkgs []domain.PackageName) ([]domain.PackageName, error)
}

// recoverInterruptedBuilds marks builds left Pending/Running by an earlier
// run as failed (#311), logs each one to the activity log, and returns a
// one-line notice for the UI ("" if there was nothing to report). Must be
// called with the TUI instance lock held, before the program starts.
func recoverInterruptedBuilds(cfg domain.WorkspaceConfig, pkgCatalog catalog.PackageCatalog, engine interruptedBuildRecoverer) string {
	ctx := trace.Start(context.Background(), "recover-interrupted")
	pkgs, err := pkgCatalog.Packages(ctx)
	if err != nil {
		_ = errorlog.Log(ctx, cfg.RootPath, "recover", fmt.Sprintf("list packages: %v", err))
		return fmt.Sprintf("Could not check for interrupted builds: %v", err)
	}

	recovered, err := engine.RecoverInterrupted(ctx, pkgs)
	names := make([]string, len(recovered))
	for i, pkg := range recovered {
		names[i] = string(pkg)
		_ = aurlog.Log(ctx, cfg.ActivityLogPath, "build", string(pkg), "status", "interrupted")
	}

	var parts []string
	if len(recovered) > 0 {
		parts = append(parts, fmt.Sprintf("Recovered %d interrupted build(s), marked failed: %s", len(recovered), strings.Join(names, ", ")))
	}
	if err != nil {
		_ = errorlog.Log(ctx, cfg.RootPath, "recover", err.Error())
		parts = append(parts, fmt.Sprintf("could not recover some interrupted builds: %v", err))
	}
	notice := strings.Join(parts, "; ")
	trace.Log(ctx, "recover-interrupted", "packages", len(pkgs), "recovered", len(recovered), "notice", notice)
	return notice
}

func runRemove(
	cfg domain.WorkspaceConfig,
	pkgCatalog catalog.PackageCatalog,
	provider git.Provider,
	forge git.Forge,
	name string,
) {
	release := acquireWorkspaceLock(cfg, "aur-curator remove")
	defer func() { _ = release() }()

	ctx := context.Background()
	ctx = trace.Start(ctx, "remove")

	pkgName := domain.PackageName(name)
	paths := store.NewStorePaths(cfg.RootPath)

	// Step 1: Pre-flight existence check
	repoPath, err := pkgCatalog.RepositoryPath(pkgName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "remove %q: catalog: %v\n", name, err)
		_ = errorlog.Log(ctx, cfg.RootPath, "remove", fmt.Sprintf("pre-flight catalog lookup failed: %v", err))
		_ = aurlog.Log(ctx, cfg.ActivityLogPath, "remove", name, "status", "failed", "error", err.Error())
		os.Exit(1)
	}
	if _, err := os.Stat(repoPath); err != nil {
		fmt.Fprintf(os.Stderr, "remove %q: package not found at %q\n", name, repoPath)
		_ = errorlog.Log(ctx, cfg.RootPath, "remove", fmt.Sprintf("package directory not found: %s", repoPath))
		_ = aurlog.Log(ctx, cfg.ActivityLogPath, "remove", name, "status", "not_found", "path", repoPath)
		os.Exit(1)
	}

	// Step 2: Confirmation prompt
	builds := deps.NewDependencies(cfg, provider, pkgCatalog)

	// aur-curator never uninstalls (workflow.md § Remove Mechanics, #351):
	// it only checks whether the package is installed and, if so, tells the
	// user how to remove it. pacman -Q needs no root.
	installedVersion, installedErr := pacmanInstalledVersion(ctx, name)

	fmt.Printf("This will delete:\n")
	fmt.Printf("  - Package directory: %s\n", repoPath)
	fmt.Printf("  - Store metadata: %s\n", paths.PackageDir(pkgName))
	fmt.Printf("  - Build binaries in: %s\n", paths.BuildsDir())
	fmt.Printf("  - Gitea repository: %s/%s\n", cfg.GiteaOrg, name)

	fmt.Printf("It will not uninstall the package.\n")
	switch {
	case installedErr != nil:
		fmt.Printf("  Could not check whether %s is installed: %v\n", name, installedErr)
	case installedVersion != "":
		fmt.Printf("  Installed: %s — stays installed\n", installedVersion)
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("Continue? [y/N]: ")
	answer, err := reader.ReadString('\n')
	if err != nil || !strings.HasPrefix(strings.ToLower(strings.TrimSpace(answer)), "y") {
		fmt.Printf("Cancelled.\n")
		_ = aurlog.Log(ctx, cfg.ActivityLogPath, "remove", name, "status", "cancelled", "reason", "user_abort")
		os.Exit(0)
	}

	// Step 3: record whether the package is installed (never uninstalled).
	switch {
	case installedErr != nil:
		_ = errorlog.Log(ctx, cfg.RootPath, "remove", fmt.Sprintf("remove %s: installed check failed: %v", name, installedErr))
		_ = aurlog.Log(ctx, cfg.ActivityLogPath, "remove", name, "installed", "unknown", "error", installedErr.Error())
	case installedVersion != "":
		_ = aurlog.Log(ctx, cfg.ActivityLogPath, "remove", name, "installed", "yes", "version", installedVersion)
	default:
		_ = aurlog.Log(ctx, cfg.ActivityLogPath, "remove", name, "installed", "no")
	}

	// Step 4: Gitea remote deletion (best-effort)
	remoteErr := forge.DeleteRepository(ctx, name)
	if remoteErr != nil {
		_ = errorlog.Log(ctx, cfg.RootPath, "remove", fmt.Sprintf("remote delete failed: %v", remoteErr))
		_ = aurlog.Log(ctx, cfg.ActivityLogPath, "remove", name, "remote_delete", "failed", "error", remoteErr.Error())
	} else {
		_ = aurlog.Log(ctx, cfg.ActivityLogPath, "remove", name, "remote_delete", "success")
	}

	// Step 5: Build binary cleanup
	ids, err := packageBuildRefs(builds.BuildDB, pkgName)
	if err != nil {
		_ = errorlog.Log(ctx, cfg.RootPath, "remove", fmt.Sprintf("build dirs unknown: %v", err))
		_ = aurlog.Log(ctx, cfg.ActivityLogPath, "remove", name, "build_binaries_delete", "failed", "error", err.Error())
	} else {
		for _, id := range ids {
			if err := os.RemoveAll(paths.BuildArtifactsDir(id)); err != nil {
				_ = errorlog.Log(ctx, cfg.RootPath, "remove", fmt.Sprintf("build dir delete failed (build_id=%s): %v", id, err))
				_ = aurlog.Log(ctx, cfg.ActivityLogPath, "remove", name, "build_binaries_delete", "failed", "build_id", string(id), "error", err.Error())
			} else {
				_ = aurlog.Log(ctx, cfg.ActivityLogPath, "remove", name, "build_binaries_delete", "success", "build_id", string(id))
			}
		}
	}

	// Step 6: Store record deletion
	if err := builds.ReviewDB.Delete(pkgName); err != nil {
		_ = errorlog.Log(ctx, cfg.RootPath, "remove", fmt.Sprintf("review delete failed: %v", err))
		_ = aurlog.Log(ctx, cfg.ActivityLogPath, "remove", name, "review_delete", "failed", "error", err.Error())
	} else {
		_ = aurlog.Log(ctx, cfg.ActivityLogPath, "remove", name, "review_delete", "success")
	}

	if err := builds.BuildDB.Delete(pkgName); err != nil {
		_ = errorlog.Log(ctx, cfg.RootPath, "remove", fmt.Sprintf("build delete failed: %v", err))
		_ = aurlog.Log(ctx, cfg.ActivityLogPath, "remove", name, "build_delete", "failed", "error", err.Error())
	} else {
		_ = aurlog.Log(ctx, cfg.ActivityLogPath, "remove", name, "build_delete", "success")
	}

	artifacts, err := builds.ArtifactDB.GetArtifactsByPackage(pkgName)
	if err != nil {
		_ = errorlog.Log(ctx, cfg.RootPath, "remove", fmt.Sprintf("artifact list failed: %v", err))
		_ = aurlog.Log(ctx, cfg.ActivityLogPath, "remove", name, "artifact_delete", "failed", "error", err.Error())
	}
	for _, a := range artifacts {
		if err := builds.ArtifactDB.Delete(a.ID); err != nil {
			_ = errorlog.Log(ctx, cfg.RootPath, "remove", fmt.Sprintf("artifact delete failed (id=%s): %v", a.ID, err))
			_ = aurlog.Log(ctx, cfg.ActivityLogPath, "remove", name, "artifact_delete", "failed", "error", err.Error())
		} else {
			_ = aurlog.Log(ctx, cfg.ActivityLogPath, "remove", name, "artifact_delete", "success", "artifact_id", string(a.ID))
		}
	}

	pkgDir := paths.PackageDir(pkgName)
	if err := os.RemoveAll(pkgDir); err != nil {
		_ = errorlog.Log(ctx, cfg.RootPath, "remove", fmt.Sprintf("store metadata delete failed: %v", err))
		_ = aurlog.Log(ctx, cfg.ActivityLogPath, "remove", name, "store_metadata_delete", "failed", "path", pkgDir, "error", err.Error())
	} else {
		_ = aurlog.Log(ctx, cfg.ActivityLogPath, "remove", name, "store_metadata_delete", "success", "path", pkgDir)
	}

	// Step 7: Local clone directory removal
	if err := os.RemoveAll(repoPath); err != nil {
		_ = errorlog.Log(ctx, cfg.RootPath, "remove", fmt.Sprintf("clone delete failed: %v", err))
		_ = aurlog.Log(ctx, cfg.ActivityLogPath, "remove", name, "status", "partial", "error", "clone_delete: "+err.Error())
	} else {
		_ = aurlog.Log(ctx, cfg.ActivityLogPath, "remove", name, "status", "success")
		fmt.Printf("Removed %q.\n", name)
	}

	switch {
	case installedErr != nil:
		fmt.Printf("Could not check whether %s is installed: %v\n", name, installedErr)
	case installedVersion != "":
		fmt.Printf("Still installed: run sudo pacman -Rs %s\n", name)
	}
}

// pacmanInstalledVersion reports the installed version of name, or "" if
// it is not installed. It never needs root and never changes the system.
func pacmanInstalledVersion(ctx context.Context, name string) (string, error) {
	out, err := exec.CommandContext(ctx, "pacman", "-Q", name).Output()
	return interpretPacmanQ(out, err)
}

// interpretPacmanQ turns the result of `pacman -Q <name>` into an installed
// version. pacman exits 1 when the package is not installed; any other
// failure (pacman missing, another exit code) is an error, never reported
// as "not installed" (#351).
func interpretPacmanQ(out []byte, err error) (string, error) {
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return "", nil
		}
		return "", fmt.Errorf("pacman -Q: %w", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) < 2 {
		return "", fmt.Errorf("pacman -Q: unexpected output %q", strings.TrimSpace(string(out)))
	}
	return fields[1], nil
}

func runCheck(
	cfg domain.WorkspaceConfig,
	pkgCatalog catalog.PackageCatalog,
	provider git.Provider,
) {
	release := acquireWorkspaceLock(cfg, "aur-curator check")
	defer func() { _ = release() }()

	d := deps.NewDependencies(cfg, provider, pkgCatalog)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	ctx = trace.Start(ctx, "check")

	paths := store.NewStorePaths(cfg.RootPath)

	var (
		packagesChecked       int
		check1OrphanFixes     int
		check1MissingRev      int
		check1Corrupted       int
		check2DanglingRefs    int
		check2OrphanArts      int
		check2StaleArts       int
		check2OrphanBuildDirs int
		check3MissingBins     int
		check3RepoFiles       int
		check3NotMarked       int
		check3NotInRepo       int
		findings              []string
	)

	// ---- Check 0: workspace root and .aur-curator/ accessibility (fail-fast) ----
	if _, err := os.Stat(cfg.RootPath); err != nil {
		fmt.Fprintf(os.Stderr, "check 0: workspace root %q does not exist: %v\n", cfg.RootPath, err)
		os.Exit(1)
	}
	aurDir := filepath.Join(cfg.RootPath, ".aur-curator")
	if info, err := os.Stat(aurDir); err != nil {
		fmt.Fprintf(os.Stderr, "check 0: %q not found: %v\n", aurDir, err)
		os.Exit(1)
	} else if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "check 0: %q exists but is not a directory\n", aurDir)
		os.Exit(1)
	}
	trace.Log(ctx, "check0", "state", "passed")

	// Fetch catalog and build lookup set
	packages, err := pkgCatalog.Packages(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "check: catalog error: %v\n", err)
		_ = errorlog.Log(ctx, cfg.RootPath, "check", "catalog.Packages failed: "+err.Error())
		os.Exit(1)
	}
	packagesChecked = len(packages)

	catSet := make(map[domain.PackageName]bool, len(packages))
	for _, pkg := range packages {
		catSet[pkg] = true
	}

	// ---- Check 1: catalog ↔ store review-record cross-check ----
	ctx = trace.Start(ctx, "check1")

	// Forward: catalog → store (report-only)
	for _, pkg := range packages {
		if _, err := d.ReviewDB.Get(pkg); err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				check1MissingRev++
				findings = append(findings, fmt.Sprintf("package %q: no Review record", pkg))
			} else {
				check1Corrupted++
				findings = append(findings, fmt.Sprintf("package %q: corrupted review record: %v", pkg, err))
			}
		}
	}

	// Reverse: stores → catalog (auto-fix orphaned package dirs)
	storePkgDirs, err := os.ReadDir(paths.PackagesDir())
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			_ = errorlog.Log(ctx, cfg.RootPath, "check", "cannot read packages directory: "+err.Error())
		}
	} else {
		for _, entry := range storePkgDirs {
			if !entry.IsDir() {
				continue
			}
			pkgName := domain.PackageName(entry.Name())
			if catSet[pkgName] {
				continue
			}
			dirPath := paths.PackageDir(pkgName)
			if err := os.RemoveAll(dirPath); err != nil {
				_ = errorlog.Log(ctx, cfg.RootPath, "check", "orphan cleanup failed: "+err.Error())
				_ = aurlog.Log(ctx, cfg.ActivityLogPath, "check", string(pkgName), "check1", "orphan_cleanup", "status", "failed", "error", err.Error())
				findings = append(findings, fmt.Sprintf("orphaned package dir %q: could not delete: %v", pkgName, err))
			} else {
				check1OrphanFixes++
				_ = aurlog.Log(ctx, cfg.ActivityLogPath, "check", string(pkgName), "check1", "orphan_cleanup", "status", "success", "path", dirPath)
				fmt.Fprintf(os.Stderr, "check 1: removed orphaned package dir %q\n", pkgName)
			}
		}
	}
	trace.Log(ctx, "check1", "state", "done")

	// ---- Check 2: dangling ArtifactIDs + orphaned artifacts ----
	ctx = trace.Start(ctx, "check2")

	// Dangling ArtifactIDs in Build records
	for _, pkg := range packages {
		build, err := d.BuildDB.Get(pkg)
		if err != nil {
			if !errors.Is(err, domain.ErrNotFound) {
				check1Corrupted++
				findings = append(findings, fmt.Sprintf("package %q: corrupted build record: %v", pkg, err))
			}
			continue
		}
		if len(build.ArtifactIDs) == 0 {
			continue
		}

		removed, refFindings := removeDanglingArtifactRefs(ctx, cfg.ActivityLogPath, d.BuildDB, d.ArtifactDB, pkg, build)
		check2DanglingRefs += removed
		findings = append(findings, refFindings...)
	}

	// Orphaned artifacts (package no longer in catalog)
	allArts, err := d.ArtifactDB.ListAll()
	if err != nil {
		_ = errorlog.Log(ctx, cfg.RootPath, "check", "artifact list failed: "+err.Error())
		var errs []error
		if j, ok := err.(interface{ Unwrap() []error }); ok {
			errs = j.Unwrap()
		} else {
			errs = []error{err}
		}
		for _, e := range errs {
			findings = append(findings, "artifact record unreadable: "+e.Error())
		}
	}
	for _, art := range allArts {
		if catSet[art.PackageName] {
			continue
		}
		if err := d.ArtifactDB.Delete(art.ID); err != nil {
			_ = aurlog.Log(ctx, cfg.ActivityLogPath, "check", string(art.ID), "check2", "orphaned_artifact", "status", "failed", "error", err.Error())
			findings = append(findings, fmt.Sprintf("orphaned artifact %q: could not delete: %v", art.ID, err))
		} else {
			check2OrphanArts++
			_ = aurlog.Log(ctx, cfg.ActivityLogPath, "check", string(art.ID), "check2", "orphaned_artifact", "status", "success", "package", string(art.PackageName))
			fmt.Fprintf(os.Stderr, "check 2: removed orphaned artifact %q\n", art.ID)
		}
	}

	// Stale artifacts (old build IDs for packages still in catalog)
	current := make(map[domain.PackageName]domain.BuildID, len(packages))
	skip := make(map[domain.PackageName]bool, len(packages))
	for _, pkg := range packages {
		ls, err := d.BuildDB.GetLastSuccess(pkg)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				continue
			}
			_ = aurlog.Log(ctx, cfg.ActivityLogPath, "check", string(pkg), "check2", "stale_artifact", "status", "failed", "error", err.Error())
			findings = append(findings, fmt.Sprintf("package %q: GetLastSuccess failed: %v", pkg, err))
			skip[pkg] = true
			continue
		}
		if ls != nil {
			current[pkg] = ls.ID
		}
	}
	var staleArts []domain.Artifact
	for _, art := range allArts {
		if !catSet[art.PackageName] {
			continue
		}
		if skip[art.PackageName] {
			continue
		}
		staleArts = append(staleArts, art)
	}
	if len(staleArts) > 0 {
		stale := build.StaleArtifacts(staleArts, current)
		for _, art := range stale {
			if err := d.ArtifactDB.Delete(art.ID); err != nil {
				_ = aurlog.Log(ctx, cfg.ActivityLogPath, "check", string(art.ID), "check2", "stale_artifact", "status", "failed", "error", err.Error())
				findings = append(findings, fmt.Sprintf("stale artifact %q: could not delete: %v", art.ID, err))
			} else {
				check2StaleArts++
				_ = aurlog.Log(ctx, cfg.ActivityLogPath, "check", string(art.ID), "check2", "stale_artifact", "status", "success", "package", string(art.PackageName), "build_id", string(art.BuildID))
				fmt.Fprintf(os.Stderr, "check 2: removed stale artifact %q (build %q, package %q)\n", art.ID, art.BuildID, art.PackageName)
			}
		}
	}

	// Orphaned build directories (#357)
	removedDirs, sweepFindings := sweepOrphanBuildDirs(ctx, cfg.ActivityLogPath, paths, d.BuildDB, d.ArtifactDB, packages)
	check2OrphanBuildDirs += removedDirs
	findings = append(findings, sweepFindings...)

	trace.Log(ctx, "check2", "state", "done")

	// ---- Check 3: physical/state checks (report-only) ----
	ctx = trace.Start(ctx, "check3")

	for _, pkg := range packages {
		// Missing binaries of the last success, not the latest attempt:
		// after a failed rebuild the latest attempt has no artifacts, and
		// the last success's files must still be checked (#378). An
		// unreadable last success was already reported by check 2.
		ls, err := d.BuildDB.GetLastSuccess(pkg)
		if err != nil || ls == nil {
			continue
		}
		for _, artID := range ls.ArtifactIDs {
			art, err := d.ArtifactDB.Get(artID)
			if err != nil {
				continue
			}
			binPath := filepath.Join(paths.BuildArtifactsDir(ls.ID), art.Filename)
			if _, err := os.Stat(binPath); err != nil {
				check3MissingBins++
				_ = aurlog.Log(ctx, cfg.ActivityLogPath, "check", string(pkg), "check3", "missing_binary", "artifact_id", string(artID), "path", binPath)
				findings = append(findings, fmt.Sprintf("package %q: missing binary %q (artifact %q)", pkg, art.Filename, artID))
			}
		}
	}

	// Repository integrity (#378): report-only.
	repo := checkRepository(ctx, cfg.ActivityLogPath, d.Repo.Location, cfg.RepoDBName, d.BuildDB, d.ArtifactDB, packages)
	check3RepoFiles = repo.fileProblems
	check3NotMarked = repo.notMarked
	check3NotInRepo = repo.notInRepo
	findings = append(findings, repo.findings...)

	trace.Log(ctx, "check3", "state", "done")

	// ---- Summary ----
	fmt.Println("\n=== Internal Consistency Check ===")
	fmt.Printf("Packages checked: %d\n", packagesChecked)
	fmt.Printf("Check 1 — orphaned store records auto-fixed: %d\n", check1OrphanFixes)
	fmt.Printf("Check 1 — missing reviews (report-only): %d\n", check1MissingRev)
	fmt.Printf("Check 1 — corrupted records (report-only): %d\n", check1Corrupted)
	fmt.Printf("Check 2 — dangling artifact refs auto-fixed: %d\n", check2DanglingRefs)
	fmt.Printf("Check 2 — orphaned artifacts auto-fixed: %d\n", check2OrphanArts)
	fmt.Printf("Check 2 — stale artifacts auto-fixed: %d\n", check2StaleArts)
	fmt.Printf("Check 2 — orphaned build dirs auto-fixed: %d\n", check2OrphanBuildDirs)
	fmt.Printf("Check 3 — missing binaries (report-only): %d\n", check3MissingBins)
	fmt.Printf("Check 3 — repository files missing or mismatched (report-only): %d\n", check3RepoFiles)
	fmt.Printf("Check 3 — in repository, not marked published (report-only): %d\n", check3NotMarked)
	fmt.Printf("Check 3 — marked published, not in repository (report-only): %d\n", check3NotInRepo)

	if len(findings) > 0 {
		fmt.Println()
		for _, f := range findings {
			_, _ = fmt.Fprintln(os.Stdout, f)
			_ = errorlog.Log(ctx, cfg.RootPath, "check", f)
		}
		os.Exit(1)
	}

	fmt.Println("\nNo issues found.")
	os.Exit(0)
}

// runUpdate checks all curated packages for updates, categorizes them
// by lifecycle state, and interactively confirms which to fetch and
// queue for review. Never installs anything.
func runUpdate(
	cfg domain.WorkspaceConfig,
	pkgCatalog catalog.PackageCatalog,
	provider git.Provider,
) {
	release := acquireWorkspaceLock(cfg, "aur-curator update")
	defer func() { _ = release() }()

	d := deps.NewDependencies(cfg, provider, pkgCatalog)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	ctx = trace.Start(ctx, "update")

	// Step 1: Fetch catalog.
	packages, err := pkgCatalog.Packages(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "update: catalog error: %v\n", err)
		_ = errorlog.Log(ctx, cfg.RootPath, "update", "catalog.Packages failed: "+err.Error())
		os.Exit(1)
	}

	if len(packages) == 0 {
		fmt.Println("No curated packages found.")
		os.Exit(0)
	}

	// Step 2: Categorize packages by lifecycle state.
	type pendingUpdate struct {
		pkg          domain.PackageName
		aurVer       string
		localVer     string
		localUnknown bool
	}

	var bucket2NotStale []domain.PackageName
	var bucket2Stale []domain.PackageName // each entry holds pkg + its versions
	var bucket2StaleVersions []pendingUpdate
	var bucket3Candidates []pendingUpdate

	// Single AUR RPC for all packages.
	aurClient := aurinfo.NewClient("https://aur.archlinux.org/rpc/", 15*time.Second, aurinfo.NewGitLocalVersionResolver(provider, d.ReviewDB))
	aurVersions, err := aurClient.FetchAURVersions(ctx, packages)
	if err != nil {
		fmt.Fprintf(os.Stderr, "update: AUR fetch failed: %v\n", err)
		_ = errorlog.Log(ctx, cfg.RootPath, "update", "AUR fetch failed: "+err.Error())
		os.Exit(1)
	}
	trace.Log(ctx, "update", "phase", "aur_fetch", "count", len(aurVersions))

	for _, pkg := range packages {
		review, reviewErr := d.ReviewDB.Get(pkg)
		if reviewErr != nil {
			if errors.Is(reviewErr, domain.ErrNotFound) {
				continue // NotReviewed — excluded entirely
			}
			// Unexpected error — treat as NotReviewed (skip)
			continue
		}

		// Build lookup for DeriveState.
		// We don't need build records for categorization here —
		// the packet says bucket 2 is ReviewPending and bucket 3
		// is everything else. We derive that from the review status.
		var isMerged bool
		if review.UpstreamCommit != "" {
			isMerged, _ = provider.IsMerged(ctx, pkg, review.UpstreamCommit)
		}

		state := lifecycle.DeriveState(lifecycle.Inputs{
			ReviewStatus: review.Status,
			HasReview:    true,
			IsMerged:     isMerged,
		})

		aurVer := aurVersions[pkg]

		switch state {
		case lifecycle.ReviewPending:
			// Bucket 2: read PKGBUILD at review's UpstreamCommit.
			pkgbuild, fileErr := provider.FileAtCommit(ctx, pkg, review.UpstreamCommit, "PKGBUILD")
			if fileErr != nil {
				// Can't read — skip this package
				continue
			}
			localVer, localUnknown := aurinfo.ExtractFullVersion(pkgbuild)

			stale, _ := aurinfo.CompareVersions(aurVer, localVer)
			if stale {
				bucket2Stale = append(bucket2Stale, pkg)
				bucket2StaleVersions = append(bucket2StaleVersions, pendingUpdate{pkg: pkg, aurVer: aurVer, localVer: localVer, localUnknown: localUnknown})
			} else {
				bucket2NotStale = append(bucket2NotStale, pkg)
			}

		case lifecycle.Approved, lifecycle.Merged, lifecycle.Built, lifecycle.Rejected:
			// Bucket 3: compare AUR against local master PKGBUILD.
			localVer, localUnknown := parseLocalMasterVersion(ctx, provider, pkg)
			stale, _ := aurinfo.CompareVersions(aurVer, localVer)
			if stale {
				bucket3Candidates = append(bucket3Candidates, pendingUpdate{pkg: pkg, aurVer: aurVer, localVer: localVer, localUnknown: localUnknown})
			}
		}
		// DeriveState is called without build inputs here, so merged
		// packages derive as Merged and land in bucket 3 whatever their
		// build state.
	}

	// Step 3: Output bucket 2 informational lines first.
	for _, pkg := range bucket2NotStale {
		fmt.Printf("%s: review pending, no newer upstream version — no action needed.\n", pkg)
	}

	// Step 4: Output bucket 2 stale prompts.
	reader := bufio.NewReader(os.Stdin)
	for i, pkg := range bucket2Stale {
		v := bucket2StaleVersions[i]
		fmt.Printf("%s: pending review is stale — AUR has moved to %s since this review was created at %s. Confirming will discard the current review's notes/findings. Proceed? [y/n] ", pkg, v.aurVer, v.localVer)
		answer, err := reader.ReadString('\n')
		if err != nil || !strings.HasPrefix(strings.ToLower(strings.TrimSpace(answer)), "y") {
			continue
		}
		if _, err := review.FetchAndReview(ctx, provider, d.Reviews, pkg, true); err != nil {
			fmt.Fprintf(os.Stderr, "%s: fetch failed: %v\n", pkg, err)
			_ = aurlog.Log(ctx, cfg.ActivityLogPath, "update", string(pkg), "fetch", "failed", "error", err.Error())
		} else {
			fmt.Printf("%s: fetch complete, review refreshed.\n", pkg)
			_ = aurlog.Log(ctx, cfg.ActivityLogPath, "update", string(pkg), "fetch", "success")
		}
	}

	// Step 5: Bucket 3 batch list.
	if len(bucket3Candidates) > 0 {
		fmt.Println("\n=== Packages with updates available ===")
		for i, c := range bucket3Candidates {
			fmt.Printf("  [%d] %s: %s -> %s", i+1, c.pkg, c.localVer, c.aurVer)
			if c.localUnknown {
				fmt.Printf(" (local version unknown)")
			}
			fmt.Println()
		}

		fmt.Print("(y)es, (n)o, (a)ll remaining, (q)uit? ")
		answer, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println()
			fmt.Println("Aborted.")
			_ = aurlog.Log(ctx, cfg.ActivityLogPath, "update", "batch", "status", "aborted", "reason", "read_error")
			os.Exit(0)
		}
		answer = strings.ToLower(strings.TrimSpace(answer))

		// Process remaining candidates based on answer.
		var startIdx int
		switch answer {
		case "q":
			// Quit — skip all remaining
			fmt.Println("Aborted.")
			_ = aurlog.Log(ctx, cfg.ActivityLogPath, "update", "batch", "status", "aborted", "reason", "user_quit")

		case "a":
			// All remaining — process all from start
			startIdx = 0
			for _, c := range bucket3Candidates[startIdx:] {
				if _, err := review.FetchAndReview(ctx, provider, nil, c.pkg, false); err != nil {
					fmt.Fprintf(os.Stderr, "%s: fetch failed: %v\n", c.pkg, err)
					_ = aurlog.Log(ctx, cfg.ActivityLogPath, "update", string(c.pkg), "fetch", "failed", "error", err.Error())
				} else {
					fmt.Printf("%s: fetch complete.\n", c.pkg)
					_ = aurlog.Log(ctx, cfg.ActivityLogPath, "update", string(c.pkg), "fetch", "success")
				}
			}

		default:
			// Individual yes/no — process each from startIdx
			for _, c := range bucket3Candidates[startIdx:] {
				fmt.Printf("%s: proceed? [y/n] ", c.pkg)
				yes, err := reader.ReadString('\n')
				if err != nil || !strings.HasPrefix(strings.ToLower(strings.TrimSpace(yes)), "y") {
					fmt.Printf("%s: skipped.\n", c.pkg)
					continue
				}
				if _, err := review.FetchAndReview(ctx, provider, nil, c.pkg, false); err != nil {
					fmt.Fprintf(os.Stderr, "%s: fetch failed: %v\n", c.pkg, err)
					_ = aurlog.Log(ctx, cfg.ActivityLogPath, "update", string(c.pkg), "fetch", "failed", "error", err.Error())
				} else {
					fmt.Printf("%s: fetch complete.\n", c.pkg)
					_ = aurlog.Log(ctx, cfg.ActivityLogPath, "update", string(c.pkg), "fetch", "success")
				}
			}
		}
	}

	fmt.Println("Update complete.")
}

// parseLocalMasterVersion reads the PKGBUILD for a package from local master
// and extracts the pkgver value. Returns the version string and a flag
// indicating if parsing failed.
func parseLocalMasterVersion(ctx context.Context, provider git.Provider, pkg domain.PackageName) (string, bool) {
	head, err := provider.LocalHeadCommit(ctx, pkg)
	if err != nil {
		return "", true
	}
	content, err := provider.FileAtCommit(ctx, pkg, head, "PKGBUILD")
	if err != nil {
		return "", true
	}
	return aurinfo.ExtractFullVersion(content)
}
