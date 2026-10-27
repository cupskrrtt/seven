// Package style holds the colour theme and low-level rendering helpers.
package style

import (
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

// DiffLine renders one diff line in colour.
func DiffLine(l svn.DiffLine) string {
	s := lipgloss.NewStyle()
	switch l.Kind {
	case svn.LineAdded:
		s = s.Foreground(Green)
	case svn.LineRemoved:
		s = s.Foreground(Red)
	case svn.LineHunk:
		s = s.Foreground(Cyan)
	case svn.LineHeader:
		s = s.Bold(true)
	}
	return s.Render(expandTabs(l.Text))
}

func expandTabs(s string) string { return strings.ReplaceAll(s, "\t", "    ") }

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
