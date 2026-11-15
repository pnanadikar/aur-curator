package inspect

import (
	"path"
	"strings"
)

// Category is a kind of entry the inspection lists (domain.md § Build →
// Confirmed). The text is what the user sees.
type Category string

// The categories, in the order an entry is tested and listed.
const (
	CatSetID    Category = "setuid / setgid"
	CatNotRoot  Category = "not owned by root"
	CatInstall  Category = "install script"
	CatHook     Category = "pacman hook"
	CatUnit     Category = "systemd unit"
	CatSudoers  Category = "sudoers"
	CatSysusers Category = "sysusers / tmpfiles"
	CatUdev     Category = "udev rule"
	CatPolkit   Category = "polkit rule"
	CatEtc      Category = "under /etc"
	CatUnusual  Category = "unusual entry"
)

// Categories lists every category in test and display order.
var Categories = []Category{
	CatSetID, CatNotRoot, CatInstall, CatHook, CatUnit, CatSudoers,
	CatSysusers, CatUdev, CatPolkit, CatEtc, CatUnusual,
}

// prefixes are the path-based categories: an entry under one of the
// directories, or equal to one of the files (no trailing slash).
var prefixes = []struct {
	cat   Category
	paths []string
}{
	{CatHook, []string{"usr/share/libalpm/hooks/", "etc/pacman.d/hooks/"}},
	{CatUnit, []string{"usr/lib/systemd/system/", "usr/lib/systemd/user/", "etc/systemd/"}},
	{CatSudoers, []string{"etc/sudoers", "etc/sudoers.d/"}},
	{CatSysusers, []string{"usr/lib/sysusers.d/", "usr/lib/tmpfiles.d/"}},
	{CatUdev, []string{"usr/lib/udev/rules.d/", "etc/udev/rules.d/"}},
	{CatPolkit, []string{"usr/share/polkit-1/rules.d/", "etc/polkit-1/rules.d/"}},
	{CatEtc, []string{"etc/"}},
}

// metadata are makepkg's own files at the archive's top level: never
// listed (.INSTALL is, as the install script).
var metadata = map[string]bool{".BUILDINFO": true, ".MTREE": true, ".PKGINFO": true}

// Group is one category with its entries, in archive order.
type Group struct {
	Category Category
	Entries  []Entry
}

// Report is what one artifact installs with special power.
type Report struct {
	// Groups holds the categories that have entries, in Categories
	// order.
	Groups []Group
}

// Empty reports whether nothing was found.
func (r Report) Empty() bool { return len(r.Groups) == 0 }

// HasInstall reports whether the archive has an install script.
func (r Report) HasInstall() bool {
	for _, g := range r.Groups {
		if g.Category == CatInstall {
			return true
		}
	}
	return false
}

// Count is the number of entries listed.
func (r Report) Count() int {
	n := 0
	for _, g := range r.Groups {
		n += len(g.Entries)
	}
	return n
}

// Classify puts each entry in the first category that applies, or in
// none.
func Classify(entries []Entry) Report {
	by := map[Category][]Entry{}
	for _, e := range entries {
		if c, ok := categoryOf(e); ok {
			by[c] = append(by[c], e)
		}
	}
	var r Report
	for _, c := range Categories {
		if len(by[c]) > 0 {
			r.Groups = append(r.Groups, Group{Category: c, Entries: by[c]})
		}
	}
	return r
}

func categoryOf(e Entry) (Category, bool) {
	if metadata[e.Path] {
		return "", false
	}
	fileOrLink := e.Type == "file" || e.Type == "link"
	switch {
	case e.Type == "file" && e.Mode&0o6000 != 0:
		return CatSetID, true
	case e.UID != 0 || e.GID != 0:
		return CatNotRoot, true
	case e.Path == ".INSTALL" && e.Type == "file":
		return CatInstall, true
	}
	if !cleanRelative(e.Path) {
		return CatUnusual, true
	}
	switch e.Type {
	case "char", "block", "fifo", "socket":
		return CatUnusual, true
	}
	if !fileOrLink {
		return "", false
	}
	for _, p := range prefixes {
		for _, pre := range p.paths {
			if e.Path == pre || (strings.HasSuffix(pre, "/") && strings.HasPrefix(e.Path, pre)) {
				return p.cat, true
			}
		}
	}
	return "", false
}

// cleanRelative reports whether p is a relative path with no "." or ".."
// element, as pacman's entries are.
func cleanRelative(p string) bool {
	return p != "" && !strings.HasPrefix(p, "/") && path.Clean(p) == p && p != ".." && !strings.HasPrefix(p, "../")
}
