package sanitize

import (
	"strings"
	"testing"
)

func TestForLogDisplay(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty",
			input:    "",
			expected: "",
		},
		{
			name:     "plain multi-line",
			input:    "a\nb",
			expected: "a\nb",
		},
		{
			name:     "trailing newline kept",
			input:    "a\nb\n",
			expected: "a\nb\n",
		},
		{
			name:     "CRLF",
			input:    "a\r\nb\r\n",
			expected: "a\nb\n",
		},
		{
			name:     "progress, last segment wins",
			input:    "\r 0 x\r 42 y\r100 z",
			expected: "100 z",
		},
		{
			name:     "leading CR",
			input:    "\rabc",
			expected: "abc",
		},
		{
			name:     "CR only",
			input:    "\r",
			expected: "",
		},
		{
			name:     "multiple trailing CR",
			input:    "abc\r\r",
			expected: "abc",
		},
		{
			name:     "tab kept",
			input:    "a\tb",
			expected: "a\tb",
		},
		{
			name:     "C0 and DEL dropped",
			input:    "a\bb\x07c\x7fd",
			expected: "abcd",
		},
		{
			name:     "ESC dropped",
			input:    "\x1b[1mbold",
			expected: "[1mbold",
		},
		{
			name:     "C1 dropped",
			input:    "a\u009bb",
			expected: "ab",
		},
		{
			name:     "non-ASCII kept",
			input:    "café →",
			expected: "café →",
		},
		{
			name:     "mixed lines",
			input:    "head\n\r 0 a\r100 b\ntail\n",
			expected: "head\n100 b\ntail\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ForLogDisplay(tt.input)
			if got != tt.expected {
				t.Errorf("ForLogDisplay(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestForMessage(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "plain text",
			input:    "fatal: unable to access 'https://aur.archlinux.org/foo.git/': Could not resolve host: aur.archlinux.org",
			expected: "fatal: unable to access 'https://aur.archlinux.org/foo.git/': Could not resolve host: aur.archlinux.org",
		},
		{
			name:     "single newline",
			input:    "line1\nline2",
			expected: "line1 line2",
		},
		{
			name:     "windows newlines",
			input:    "line1\r\nline2\r\n",
			expected: "line1 line2",
		},
		{
			name:     "mixed newlines",
			input:    "line1\nline2\rline3\r\nline4",
			expected: "line1 line2 line3 line4",
		},
		{
			name:     "consecutive newlines collapse to single space",
			input:    "line1\n\n\nline2",
			expected: "line1 line2",
		},
		{
			name:     "newlines after spaces",
			input:    "line1 \n\n line2",
			expected: "line1  line2",
		},
		{
			name:     "carriage return only",
			input:    "line1\rline2",
			expected: "line1 line2",
		},
		{
			name:     "tab character removed",
			input:    "col1\tcol2",
			expected: "col1col2",
		},
		{
			name:     "bell and other control chars removed",
			input:    "text\x07more\x08text",
			expected: "textmoretext",
		},
		{
			name:     "trims leading and trailing whitespace",
			input:    "  hello world  ",
			expected: "hello world",
		},
		{
			name:     "trailing newlines trimmed",
			input:    "hello\n\n",
			expected: "hello",
		},
		{
			name:     "short truncation at 200 runes",
			input:    strings.Repeat("a", 200),
			expected: strings.Repeat("a", 200),
		},
		{
			name:     "truncates at 201 runes with ellipsis prefix",
			input:    strings.Repeat("a", 201),
			expected: "..." + strings.Repeat("a", 197),
		},
		{
			name:     "truncates at 250 runes preserving tail",
			input:    strings.Repeat("a", 250),
			expected: "..." + strings.Repeat("a", 197),
		},
		{
			name:     "multi-line git error",
			input:    "Cloning into bare '/tmp/aur-work/foo'...\nremote: Enumerating objects: 5, done.\nremote: Counting objects: 100% (5/5), done.\nerror: failed to fetch some objects",
			expected: "Cloning into bare '/tmp/aur-work/foo'... remote: Enumerating objects: 5, done. remote: Counting objects: 100% (5/5), done. error: failed to fetch some objects",
		},
		{
			name:     "unicode preserved",
			input:    "fatal: ÆØÅ éàü",
			expected: "fatal: ÆØÅ éàü",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ForMessage(tt.input)
			if result != tt.expected {
				t.Errorf("ForMessage(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestForMessageIdempotent(t *testing.T) {
	input := "line1\nline2\r\nline3"
	first := ForMessage(input)
	second := ForMessage(first)
	if first != second {
		t.Errorf("ForMessage not idempotent: first=%q, second=%q", first, second)
	}
}
