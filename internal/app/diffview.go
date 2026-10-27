package app

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

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
	m.clampMainScroll()
}

func (m *Model) clampMainScroll() {
	page := max(1, m.mainHeight()-2)
	m.mainScroll = max(0, min(m.mainScroll, len(m.mainVisible)-page))
}

// visibleIndex returns the visible row showing raw line i, unfolding its
// file if needed.
func (m *Model) visibleIndex(i int) int {
	if s := m.mainSectionOf[i]; s >= 0 && m.mainFolded[s] && i != m.mainSections[s].start {
		delete(m.mainFolded, s)
		m.rebuildVisible()
	}
	for v, r := range m.mainVisible {
		if r >= i {
			return v
		}
	}
	return len(m.mainVisible) - 1
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
