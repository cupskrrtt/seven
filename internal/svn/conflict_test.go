package svn

import (
	"reflect"
	"testing"
)

// Markers exactly as written by svn 1.14 (diff3 style with a base section).
const conflicted = "1\n<<<<<<< .mine\nMINE2\n||||||| .r2\n2\n=======\nTHEIRS2\n>>>>>>> .r3\n3\n" +
	"<<<<<<< .mine\nMINE7\n=======\nTHEIRS7a\nTHEIRS7b\n>>>>>>> .r3\n8\n"

func TestParseConflicts(t *testing.T) {
	lines := SplitLines(conflicted)
	hunks := ParseConflicts(lines)
	want := []ConflictHunk{{Start: 1, Base: 3, Sep: 5, End: 7}, {Start: 9, Base: -1, Sep: 11, End: 14}}
	if !reflect.DeepEqual(hunks, want) {
		t.Fatalf("hunks = %+v, want %+v", hunks, want)
	}
	h := hunks[0]
	if got := h.Mine(lines); !reflect.DeepEqual(got, []string{"MINE2"}) {
		t.Errorf("mine = %q", got)
	}
	if got := h.BaseLines(lines); !reflect.DeepEqual(got, []string{"2"}) {
		t.Errorf("base = %q", got)
	}
	if got := hunks[1].Theirs(lines); !reflect.DeepEqual(got, []string{"THEIRS7a", "THEIRS7b"}) {
		t.Errorf("theirs = %q", got)
	}
}

func TestResolveHunks(t *testing.T) {
	cases := []struct {
		first, second Choice
		want          string
	}{
		{ChooseMine, ChooseMine, "1\nMINE2\n3\nMINE7\n8\n"},
		{ChooseTheirs, ChooseBoth, "1\nTHEIRS2\n3\nMINE7\nTHEIRS7a\nTHEIRS7b\n8\n"},
		{ChooseBase, ChooseTheirs, "1\n2\n3\nTHEIRS7a\nTHEIRS7b\n8\n"},
	}
	for _, c := range cases {
		lines := SplitLines(conflicted)
		lines = ResolveHunk(lines, ParseConflicts(lines)[0], c.first)
		// Hunk positions shift after each resolution, so re-parse.
		lines = ResolveHunk(lines, ParseConflicts(lines)[0], c.second)
		if got := JoinLines(lines); got != c.want {
			t.Errorf("%v,%v: got %q, want %q", c.first, c.second, got, c.want)
		}
		if n := len(ParseConflicts(lines)); n != 0 {
			t.Errorf("%d hunks left", n)
		}
	}
}

func TestResolveKeepsCRLF(t *testing.T) {
	lines := SplitLines("a\r\n<<<<<<< .mine\r\nm\r\n=======\r\nt\r\n>>>>>>> .r3\r\nz\r\n")
	hunks := ParseConflicts(lines)
	if len(hunks) != 1 {
		t.Fatalf("hunks = %+v", hunks)
	}
	if got := JoinLines(ResolveHunk(lines, hunks[0], ChooseTheirs)); got != "a\r\nt\r\nz\r\n" {
		t.Errorf("got %q", got)
	}
}

func TestMalformedConflictIgnored(t *testing.T) {
	lines := SplitLines("<<<<<<< .mine\nx\n=======\ny\n") // no end marker
	if hunks := ParseConflicts(lines); len(hunks) != 0 {
		t.Errorf("hunks = %+v", hunks)
	}
}

func TestHideConflictArtifacts(t *testing.T) {
	files := []FileStatus{
		{Path: "f.txt", Item: ItemConflicted},
		{Path: "f.txt.mine", Item: ItemUnversioned},
		{Path: "f.txt.r2", Item: ItemUnversioned},
		{Path: "f.txt.merge-left.r4", Item: ItemUnversioned},
		{Path: "g.txt.mine", Item: ItemUnversioned}, // g.txt is not conflicted
	}
	got := hideConflictArtifacts(files)
	if len(got) != 2 || got[0].Path != "f.txt" || got[1].Path != "g.txt.mine" {
		t.Errorf("got %+v", got)
	}
}
