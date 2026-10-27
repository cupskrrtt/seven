package app

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"github.com/cupskrrtt/seven/internal/svn"
	"github.com/cupskrrtt/seven/internal/ui/style"
)

// autoFoldFiles is the number of files above which a diff opens folded.
const autoFoldFiles = 10

// diffSection is one file of a multi-file diff, from its "Index:" (or
// "Property changes on:") header to the line before the next file.
type diffSection struct {
	start, end     int // inclusive indices into mainRaw
	name           string
	added, removed int
}

// parseSections splits a diff into per-file sections. Lines before the
// first file header (e.g. a revision's message) belong to no section.
func parseSections(lines []svn.DiffLine) (sections []diffSection, sectionOf []int) {
	sectionOf = make([]int, len(lines))
	cur := -1
	for i, l := range lines {
		name, isIndex := strings.CutPrefix(l.Text, "Index: ")
		propName, isProp := strings.CutPrefix(l.Text, "Property changes on: ")
		// A file's property changes follow its text diff in the same section.
		if isIndex || (isProp && (cur < 0 || sections[cur].name != propName)) {
			if isProp {
				name = propName
			}
			if cur >= 0 {
				sections[cur].end = i - 1
			}
			sections = append(sections, diffSection{start: i, name: strings.TrimSpace(name)})
			cur++
		}
		sectionOf[i] = cur
		if cur >= 0 {
			switch l.Kind {
			case svn.LineAdded:
				sections[cur].added++
			case svn.LineRemoved:
				sections[cur].removed++
			}
		}
	}
	if cur >= 0 {
		sections[cur].end = len(lines) - 1
	}
	return sections, sectionOf
}

// resetDiffView recomputes sections, folds and search matches for new
// main-view content. Big diffs start folded.
func (m *Model) resetDiffView() {
	m.mainSections, m.mainSectionOf = parseSections(m.mainRaw)
	m.mainCurFile = 0
	if len(m.mainSections) == 0 {
		m.mainCurFile = -1
	}
	m.mainFolded = map[int]bool{}
	if len(m.mainSections) > autoFoldFiles {
		for i := range m.mainSections {
			m.mainFolded[i] = true
		}
	}
	m.rebuildVisible()
	m.findMatches()
}

// rebuildVisible lists the raw lines shown after folding.
func (m *Model) rebuildVisible() {
	m.mainVisible = m.mainVisible[:0]
	for i := 0; i < len(m.mainRaw); i++ {
		m.mainVisible = append(m.mainVisible, i)
		if s := m.mainSectionOf[i]; s >= 0 && m.mainFolded[s] && i == m.mainSections[s].start {
			i = m.mainSections[s].end // a folded file shows only its header row
		}
	}
	m.buildRows()
	m.clampMainScroll()
}

// diffRow is one screen row of the main view. A full row shows raw line
// raw across the whole width; a side-by-side row pairs an old line (left)
// with a new line (right), either of which may be -1.
type diffRow struct {
	raw          int // first raw line in the row; rows are ordered by it
	full         bool
	left, right  int
	oldNo, newNo int // line numbers in the old and new file (0 = none)
}

func (r diffRow) has(i int) bool { return r.raw == i || r.left == i || r.right == i }

// sideBySideMinWidth is the narrowest terminal that gets two columns.
const sideBySideMinWidth = 100

func (m *Model) sideBySideActive() bool { return m.sideBySide && m.width >= sideBySideMinWidth }

var hunkHeaderRe = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// buildRows lays the visible lines out as screen rows: one per line in
// unified mode; in side-by-side mode, runs of removed and added lines
// inside a hunk are paired up and context appears on both sides.
func (m *Model) buildRows() {
	m.mainRows = m.mainRows[:0]
	if !m.sideBySideActive() {
		for _, i := range m.mainVisible {
			m.mainRows = append(m.mainRows, diffRow{raw: i, full: true, left: -1, right: -1})
		}
		return
	}
	inHunk := false
	oldNo, newNo := 0, 0
	vis := m.mainVisible
	for k := 0; k < len(vis); k++ {
		i := vis[k]
		l := m.mainRaw[i]
		switch {
		case l.Kind == svn.LineHunk:
			if g := hunkHeaderRe.FindStringSubmatch(l.Text); g != nil {
				inHunk = true
				fmt.Sscan(g[1], &oldNo)
				fmt.Sscan(g[2], &newNo)
			} else {
				inHunk = false // a property hunk ("## ... ##")
			}
			m.mainRows = append(m.mainRows, diffRow{raw: i, full: true, left: -1, right: -1})
		case !inHunk || l.Kind == svn.LineHeader || strings.HasPrefix(l.Text, `\`):
			if l.Kind == svn.LineHeader {
				inHunk = false
			}
			m.mainRows = append(m.mainRows, diffRow{raw: i, full: true, left: -1, right: -1})
		case l.Kind == svn.LineContext:
			m.mainRows = append(m.mainRows, diffRow{raw: i, left: i, right: i, oldNo: oldNo, newNo: newNo})
			oldNo++
			newNo++
		default:
			// A run of removed lines followed by added lines.
			var removed, added []int
			for ; k < len(vis) && m.mainRaw[vis[k]].Kind == svn.LineRemoved; k++ {
				removed = append(removed, vis[k])
			}
			for ; k < len(vis) && m.mainRaw[vis[k]].Kind == svn.LineAdded; k++ {
				added = append(added, vis[k])
			}
			k--
			for j := 0; j < max(len(removed), len(added)); j++ {
				row := diffRow{left: -1, right: -1}
				if j < len(removed) {
					row.left, row.oldNo = removed[j], oldNo
					oldNo++
				}
				if j < len(added) {
					row.right, row.newNo = added[j], newNo
					newNo++
				}
				row.raw = row.left
				if row.raw < 0 || (row.right >= 0 && row.right < row.raw) {
					row.raw = row.right
				}
				m.mainRows = append(m.mainRows, row)
			}
		}
	}
}

func (m *Model) clampMainScroll() {
	page := max(1, m.mainHeight()-2)
	m.mainScroll = max(0, min(m.mainScroll, len(m.mainRows)-page))
}

// visibleIndex returns the screen row showing raw line i, unfolding its
// file if needed.
func (m *Model) visibleIndex(i int) int {
	if s := m.mainSectionOf[i]; s >= 0 && m.mainFolded[s] && i != m.mainSections[s].start {
		delete(m.mainFolded, s)
		m.rebuildVisible()
	}
	for v, r := range m.mainRows {
		if r.has(i) {
			return v
		}
	}
	for v, r := range m.mainRows {
		if r.raw >= i {
			return v
		}
	}
	return len(m.mainRows) - 1
}

// toggleSideBySide switches layouts, keeping the top line in place.
func (m *Model) toggleSideBySide() {
	m.sideBySide = !m.sideBySide
	top := -1
	if m.mainScroll < len(m.mainRows) {
		top = m.mainRows[m.mainScroll].raw
	}
	m.buildRows()
	if top >= 0 {
		m.mainScroll = m.visibleIndex(top)
	}
	m.clampMainScroll()
	switch {
	case m.sideBySide && !m.sideBySideActive():
		m.status = fmt.Sprintf("Side-by-side needs a terminal at least %d columns wide", sideBySideMinWidth)
	case m.sideBySide:
		m.status = "Side-by-side diff"
	default:
		m.status = "Unified diff"
	}
}

// renderSideBySide draws a paired row in two columns of width half each.
func (m *Model) renderSideBySide(r diffRow, half int) string {
	side := func(i, no int) string {
		if i < 0 {
			return strings.Repeat(" ", half) // nothing on this side
		}
		l := m.mainRaw[i]
		text := l.Text
		if text != "" {
			text = text[1:] // drop the +/-/space marker
		}
		body := svn.DiffLine{Kind: l.Kind, Text: text}
		var rendered string
		if m.mainSearchRe != nil && m.mainSearchRe.MatchString(style.ExpandTabs(text)) {
			current := m.mainMatch >= 0 && m.mainMatches[m.mainMatch] == i
			rendered = style.DiffLineHighlight(body, m.mainSearchRe, current)
		} else {
			rendered = style.DiffLine(body)
		}
		if m.mainHScroll > 0 {
			rendered = ansi.Cut(rendered, m.mainHScroll, m.mainHScroll+half)
		}
		num := style.Dim.Render(fmt.Sprintf("%4d ", no))
		return style.Pad(num+rendered, half)
	}
	return side(r.left, r.oldNo) + style.Dim.Render("│") + side(r.right, r.newNo)
}

// scrollToRaw makes raw lines from..to visible, scrolling only if needed,
// and makes their file the current one.
func (m *Model) scrollToRaw(from, to int) {
	if s := m.mainSectionOf[from]; s >= 0 {
		m.mainCurFile = s
	}
	vFrom, vTo := m.visibleIndex(from), m.visibleIndex(to)
	inner := m.mainHeight() - 2
	if vFrom < m.mainScroll || vTo >= m.mainScroll+inner {
		m.mainScroll = max(0, vFrom-2)
	}
	m.clampMainScroll()
}

func (m *Model) toggleFold() {
	s := m.mainCurFile
	if s < 0 {
		return
	}
	m.mainFolded[s] = !m.mainFolded[s]
	m.rebuildVisible()
	m.scrollToRaw(m.mainSections[s].start, m.mainSections[s].start)
}

func (m *Model) toggleFoldAll() {
	anyOpen := false
	for i := range m.mainSections {
		if !m.mainFolded[i] {
			anyOpen = true
		}
	}
	m.mainFolded = map[int]bool{}
	if anyOpen {
		for i := range m.mainSections {
			m.mainFolded[i] = true
		}
	}
	m.rebuildVisible()
	if s := m.mainCurFile; s >= 0 {
		m.scrollToRaw(m.mainSections[s].start, m.mainSections[s].start)
	}
}

// jumpFile makes the next (+1) or previous (-1) file current and scrolls
// its header to the top.
func (m *Model) jumpFile(step int) {
	n := len(m.mainSections)
	if n == 0 {
		return
	}
	m.mainCurFile = min(max(m.mainCurFile+step, 0), n-1)
	m.mainScroll = m.visibleIndex(m.mainSections[m.mainCurFile].start)
	m.clampMainScroll()
}

// setSearch compiles query (smart case: case-insensitive unless it has an
// upper-case letter) and jumps to the first match.
func (m *Model) setSearch(query string) {
	m.mainSearch, m.mainSearchRe = query, nil
	if query != "" {
		pattern := regexp.QuoteMeta(query)
		if !strings.ContainsFunc(query, unicode.IsUpper) {
			pattern = "(?i)" + pattern
		}
		m.mainSearchRe = regexp.MustCompile(pattern)
	}
	m.findMatches()
	if len(m.mainMatches) > 0 {
		m.mainMatch = 0
		m.scrollToRaw(m.mainMatches[0], m.mainMatches[0])
		m.status = fmt.Sprintf("%d match(es) for %q", len(m.mainMatches), query)
	} else if query != "" {
		m.status = fmt.Sprintf("No matches for %q", query)
	}
}

func (m *Model) findMatches() {
	m.mainMatches, m.mainMatch = nil, -1
	if m.mainSearchRe == nil {
		return
	}
	for i, l := range m.mainRaw {
		if m.mainSearchRe.MatchString(style.ExpandTabs(l.Text)) {
			m.mainMatches = append(m.mainMatches, i)
		}
	}
}

// nextMatch moves to the next (+1) or previous (-1) match, wrapping.
func (m *Model) nextMatch(step int) {
	n := len(m.mainMatches)
	if n == 0 {
		m.status = fmt.Sprintf("No matches for %q", m.mainSearch)
		return
	}
	if m.mainMatch < 0 {
		m.mainMatch = 0
	} else {
		m.mainMatch = (m.mainMatch + step + n) % n
	}
	i := m.mainMatches[m.mainMatch]
	m.scrollToRaw(i, i)
	m.status = fmt.Sprintf("Match %d/%d", m.mainMatch+1, n)
}

// renderMainLine draws raw line i: folded file rows, fold arrows on file
// headers and search highlights.
func (m *Model) renderMainLine(i int) string {
	l := m.mainRaw[i]
	if s := m.mainSectionOf[i]; s >= 0 && i == m.mainSections[s].start && len(m.mainSections) > 1 {
		sec := m.mainSections[s]
		arrow := style.Title
		if s == m.mainCurFile {
			arrow = style.Current
		}
		if m.mainFolded[s] {
			return arrow.Render("▶ "+sec.name) + "  " +
				style.DiffLine(svn.DiffLine{Kind: svn.LineAdded, Text: fmt.Sprintf("+%d", sec.added)}) + " " +
				style.DiffLine(svn.DiffLine{Kind: svn.LineRemoved, Text: fmt.Sprintf("-%d", sec.removed)})
		}
		return arrow.Render("▼ ") + m.styledLine(i, l)
	}
	return m.styledLine(i, l)
}

func (m *Model) styledLine(i int, l svn.DiffLine) string {
	if m.mainSearchRe != nil && m.mainSearchRe.MatchString(style.ExpandTabs(l.Text)) {
		current := m.mainMatch >= 0 && m.mainMatches[m.mainMatch] == i
		return style.DiffLineHighlight(l, m.mainSearchRe, current)
	}
	return m.mainLines[i]
}
