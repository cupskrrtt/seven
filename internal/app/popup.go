package app

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/cupskrrtt/seven/internal/ui/style"
)

// popup is a modal that receives all input while open.
type popup interface {
	// update handles a message; close reports whether the popup is done.
	update(msg tea.Msg) (close bool, cmd tea.Cmd)
	title() string
	body(w int) []string
	hint() string
}

// confirmPopup asks a yes/no question. On yes it returns onYes, or calls
// onYesFn on the UI goroutine when the action must change model state.
type confirmPopup struct {
	heading string
	text    string
	onYes   tea.Cmd
	onYesFn func() tea.Cmd
}

func (p *confirmPopup) update(msg tea.Msg) (bool, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "y", "enter":
			if p.onYesFn != nil {
				return true, p.onYesFn()
			}
			return true, p.onYes
		case "n", "esc", "q":
			return true, nil
		}
	}
	return false, nil
}
func (p *confirmPopup) title() string       { return p.heading }
func (p *confirmPopup) body(w int) []string { return wrap(p.text, w) }
func (p *confirmPopup) hint() string        { return "enter/y confirm · esc cancel" }

// menuItem is one entry of a menuPopup. run executes on the UI goroutine,
// so it may change model state (including opening another popup).
type menuItem struct {
	key   string
	label string
	desc  string
	run   func() tea.Cmd
}

// menuPopup lets the user pick an action by moving or by its shortcut key.
type menuPopup struct {
	heading string
	items   []menuItem
	list    list
}

func (p *menuPopup) update(msg tea.Msg) (bool, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return false, nil
	}
	switch k.String() {
	case "esc", "q":
		return true, nil
	case "j", "down":
		p.list.move(1, len(p.items))
	case "k", "up":
		p.list.move(-1, len(p.items))
	case "enter":
		return true, p.items[p.list.cursor].run()
	default:
		for _, it := range p.items {
			if it.key == k.String() {
				return true, it.run()
			}
		}
	}
	return false, nil
}
func (p *menuPopup) title() string { return p.heading }
func (p *menuPopup) body(w int) []string {
	lines := make([]string, len(p.items))
	for i, it := range p.items {
		lines[i] = fmt.Sprintf("%s  %-22s %s", style.Key.Render(it.key), it.label, style.Dim.Render(it.desc))
	}
	return p.list.render(lines, w, len(lines), true)
}
func (p *menuPopup) hint() string { return "enter / shortcut select · esc cancel" }

// messagePopup shows text (errors, update results) until dismissed.
type messagePopup struct {
	heading string
	text    string
	isError bool
}

func (p *messagePopup) update(msg tea.Msg) (bool, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc", "enter", "q", " ":
			return true, nil
		}
	}
	return false, nil
}
func (p *messagePopup) title() string { return p.heading }
func (p *messagePopup) body(w int) []string {
	lines := wrap(p.text, w)
	if p.isError {
		for i, l := range lines {
			lines[i] = style.ErrorText.Render(l)
		}
	}
	return lines
}
func (p *messagePopup) hint() string { return "esc close" }

// inputPopup collects one or more single-line values.
type inputPopup struct {
	heading  string
	labels   []string
	inputs   []textinput.Model
	focus    int
	onSubmit func(values []string) tea.Cmd
}

func newInputPopup(heading string, labels []string, masked []bool, onSubmit func([]string) tea.Cmd) *inputPopup {
	p := &inputPopup{heading: heading, labels: labels, onSubmit: onSubmit}
	for i := range labels {
		ti := textinput.New()
		ti.Prompt = ""
		if i < len(masked) && masked[i] {
			ti.EchoMode = textinput.EchoPassword
		}
		p.inputs = append(p.inputs, ti)
	}
	p.inputs[0].Focus()
	return p
}

func (p *inputPopup) update(msg tea.Msg) (bool, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc":
			return true, nil
		case "tab", "down", "shift+tab", "up":
			p.inputs[p.focus].Blur()
			step := 1
			if k.String() == "shift+tab" || k.String() == "up" {
				step = len(p.inputs) - 1
			}
			p.focus = (p.focus + step) % len(p.inputs)
			return false, p.inputs[p.focus].Focus()
		case "enter":
			if p.focus < len(p.inputs)-1 {
				p.inputs[p.focus].Blur()
				p.focus++
				return false, p.inputs[p.focus].Focus()
			}
			values := make([]string, len(p.inputs))
			for i, in := range p.inputs {
				values[i] = in.Value()
			}
			return true, p.onSubmit(values)
		}
	}
	var cmd tea.Cmd
	p.inputs[p.focus], cmd = p.inputs[p.focus].Update(msg)
	return false, cmd
}
func (p *inputPopup) title() string { return p.heading }
func (p *inputPopup) body(w int) []string {
	var lines []string
	for i, in := range p.inputs {
		in.Width = max(1, w-lipgloss.Width(p.labels[i])-3)
		lines = append(lines, style.Dim.Render(p.labels[i]+": ")+in.View())
	}
	return lines
}
func (p *inputPopup) hint() string { return "enter submit · tab next field · esc cancel" }

// commitPopup edits a commit message.
type commitPopup struct {
	heading  string
	ta       textarea.Model
	onSubmit func(msg string) tea.Cmd
}

func newCommitPopup(heading string, onSubmit func(string) tea.Cmd) *commitPopup {
	ta := textarea.New()
	ta.Placeholder = "Commit message"
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.SetHeight(8)
	ta.CharLimit = 0
	// enter commits; alt+enter or ctrl+j inserts a newline.
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter", "ctrl+j"))
	ta.Focus()
	return &commitPopup{heading: heading, ta: ta, onSubmit: onSubmit}
}

func (p *commitPopup) update(msg tea.Msg) (bool, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc":
			return true, nil
		case "enter":
			text := strings.TrimSpace(p.ta.Value())
			if text == "" {
				return false, nil
			}
			return true, p.onSubmit(text)
		}
	}
	var cmd tea.Cmd
	p.ta, cmd = p.ta.Update(msg)
	return false, cmd
}
func (p *commitPopup) title() string { return p.heading }
func (p *commitPopup) body(w int) []string {
	p.ta.SetWidth(w)
	return strings.Split(p.ta.View(), "\n")
}
func (p *commitPopup) hint() string { return "enter commit · alt+enter/ctrl+j newline · esc cancel" }

// helpPopup lists key bindings.
type helpPopup struct {
	lines []string
	list  list
}

func (p *helpPopup) update(msg tea.Msg) (bool, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc", "q", "?", "enter":
			return true, nil
		case "j", "down":
			p.list.move(1, len(p.lines))
		case "k", "up":
			p.list.move(-1, len(p.lines))
		}
	}
	return false, nil
}
func (p *helpPopup) title() string { return "Keybindings" }
func (p *helpPopup) body(w int) []string {
	return p.list.render(p.lines, w, 20, false)
}
func (p *helpPopup) hint() string { return "j/k scroll · esc close" }

// renderPopup draws p as a bordered box of width w.
func renderPopup(p popup, w int) string {
	inner := w - 2
	lines := p.body(inner - 2)
	for i, l := range lines {
		lines[i] = " " + l
	}
	lines = append(lines, "", " "+style.Dim.Render(p.hint()))
	return style.Box(p.title(), lines, w, len(lines)+2, true)
}

// overlay draws fg centred on top of bg.
func overlay(bg, fg string, width, height int) string {
	bgLines := strings.Split(bg, "\n")
	fgLines := strings.Split(fg, "\n")
	fgW := lipgloss.Width(fg)
	x := max(0, (width-fgW)/2)
	y := max(0, (height-len(fgLines))/2)
	for i, fl := range fgLines {
		row := y + i
		if row >= len(bgLines) {
			break
		}
		bl := bgLines[row]
		left := ansi.Truncate(bl, x, "")
		left += strings.Repeat(" ", max(0, x-lipgloss.Width(left)))
		right := ansi.Cut(bl, x+fgW, width)
		bgLines[row] = left + "\x1b[0m" + fl + "\x1b[0m" + right
	}
	return strings.Join(bgLines, "\n")
}

// wrap splits text on newlines and soft-wraps each line to w.
func wrap(text string, w int) []string {
	return strings.Split(ansi.Wrap(text, max(1, w), ""), "\n")
}
