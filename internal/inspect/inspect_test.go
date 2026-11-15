package inspect

import (
	"reflect"
	"strings"
	"testing"
)

// listing is bsdtar's mtree output for a fixture package (the shape seen
// on 15 Nov, #525), with escaped names.
const listing = `#mtree
./.BUILDINFO gname=root uname=root mode=644 gid=0 uid=0 type=file
./.INSTALL gname=root uname=root mode=644 gid=0 uid=0 type=file
./.MTREE gname=root uname=root mode=644 gid=0 uid=0 type=file
./.PKGINFO gname=root uname=root mode=644 gid=0 uid=0 type=file
./etc gname=root uname=root mode=755 gid=0 uid=0 type=dir
./etc/p7.conf gname=root uname=root mode=644 gid=0 uid=0 type=file
./etc/sudoers.d/p7 gname=root uname=root mode=440 gid=0 uid=0 type=file
./usr/bin/link gname=root uname=root mode=777 gid=0 uid=0 type=link link=suidtool
./usr/bin/sgidtool gname=root uname=root mode=2755 gid=0 uid=0 type=file
./usr/bin/suidtool gname=root uname=root mode=4755 gid=0 uid=0 type=file
./usr/share/a\040dir/f\040->\040x gname=root uname=root mode=644 gid=0 uid=0 type=file
./usr/share/a\012b gname=root uname=root mode=644 gid=0 uid=0 type=file
`

func TestParseMtree(t *testing.T) {
	es, err := ParseMtree(strings.NewReader(listing))
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 12 {
		t.Fatalf("%d entries, want 12: %+v", len(es), es)
	}
	want := map[string]Entry{
		"usr/bin/suidtool":       {Path: "usr/bin/suidtool", Type: "file", Mode: 0o4755, Uname: "root", Gname: "root"},
		"usr/bin/link":           {Path: "usr/bin/link", Type: "link", Mode: 0o777, Uname: "root", Gname: "root", Link: "suidtool"},
		"usr/share/a dir/f -> x": {Path: "usr/share/a dir/f -> x", Type: "file", Mode: 0o644, Uname: "root", Gname: "root"},
		"usr/share/a\nb":         {Path: "usr/share/a\nb", Type: "file", Mode: 0o644, Uname: "root", Gname: "root"},
	}
	found := 0
	for _, e := range es {
		if w, ok := want[e.Path]; ok {
			found++
			if !reflect.DeepEqual(e, w) {
				t.Errorf("entry %q = %+v, want %+v", e.Path, e, w)
			}
		}
	}
	if found != len(want) {
		t.Errorf("found %d of %d expected entries", found, len(want))
	}
}

func TestParseMtreeSetUnset(t *testing.T) {
	es, err := ParseMtree(strings.NewReader("#mtree\n/set type=file uid=0 gid=0 mode=644\n. type=dir mode=755\n./a\n./b mode=4755\n/unset all\n./c type=dir mode=700 uid=5 gid=5\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 3 || es[0].Path != "a" || es[0].Mode != 0o644 || es[1].Mode != 0o4755 || es[2].UID != 5 {
		t.Fatalf("entries %+v", es)
	}
}

func TestParseMtreeErrors(t *testing.T) {
	for name, in := range map[string]string{
		"no type":      "./a mode=644 uid=0 gid=0\n",
		"no mode":      "./a type=file uid=0 gid=0\n",
		"no uid":       "./a type=file mode=644 gid=0\n",
		"bad mode":     "./a type=file mode=9z uid=0 gid=0\n",
		"bad escape":   `./a\x type=file mode=644 uid=0 gid=0` + "\n",
		"short escape": `./a\04 type=file mode=644 uid=0 gid=0` + "\n",
	} {
		if _, err := ParseMtree(strings.NewReader(in)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func entry(p, typ string, mode uint32, uid, gid int) Entry {
	return Entry{Path: p, Type: typ, Mode: mode, UID: uid, GID: gid}
}

// TestClassify: each rule of domain.md § Build → Confirmed, literal
// category texts, first match wins.
func TestClassify(t *testing.T) {
	for _, tc := range []struct {
		e    Entry
		want Category // "" means not listed
	}{
		{entry("usr/bin/s", "file", 0o4755, 0, 0), "setuid / setgid"},
		{entry("usr/bin/g", "file", 0o2755, 0, 0), "setuid / setgid"},
		{entry("etc/sudoers.d/x", "file", 0o4440, 0, 0), "setuid / setgid"},
		{entry("usr/bin/s", "file", 0o4755, 1000, 0), "setuid / setgid"},
		{entry("usr/bin/n", "file", 0o755, 1000, 0), "not owned by root"},
		{entry("usr/share/d", "dir", 0o755, 0, 50), "not owned by root"},
		{entry(".INSTALL", "file", 0o644, 0, 0), "install script"},
		{entry("usr/share/libalpm/hooks/x.hook", "file", 0o644, 0, 0), "pacman hook"},
		{entry("etc/pacman.d/hooks/x.hook", "link", 0o777, 0, 0), "pacman hook"},
		{entry("usr/lib/systemd/system/x.service", "file", 0o644, 0, 0), "systemd unit"},
		{entry("usr/lib/systemd/user/x.service", "file", 0o644, 0, 0), "systemd unit"},
		{entry("etc/systemd/system/multi-user.target.wants/x.service", "link", 0o777, 0, 0), "systemd unit"},
		{entry("etc/sudoers", "file", 0o440, 0, 0), "sudoers"},
		{entry("etc/sudoers.d/x", "file", 0o440, 0, 0), "sudoers"},
		{entry("usr/lib/sysusers.d/x.conf", "file", 0o644, 0, 0), "sysusers / tmpfiles"},
		{entry("usr/lib/tmpfiles.d/x.conf", "file", 0o644, 0, 0), "sysusers / tmpfiles"},
		{entry("usr/lib/udev/rules.d/99-x.rules", "file", 0o644, 0, 0), "udev rule"},
		{entry("etc/udev/rules.d/99-x.rules", "file", 0o644, 0, 0), "udev rule"},
		{entry("usr/share/polkit-1/rules.d/x.rules", "file", 0o644, 0, 0), "polkit rule"},
		{entry("etc/polkit-1/rules.d/x.rules", "file", 0o644, 0, 0), "polkit rule"},
		{entry("etc/x.conf", "file", 0o644, 0, 0), "under /etc"},
		{entry("etc/profile.d/x.sh", "link", 0o777, 0, 0), "under /etc"},
		{entry("etc/sudoersX", "file", 0o644, 0, 0), "under /etc"},
		{entry("usr/lib/x/dev", "char", 0o600, 0, 0), "unusual entry"},
		{entry("usr/lib/x/fifo", "fifo", 0o600, 0, 0), "unusual entry"},
		{entry("/etc/x", "file", 0o644, 0, 0), "unusual entry"},
		{entry("usr/../etc/sudoers.d/x", "file", 0o644, 0, 0), "unusual entry"},
		{entry("../x", "file", 0o644, 0, 0), "unusual entry"},
		{entry("etc", "dir", 0o755, 0, 0), ""},
		{entry("etc/systemd/system", "dir", 0o755, 0, 0), ""},
		{entry("usr/bin/x", "file", 0o755, 0, 0), ""},
		{entry("usr/bin/x", "link", 0o777, 0, 0), ""},
		{entry(".BUILDINFO", "file", 0o644, 0, 0), ""},
		{entry(".MTREE", "file", 0o644, 0, 0), ""},
		{entry(".PKGINFO", "file", 0o644, 0, 0), ""},
		{entry(".MTREE", "file", 0o644, 1000, 1000), ""},
	} {
		r := Classify([]Entry{tc.e})
		var got Category
		if len(r.Groups) == 1 {
			got = r.Groups[0].Category
		} else if len(r.Groups) > 1 {
			t.Fatalf("%+v in %d groups", tc.e, len(r.Groups))
		}
		if got != tc.want {
			t.Errorf("%s %s %o %d:%d → %q, want %q", tc.e.Path, tc.e.Type, tc.e.Mode, tc.e.UID, tc.e.GID, got, tc.want)
		}
	}
}

// TestClassifyReport: groups in Categories order, entries in archive
// order; HasInstall, Count, Empty.
func TestClassifyReport(t *testing.T) {
	es, err := ParseMtree(strings.NewReader(listing))
	if err != nil {
		t.Fatal(err)
	}
	r := Classify(es)
	var got []string
	for _, g := range r.Groups {
		var ps []string
		for _, e := range g.Entries {
			ps = append(ps, e.Path)
		}
		got = append(got, string(g.Category)+": "+strings.Join(ps, ", "))
	}
	want := []string{
		"setuid / setgid: usr/bin/sgidtool, usr/bin/suidtool",
		"install script: .INSTALL",
		"sudoers: etc/sudoers.d/p7",
		"under /etc: etc/p7.conf",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("groups:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if !r.HasInstall() || r.Count() != 5 || r.Empty() {
		t.Errorf("HasInstall %v Count %d Empty %v", r.HasInstall(), r.Count(), r.Empty())
	}
	if e := Classify(nil); !e.Empty() || e.HasInstall() || e.Count() != 0 {
		t.Errorf("empty report %+v", e)
	}
}

// TestCategories: every constant listed once, in the documented order.
func TestCategories(t *testing.T) {
	want := []Category{"setuid / setgid", "not owned by root", "install script", "pacman hook", "systemd unit", "sudoers", "sysusers / tmpfiles", "udev rule", "polkit rule", "under /etc", "unusual entry"}
	if !reflect.DeepEqual(Categories, want) {
		t.Errorf("Categories = %q", Categories)
	}
}
