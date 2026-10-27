package app

import (
	"fmt"

	"github.com/cupskrrtt/seven/internal/ui/style"
)

var helpSections = []struct {
	name  string
	binds [][2]string
}{
	{"Global", [][2]string{
		{"tab / shift+tab", "next / previous panel"},
		{"1 2 3", "jump to Status / Files / Log"},
		{"enter", "focus main view"},
		{"r", "refresh"},
		{"u", "update to HEAD"},
		{"U", "update to revision"},
		{"@", "toggle command log"},
		{"?", "this help"},
		{"q", "quit"},
	}},
	{"Files", [][2]string{
		{"space", "select / deselect for commit"},
		{"A", "select all / clear selection"},
		{"c", "commit selected (or all changes)"},
		{"C", "commit using $SVN_EDITOR / $EDITOR"},
		{"a", "add unversioned file"},
		{"d", "revert changes (delete if unversioned)"},
		{"D", "svn rm (schedule deletion)"},
		{"i", "add to svn:ignore"},
		{"R", "resolve conflict (mine / theirs / base / pick hunks…)"},
		{"enter", "on a conflicted file: open the hunk picker"},
		{"e", "open file in editor"},
		{"w", "toggle ignore whitespace in diff"},
	}},
	{"Log", [][2]string{
		{"j / k", "move (older pages load automatically)"},
		{"enter", "focus revision diff"},
	}},
	{"Conflict picker", [][2]string{
		{"n / p", "next / previous conflict"},
		{"m", "keep mine for this conflict"},
		{"t", "keep theirs for this conflict"},
		{"b", "keep both (mine, then theirs)"},
		{"B", "keep base (common ancestor)"},
		{"z", "undo last pick"},
		{"e", "edit file in $EDITOR"},
		{"esc", "back (picks are already saved)"},
	}},
	{"Main view", [][2]string{
		{"j / k", "scroll"},
		{"ctrl+d / ctrl+u", "half page down / up"},
		{"h / l", "pan left / right"},
		{"g / G", "top / bottom"},
		{"esc", "back to panel"},
	}},
}

func helpLines() []string {
	var lines []string
	for _, s := range helpSections {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, style.Title.Render(s.name))
		for _, b := range s.binds {
			lines = append(lines, fmt.Sprintf("  %s %s", style.Key.Render(fmt.Sprintf("%-16s", b[0])), b[1]))
		}
	}
	return lines
}
