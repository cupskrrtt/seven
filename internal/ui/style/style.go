// Package style holds the colour theme and low-level rendering helpers.
package style

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/cupskrrtt/seven/internal/svn"
)

var (
	Accent   = lipgloss.AdaptiveColor{Light: "#0b6bcb", Dark: "#5fafff"}
	Muted    = lipgloss.AdaptiveColor{Light: "#6b6b6b", Dark: "#8a8a8a"}
	Green    = lipgloss.AdaptiveColor{Light: "#1a7f37", Dark: "#56d364"}
	Red      = lipgloss.AdaptiveColor{Light: "#cf222e", Dark: "#ff7b72"}
	Yellow   = lipgloss.AdaptiveColor{Light: "#9a6700", Dark: "#e3b341"}
	Cyan     = lipgloss.AdaptiveColor{Light: "#1b7c83", Dark: "#56d4dd"}
	Magenta  = lipgloss.AdaptiveColor{Light: "#8250df", Dark: "#d2a8ff"}
	SelectBG = lipgloss.AdaptiveColor{Light: "#dbe9f9", Dark: "#264f78"}

	Bold      = lipgloss.NewStyle().Bold(true)
	Dim       = lipgloss.NewStyle().Foreground(Muted)
	Selected  = lipgloss.NewStyle().Background(SelectBG)
	Title     = lipgloss.NewStyle().Foreground(Accent).Bold(true)
	ErrorText = lipgloss.NewStyle().Foreground(Red)
	Key       = lipgloss.NewStyle().Foreground(Accent)
	Current   = lipgloss.NewStyle().Foreground(Yellow).Bold(true).Underline(true)
)

// ItemStyle colours a status code.
func ItemStyle(f svn.FileStatus) lipgloss.Style {
	s := lipgloss.NewStyle()
	switch {
	case f.Conflicted():
		return s.Foreground(Red).Bold(true)
	case f.Item == svn.ItemAdded:
		return s.Foreground(Green)
	case f.Item == svn.ItemDeleted, f.Item == svn.ItemMissing:
		return s.Foreground(Red)
	case f.Item == svn.ItemModified, f.Item == svn.ItemReplaced, f.Props == svn.ItemModified:
		return s.Foreground(Yellow)
	case f.Item == svn.ItemUnversioned:
		return s.Foreground(Muted)
	}
	return s.Foreground(Magenta)
}

func diffStyle(k svn.LineKind) lipgloss.Style {
	s := lipgloss.NewStyle()
	switch k {
	case svn.LineAdded:
		s = s.Foreground(Green)
	case svn.LineRemoved:
		s = s.Foreground(Red)
	case svn.LineHunk:
		s = s.Foreground(Cyan)
	case svn.LineHeader:
		s = s.Bold(true)
	}
	return s
}

// DiffLine renders one diff line in colour.
func DiffLine(l svn.DiffLine) string {
	return diffStyle(l.Kind).Render(ExpandTabs(l.Text))
}

// Match is the highlight for search hits.
var Match = lipgloss.NewStyle().Background(Yellow).Foreground(lipgloss.Color("#000000"))

// DiffLineHighlight renders l like DiffLine with every match of re
// highlighted; current marks the line holding the selected match.
func DiffLineHighlight(l svn.DiffLine, re *regexp.Regexp, current bool) string {
	text := ExpandTabs(l.Text)
	base := diffStyle(l.Kind)
	hit := Match
	if current {
		hit = hit.Bold(true).Underline(true)
	}
	var b strings.Builder
	last := 0
	for _, loc := range re.FindAllStringIndex(text, -1) {
		if loc[0] == loc[1] {
			continue
		}
		b.WriteString(base.Render(text[last:loc[0]]))
		b.WriteString(hit.Render(text[loc[0]:loc[1]]))
		last = loc[1]
	}
	b.WriteString(base.Render(text[last:]))
	return b.String()
}

// ExpandTabs replaces tabs with four spaces so widths are predictable.
func ExpandTabs(s string) string { return strings.ReplaceAll(s, "\t", "    ") }

// Box draws a rounded border of exactly w×h cells with title set into the
// top edge. Lines are clipped to fit; missing lines are padded.
func Box(title string, lines []string, w, h int, focused bool) string {
	if w < 4 || h < 2 {
		return ""
	}
	border := lipgloss.NewStyle().Foreground(Muted)
	titleStyle := Dim
	if focused {
		border = border.Foreground(Accent)
		titleStyle = Title
	}
	inner := w - 2

	t := ansi.Truncate(" "+title+" ", inner-1, "…")
	top := border.Render("╭─") + titleStyle.Render(t) +
		border.Render(strings.Repeat("─", max(0, inner-1-lipgloss.Width(t)))+"╮")

	var b strings.Builder
	b.WriteString(top)
	side := border.Render("│")
	for i := 0; i < h-2; i++ {
		line := ""
		if i < len(lines) {
			line = ansi.Truncate(lines[i], inner, "")
		}
		b.WriteString("\n" + side + line + strings.Repeat(" ", max(0, inner-lipgloss.Width(line))) + side)
	}
	b.WriteString("\n" + border.Render("╰"+strings.Repeat("─", inner)+"╯"))
	return b.String()
}

// Pad right-pads s with spaces to width w (used for full-width selection bars).
func Pad(s string, w int) string {
	s = ansi.Truncate(s, w, "")
	return s + strings.Repeat(" ", max(0, w-lipgloss.Width(s)))
}
