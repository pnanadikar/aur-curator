package analyzer

import "testing"

// #24 item 6: a trailing bash comment must not leak into a scalar's value
// (it would otherwise appear in the version_bump finding's summary).
func TestParseScalars_TrailingComment24(t *testing.T) {
	patch := "@@ -1,2 +1,2 @@\n" +
		"-pkgver=1.2.3 # pinned until upstream fixes the build\n" +
		"+pkgver=1.2.4\t# bumped\n" +
		" pkgrel=1  #\n"
	got := parsePKGBUILDDiff(patch)

	pkgver, ok := got.scalar("pkgver")
	if !ok || pkgver.Old != "1.2.3" || pkgver.New != "1.2.4" {
		t.Errorf("pkgver = %+v, ok=%v, want Old=1.2.3 New=1.2.4", pkgver, ok)
	}
	pkgrel, ok := got.scalar("pkgrel")
	if !ok || pkgrel.Old != "1" || pkgrel.New != "1" {
		t.Errorf("pkgrel = %+v, ok=%v, want Old=1 New=1", pkgrel, ok)
	}
}

// A "#" is only a comment when it starts a word, and never inside quotes.
func TestParseScalars_HashNotAComment24(t *testing.T) {
	cases := map[string]string{
		"pkgver=1.2#3":        "1.2#3",
		"pkgver=#1":           "#1",
		"pkgver= 1.2":         "1.2",
		"pkgver='1.2 # x'":    "'1.2 # x'",
		`pkgver="1.2 # x"`:    `"1.2 # x"`,
		`pkgver="a\" # b" #c`: `"a\" # b"`,
	}
	for line, want := range cases {
		got := parsePKGBUILDDiff("@@ -0,0 +1,1 @@\n+" + line + "\n")
		s, ok := got.scalar("pkgver")
		if !ok || s.New != want {
			t.Errorf("%s: New = %q, ok=%v, want %q", line, s.New, ok, want)
		}
	}
}

// A line that is only a comment after the "=" yields an empty value
// rather than the comment text.
func TestParseScalars_CommentOnlyValue24(t *testing.T) {
	got := parsePKGBUILDDiff("@@ -0,0 +1,1 @@\n+pkgrel= # set below\n")
	if s, ok := got.scalar("pkgrel"); ok && s.New != "" {
		t.Errorf("pkgrel.New = %q, want empty", s.New)
	}
}

// #24 item 5: escaped quotes inside a double-quoted array entry are part
// of the entry, matching bash and structuralParenDelta.
func TestExtractEntries_EscapedQuotes24(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{`source=("a\"b" 'c')`, []string{`a"b`, "c"}},
		{`source=("x\\" "y")`, []string{`x\`, "y"}},
		// Inside double quotes bash only unescapes $ ` " \ ; other
		// backslashes stay literal.
		{`source=("a\nb" "\$srcdir")`, []string{`a\nb`, "$srcdir"}},
		// Single quotes are literal: a backslash ends nothing.
		{`source=('c\' "d")`, []string{`c\`, "d"}},
		{`source=("it's" 'say "hi"')`, []string{"it's", `say "hi"`}},
	}
	for _, c := range cases {
		got := extractEntries(c.in)
		if len(got) != len(c.want) {
			t.Errorf("extractEntries(%s) = %q, want %q", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("extractEntries(%s) = %q, want %q", c.in, got, c.want)
				break
			}
		}
	}
}

func TestParseArrays_EscapedQuoteInURL24(t *testing.T) {
	patch := "@@ -1,1 +1,1 @@\n" +
		`-source=("https://example.org/a.tar.gz")` + "\n" +
		`+source=("https://example.org/\"b\".tar.gz")` + "\n"
	arrs := parsePKGBUILDDiff(patch).arraysNamed("source")
	if len(arrs) != 1 {
		t.Fatalf("got %d source arrays, want 1", len(arrs))
	}
	if a := arrs[0]; len(a.Added) != 1 || a.Added[0].Value != `https://example.org/"b".tar.gz` {
		t.Errorf("Added = %v, want [https://example.org/\"b\".tar.gz]", a.Added)
	}
}
