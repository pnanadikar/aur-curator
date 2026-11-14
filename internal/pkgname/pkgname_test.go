package pkgname

import (
	"strings"
	"testing"
)

func TestCheck(t *testing.T) {
	valid := []string{"pfetch", "visual-studio-code-bin", "mlt++", "python-foo.bar", "0ad", "a", "lib_x", "a.-+_"}
	for _, name := range valid {
		if err := Check(name); err != nil {
			t.Errorf("Check(%q) = %v, want nil", name, err)
		}
	}
	invalid := []string{"", "Foo", "fOo", "-x", ".x", "_x", "+x", "a/b", "/a", "..", ".", "a b", "a@b", "a\nb", "é", "a\x00", "a&b", "a#b"}
	for _, name := range invalid {
		err := Check(name)
		if err == nil {
			t.Errorf("Check(%q) = nil, want an error", name)
			continue
		}
		if !strings.Contains(err.Error(), Rule) {
			t.Errorf("Check(%q) = %v, want it to name the rule", name, err)
		}
	}
}

// A literal, so the message is pinned by more than the Rule constant.
func TestCheckMessage(t *testing.T) {
	want := `invalid package name "Foo": the AUR allows lowercase letters, digits and . + _ -, starting with a letter or digit`
	if err := Check("Foo"); err == nil || err.Error() != want {
		t.Errorf("Check(\"Foo\") = %v, want %q", err, want)
	}
}
