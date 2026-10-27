package svn

import (
	"regexp"
	"strings"
)

// Choice selects which side of a conflict hunk to keep.
type Choice int

const (
	ChooseMine Choice = iota
	ChooseTheirs
	ChooseBoth // mine followed by theirs
	ChooseBase
)

// ConflictHunk locates one conflict-marker block in a file. Line indices
// are 0-based and refer to the marker lines themselves; Base is -1 when the
// file uses two-way markers (no ||||||| section).
type ConflictHunk struct {
	Start, Base, Sep, End int
}

func isMarker(line, prefix string) bool {
	line = strings.TrimRight(line, "\r")
	return line == prefix || strings.HasPrefix(line, prefix+" ")
}

// SplitLines splits text into lines, keeping any "\r" so CRLF files
// round-trip through JoinLines unchanged.
func SplitLines(text string) []string { return strings.Split(text, "\n") }

// JoinLines is the inverse of SplitLines.
func JoinLines(lines []string) string { return strings.Join(lines, "\n") }

// ParseConflicts finds well-formed conflict-marker blocks in lines.
// Unterminated or malformed blocks are ignored.
func ParseConflicts(lines []string) []ConflictHunk {
	var hunks []ConflictHunk
	for i := 0; i < len(lines); i++ {
		if !isMarker(lines[i], "<<<<<<<") {
			continue
		}
		h := ConflictHunk{Start: i, Base: -1, Sep: -1, End: -1}
		for j := i + 1; j < len(lines); j++ {
			switch {
			case isMarker(lines[j], "<<<<<<<"):
				j = len(lines) // nested/unterminated: give up on this block
			case h.Sep < 0 && h.Base < 0 && isMarker(lines[j], "|||||||"):
				h.Base = j
			case h.Sep < 0 && isMarker(lines[j], "======="):
				h.Sep = j
			case h.Sep >= 0 && isMarker(lines[j], ">>>>>>>"):
				h.End = j
				j = len(lines)
			}
		}
		if h.End > 0 {
			hunks = append(hunks, h)
			i = h.End
		}
	}
	return hunks
}

// Mine, BaseLines and Theirs return the content lines of each section.
func (h ConflictHunk) Mine(lines []string) []string {
	end := h.Sep
	if h.Base >= 0 {
		end = h.Base
	}
	return lines[h.Start+1 : end]
}

func (h ConflictHunk) BaseLines(lines []string) []string {
	if h.Base < 0 {
		return nil
	}
	return lines[h.Base+1 : h.Sep]
}

func (h ConflictHunk) Theirs(lines []string) []string { return lines[h.Sep+1 : h.End] }

// ResolveHunk replaces hunk h in lines with the chosen side and returns
// the new lines. The input slice is not modified.
func ResolveHunk(lines []string, h ConflictHunk, c Choice) []string {
	var keep []string
	switch c {
	case ChooseMine:
		keep = h.Mine(lines)
	case ChooseTheirs:
		keep = h.Theirs(lines)
	case ChooseBoth:
		keep = append(append([]string{}, h.Mine(lines)...), h.Theirs(lines)...)
	case ChooseBase:
		keep = h.BaseLines(lines)
	}
	out := make([]string, 0, len(lines))
	out = append(out, lines[:h.Start]...)
	out = append(out, keep...)
	return append(out, lines[h.End+1:]...)
}

// conflictArtifactRe matches the helper files svn writes next to a
// text-conflicted file: .mine, .rN, .working, .merge-left.rN, .merge-right.rN.
var conflictArtifactRe = regexp.MustCompile(`\.(mine|working|r\d+|merge-left\.r\d+|merge-right\.r\d+)$`)

// hideConflictArtifacts drops unversioned helper files belonging to
// conflicted paths; they disappear once the conflict is resolved.
func hideConflictArtifacts(files []FileStatus) []FileStatus {
	conflicted := map[string]bool{}
	for _, f := range files {
		if f.Item == ItemConflicted {
			conflicted[f.Path] = true
		}
	}
	if len(conflicted) == 0 {
		return files
	}
	out := files[:0]
	for _, f := range files {
		if f.Item == ItemUnversioned {
			if loc := conflictArtifactRe.FindStringIndex(f.Path); loc != nil && conflicted[f.Path[:loc[0]]] {
				continue
			}
		}
		out = append(out, f)
	}
	return out
}
