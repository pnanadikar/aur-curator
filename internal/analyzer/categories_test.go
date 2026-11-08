package analyzer

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// literalCategories returns "file:line: value" for every string literal
// given as a category: a Category/category field in a composite literal,
// or an assignment to a .Category/.category field.
func literalCategories(fset *token.FileSet, file *ast.File) []string {
	var found []string
	isCategory := func(name string) bool { return name == "Category" || name == "category" }
	report := func(e ast.Expr) {
		if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			found = append(found, fset.Position(lit.Pos()).String()+": "+lit.Value)
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.KeyValueExpr:
			if key, ok := n.Key.(*ast.Ident); ok && isCategory(key.Name) {
				report(n.Value)
			}
		case *ast.AssignStmt:
			for i, lhs := range n.Lhs {
				if sel, ok := lhs.(*ast.SelectorExpr); ok && isCategory(sel.Sel.Name) && i < len(n.Rhs) {
					report(n.Rhs[i])
				}
			}
		}
		return true
	})
	return found
}

// TestCategoriesAreConstants fails if an analyzer gives a category as a
// string literal: every category must be a constant in categories.go so
// that Categories lists it and policy's mapping test sees it (#475).
func TestCategoriesAreConstants(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	fset := token.NewFileSet()
	parsed := 0
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		parsed++
		for _, f := range literalCategories(fset, file) {
			t.Errorf("category given as a string literal (use a constant from categories.go): %s", f)
		}
	}
	if parsed == 0 {
		t.Fatal("no source files parsed")
	}
}

// TestLiteralCategoriesDetects checks the checker itself, so
// TestCategoriesAreConstants cannot pass vacuously.
func TestLiteralCategoriesDetects(t *testing.T) {
	const src = `package x

var a = finding{Category: "lit_one"}
var b = rule{category: CategoryShellExec}

func f(g *finding) {
	g.Category = "lit_two"
	g.Category = CategoryURLNew
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "x.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := literalCategories(fset, file)
	want := []string{`x.go:3:27: "lit_one"`, `x.go:7:15: "lit_two"`}
	if len(got) != len(want) {
		t.Fatalf("found %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("found[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestCategoriesUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range Categories {
		if c == "" {
			t.Error("empty category")
		}
		if seen[c] {
			t.Errorf("category %q listed twice", c)
		}
		seen[c] = true
	}
}

// TestCategoriesListsEveryConstant fails if a Category* constant in
// categories.go is missing from Categories, which policy's mapping test
// would then never see (#475).
func TestCategoriesListsEveryConstant(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "categories.go", nil, 0)
	if err != nil {
		t.Fatalf("parse categories.go: %v", err)
	}
	listed := map[string]bool{}
	for _, c := range Categories {
		listed[c] = true
	}
	consts := 0
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs := spec.(*ast.ValueSpec)
			for i, name := range vs.Names {
				if !strings.HasPrefix(name.Name, "Category") || i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				consts++
				if value := strings.Trim(lit.Value, `"`); !listed[value] {
					t.Errorf("%s (%s) is not in Categories", name.Name, value)
				}
			}
		}
	}
	if consts != len(Categories) {
		t.Errorf("categories.go has %d Category constants, Categories lists %d", consts, len(Categories))
	}
}
