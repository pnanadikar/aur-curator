// Command tracecheck reports function calls whose key/value arguments
// don't pair up (an odd number of arguments after the fixed parameters).
//
// Usage: go run ./tools/tracecheck [dir]   (default ".")
//
// Checks the following rules:
//
//	/internal/trace       Log     (2 fixed params: ctx, event)
//	/internal/activitylog Log     (4 fixed params: ctx, activityLogPath, event, pkgName)
//	/internal/procgroup   Run     (2 fixed params: ctx, cmd)
//
// Import path suffixes are matched so aliased imports are handled. Calls
// spreading a slice (kv...) are skipped, since their argument count isn't
// known statically. vendor/, testdata/ and hidden directories are not
// scanned. Exits 1 if any malformed call is found, 2 on a scan error.
//
// Limitation: catches arity errors only. A call that pairs correctly but
// uses a label as a key (e.g. "build", "cache-hit") is not detected.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// Rule defines a checkable function call pattern: an import path suffix,
// a function name, and the number of fixed (non-variadic) parameters that
// precede the key/value arguments.
type Rule struct {
	Label            string // e.g. "activitylog.Log"
	ImportPathSuffix string
	FuncName         string
	FixedParams      int
}

var rules = []Rule{
	{Label: "trace.Log", ImportPathSuffix: "/internal/trace", FuncName: "Log", FixedParams: 2},
	{Label: "activitylog.Log", ImportPathSuffix: "/internal/activitylog", FuncName: "Log", FixedParams: 4},
	{Label: "procgroup.Run", ImportPathSuffix: "/internal/procgroup", FuncName: "Run", FixedParams: 2},
}

type ruleCounts map[string]int

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	total, bad, counts, err := scan(root, os.Stdout)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fmt.Printf("%d calls scanned, %d malformed\n", total, bad)
	for _, r := range rules {
		fmt.Printf("  %s: %d\n", r.Label, counts[r.Label])
	}
	if bad > 0 {
		os.Exit(1)
	}
}

// scan walks root, reports each malformed call to w, and returns the total
// calls seen, how many were malformed, and per-rule counts.
func scan(root string, w io.Writer) (total, bad int, counts ruleCounts, err error) {
	counts = make(ruleCounts)
	for _, r := range rules {
		counts[r.Label] = 0
	}
	fset := token.NewFileSet()
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		t, b, rc, err := scanFile(fset, f, w)
		total += t
		bad += b
		for label, c := range rc {
			counts[label] += c
		}
		return err
	})
	return total, bad, counts, err
}

// skipDir reports whether a directory should be excluded from the scan.
// testdata/ is skipped so deliberately malformed fixtures don't fail the
// real-repo run.
func skipDir(name string) bool {
	return name == "vendor" || name == "testdata" || strings.HasPrefix(name, ".")
}

// scanFile checks every call in f that matches any rule, reporting
// malformed ones to w. A failed write to w is returned as an error: a
// report that silently dropped findings would be worse than no report.
func scanFile(fset *token.FileSet, f *ast.File, w io.Writer) (total, bad int, counts ruleCounts, err error) {
	counts = make(ruleCounts)
	for _, rule := range rules {
		counts[rule.Label] = 0
	}
	for _, rule := range rules {
		name := importName(f, rule.ImportPathSuffix)
		if name == "" {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if err != nil {
				return false
			}
			ce, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := ce.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != rule.FuncName {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); !ok || id.Name != name {
				return true
			}
			total++
			counts[rule.Label]++
			if ce.Ellipsis.IsValid() {
				return true
			}
			if kv := len(ce.Args) - rule.FixedParams; kv < 0 || kv%2 != 0 {
				bad++
				var src strings.Builder
				_ = printer.Fprint(&src, fset, ce)
				if _, werr := fmt.Fprintf(w, "%s: %s: %d kv args\n\t%s\n", fset.Position(ce.Pos()), rule.Label, kv, src.String()); werr != nil {
					err = fmt.Errorf("write report: %w", werr)
					return false
				}
			}
			return true
		})
	}
	return total, bad, counts, err
}

// importName returns the local name the package with the given import
// path suffix is imported under in f, or "" if f doesn't import it (or
// imports it as _).
func importName(f *ast.File, suffix string) string {
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil || !strings.HasSuffix(p, suffix) {
			continue
		}
		if imp.Name == nil {
			return path.Base(strings.TrimSuffix(p, "/"))
		}
		if imp.Name.Name == "_" {
			return ""
		}
		return imp.Name.Name
	}
	return ""
}
