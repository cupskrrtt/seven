package app

import (
	"github.com/charmbracelet/x/ansi"

	"github.com/cupskrrtt/seven/internal/ui/style"
)

// list tracks the cursor and scroll offset of a scrollable panel.
type list struct {
	cursor int
	offset int
}

// move shifts the cursor by delta, clamped to [0,total).
func (l *list) move(delta, total int) {
	l.set(l.cursor+delta, total)
}

func (l *list) set(i, total int) {
	if total == 0 {
		l.cursor, l.offset = 0, 0
		return
	}
	l.cursor = min(max(i, 0), total-1)
}

// render returns the visible window of lines (height h, width w), keeping
// the cursor in view and highlighting it when the panel is focused.
func (l *list) render(lines []string, w, h int, focused bool) []string {
	if h <= 0 {
		return nil
	}
	if l.cursor < l.offset {
		l.offset = l.cursor
	}
	if l.cursor >= l.offset+h {
		l.offset = l.cursor - h + 1
	}
	l.offset = max(0, min(l.offset, len(lines)-h))

	end := min(len(lines), l.offset+h)
	out := make([]string, 0, end-l.offset)
	for i := l.offset; i < end; i++ {
		line := lines[i]
		if i == l.cursor && focused {
			// Nested ANSI resets would cut the background short, so the
			// selected row is drawn uncoloured.
			line = style.Selected.Render(style.Pad(ansi.Strip(line), w))
		}
		out = append(out, line)
	}
	return out
}
