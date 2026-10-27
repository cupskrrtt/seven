package svn

import (
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestParseInfo(t *testing.T) {
	info, err := ParseInfo(fixture(t, "info.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Revision != 7 || info.LastChanged != 6 || info.Author != "alice" {
		t.Errorf("revisions/author = %d/%d/%q", info.Revision, info.LastChanged, info.Author)
	}
	if info.RelativeURL != "^/proj/branches/feature-x" || info.WCRoot != "/tmp/wc" {
		t.Errorf("urls = %q %q", info.RelativeURL, info.WCRoot)
	}
}

func TestParseStatus(t *testing.T) {
	files, err := ParseStatus(fixture(t, "status.xml"))
	if err != nil {
		t.Fatal(err)
	}
	// Conflicts first, then versioned changes (by path), then unversioned.
	want := []struct {
		path string
		item Item
		cl   string
	}{
		{"c.txt", ItemConflicted, ""},
		{"a.txt", ItemModified, ""},
		{"d.txt", ItemAdded, "wip"},
		{"b.txt", ItemUnversioned, ""},
	}
	if len(files) != len(want) {
		t.Fatalf("got %d files: %+v", len(files), files)
	}
	for i, w := range want {
		f := files[i]
		if f.Path != w.path || f.Item != w.item || f.Changelist != w.cl {
			t.Errorf("files[%d] = %+v, want %+v", i, f, w)
		}
	}
}

func TestParseLog(t *testing.T) {
	revs, err := ParseLog(fixture(t, "log.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 2 {
		t.Fatalf("got %d revisions", len(revs))
	}
	if revs[0].Summary() != "Fix the thing" || revs[0].Date.IsZero() {
		t.Errorf("rev 6 = %+v", revs[0])
	}
	p := revs[1].Paths[0]
	if p.Action != "A" || p.CopyFrom != "/proj/trunk" || p.CopyRev != 4 {
		t.Errorf("rev 5 path = %+v", p)
	}
}

func TestParseLocation(t *testing.T) {
	cases := map[string]Location{
		"^/trunk":                     {KindTrunk, "trunk", "^", ""},
		"^/proj/trunk/src":            {KindTrunk, "trunk", "^/proj", "/src"},
		"^/proj/branches/feature-x/a": {KindBranch, "feature-x", "^/proj", "/a"},
		"^/tags/v1.0":                 {KindTag, "v1.0", "^", ""},
		"^/random/path":               {KindOther, "/random/path", "", ""},
	}
	for in, want := range cases {
		if got := ParseLocation(in); got != want {
			t.Errorf("ParseLocation(%q) = %+v, want %+v", in, got, want)
		}
	}
	loc := ParseLocation("^/proj/trunk")
	if got := loc.BranchURL(KindBranch, "x"); got != "^/proj/branches/x" {
		t.Errorf("BranchURL = %q", got)
	}
	if got := ParseLocation("^/trunk").BranchURL(KindTag, "v1"); got != "^/tags/v1" {
		t.Errorf("BranchURL = %q", got)
	}
}

func TestParseDiff(t *testing.T) {
	text := "Index: a.txt\r\n===================================================================\r\n" +
		"--- a.txt\t(revision 6)\r\n+++ a.txt\t(working copy)\r\n@@ -1,2 +1,2 @@\r\n same\r\n-old\r\n+new\r\n"
	want := []LineKind{LineHeader, LineHeader, LineHeader, LineHeader, LineHunk, LineContext, LineRemoved, LineAdded}
	lines := ParseDiff(text)
	if len(lines) != len(want) {
		t.Fatalf("got %d lines", len(lines))
	}
	for i, k := range want {
		if lines[i].Kind != k {
			t.Errorf("line %d %q kind = %d, want %d", i, lines[i].Text, lines[i].Kind, k)
		}
	}
}

func TestPathArg(t *testing.T) {
	if got := pathArg("img@2x.png"); got != "img@2x.png@" {
		t.Errorf("pathArg = %q", got)
	}
	if got := pathArg("plain.txt"); got != "plain.txt" {
		t.Errorf("pathArg = %q", got)
	}
}
