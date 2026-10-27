package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cupskrrtt/seven/internal/svn"
)

// A shelf stashes local changes as a git-format patch (which svn patch
// re-applies with binary files, adds and property changes), so it works on
// any svn >= 1.9 without svn's experimental shelving.
type shelf struct {
	ID       string    `json:"-"`
	Name     string    `json:"name"`
	Created  time.Time `json:"created"`
	Paths    []string  `json:"paths"`
	Strip    int       `json:"strip"` // svn patch --strip: depth of the working copy in the repo
	Revision int       `json:"revision"`
	Location string    `json:"location"`
}

// shelfDir is per working copy: <state dir>/shelves/<repo uuid>-<wc hash>.
func (m *Model) shelfDir() (string, error) {
	if m.info == nil {
		return "", errors.New("working copy info not loaded yet")
	}
	root := m.shelfRoot
	if root == "" {
		base, err := stateDir()
		if err != nil {
			return "", err
		}
		root = filepath.Join(base, "shelves")
	}
	h := fnv.New32a()
	h.Write([]byte(m.info.WCRoot))
	return filepath.Join(root, fmt.Sprintf("%s-%08x", sanitize(m.info.RepoUUID), h.Sum32())), nil
}

func (m *Model) listShelves() ([]shelf, error) {
	dir, err := m.shelfDir()
	if err != nil {
		return nil, err
	}
	metas, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	var out []shelf
	for _, p := range metas {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var s shelf
		if json.Unmarshal(data, &s) != nil {
			continue
		}
		s.ID = strings.TrimSuffix(filepath.Base(p), ".json")
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID }) // IDs start with the time
	return out, nil
}

func (m *Model) shelfFiles(s shelf) (patch, meta string, err error) {
	dir, err := m.shelfDir()
	return filepath.Join(dir, s.ID+".patch"), filepath.Join(dir, s.ID+".json"), err
}

// shelve saves files' changes to a new shelf and reverts them.
func (m *Model) shelve(ctx context.Context, name string, files []svn.FileStatus) (string, error) {
	var paths []string
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	paths = topLevel(paths)
	patch, err := m.client.DiffGit(ctx, paths...)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(patch) == "" {
		return "", errors.New("nothing to shelve (unversioned files are never shelved; add them first)")
	}
	dir, err := m.shelfDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	s := shelf{
		ID:       time.Now().Format("20060102-150405.000") + "-" + sanitize(name),
		Name:     name,
		Created:  time.Now(),
		Paths:    paths,
		Strip:    svn.PathDepth(m.info.RelativeURL),
		Revision: m.info.Revision,
		Location: m.location().String(),
	}
	patchFile, metaFile, _ := m.shelfFiles(s)
	meta, _ := json.MarshalIndent(s, "", "  ")
	if err := os.WriteFile(patchFile, []byte(patch), 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(metaFile, meta, 0o644); err != nil {
		return "", err
	}

	// Revert. Added files stay on disk after svn revert; their content is in
	// the patch, so remove them (after a backup) so unshelving can re-add them.
	var added []string
	for _, f := range files {
		if f.Item == svn.ItemAdded {
			added = append(added, f.Path)
		}
	}
	if len(added) > 0 {
		if _, err := m.backup(added, ""); err != nil {
			return "", fmt.Errorf("shelf saved, but backing up added files failed (nothing reverted): %w", err)
		}
	}
	if err := m.client.Revert(ctx, paths...); err != nil {
		return "", fmt.Errorf("shelf saved, but revert failed: %w", err)
	}
	for _, p := range added {
		if st, err := os.Stat(m.abs(p)); err == nil && !st.IsDir() {
			os.Remove(m.abs(p))
		}
	}
	return fmt.Sprintf("Shelved %d path(s) as %q", len(paths), name), nil
}

// unshelve applies a shelf; drop removes it afterwards when it applied cleanly.
func (m *Model) unshelve(ctx context.Context, s shelf, drop bool) (string, error) {
	patchFile, metaFile, err := m.shelfFiles(s)
	if err != nil {
		return "", err
	}
	if _, err := m.client.Patch(ctx, patchFile, svn.PatchOptions{Strip: s.Strip}); err != nil {
		if errors.Is(err, svn.ErrPatchRejected) {
			return "", fmt.Errorf("%w\n\nThe shelf %q was kept. Rejected hunks are in *.svnpatch.rej files next to the affected files", err, s.Name)
		}
		return "", err
	}
	if !drop {
		return fmt.Sprintf("Applied shelf %q (kept)", s.Name), nil
	}
	os.Remove(patchFile)
	os.Remove(metaFile)
	return fmt.Sprintf("Unshelved %q", s.Name), nil
}

// shelvesMenu offers shelving the current changes and lists existing shelves.
func (m *Model) shelvesMenu() {
	shelves, err := m.listShelves()
	if err != nil {
		m.popup = &messagePopup{heading: "Shelves", text: err.Error(), isError: true}
		return
	}
	var targets []svn.FileStatus
	for _, f := range m.files {
		if (len(m.selected) == 0 && committable(f)) || m.selected[f.Path] {
			targets = append(targets, f)
		}
	}
	what := fmt.Sprintf("all %d local change(s)", len(targets))
	if len(m.selected) > 0 {
		what = fmt.Sprintf("%d selected file(s)", len(targets))
	}
	var items []menuItem
	if len(targets) > 0 {
		items = append(items, menuItem{"s", "Shelve " + what + "…", "save them in a shelf and revert", func() tea.Cmd {
			p := newInputPopup("Shelve "+what, []string{"Name"}, nil, func(v []string) tea.Cmd {
				name := strings.TrimSpace(v[0])
				if name == "" {
					name = "WIP on " + m.location().String()
				}
				return m.op(func(ctx context.Context) (string, error) { return m.shelve(ctx, name, targets) })
			})
			p.inputs[0].SetValue("WIP on " + m.location().String())
			m.popup = p
			return nil
		}})
	}
	for i, s := range shelves {
		if i == 9 {
			break
		}
		s := s
		label := fmt.Sprintf("%s  %s", s.Name, s.Created.Local().Format("01-02 15:04"))
		desc := fmt.Sprintf("%d path(s), from %s r%d", len(s.Paths), s.Location, s.Revision)
		items = append(items, menuItem{fmt.Sprint(i + 1), label, desc, func() tea.Cmd {
			m.shelfMenu(s)
			return nil
		}})
	}
	if len(items) == 0 {
		m.status = "No local changes to shelve and no shelves yet"
		return
	}
	m.popup = &menuPopup{heading: "Shelves", items: items}
}

func (m *Model) shelfMenu(s shelf) {
	m.popup = &menuPopup{heading: "Shelf · " + s.Name, items: []menuItem{
		{"p", "Pop", "apply it, then delete the shelf", func() tea.Cmd {
			return m.op(func(ctx context.Context) (string, error) { return m.unshelve(ctx, s, true) })
		}},
		{"a", "Apply", "apply it and keep the shelf", func() tea.Cmd {
			return m.op(func(ctx context.Context) (string, error) { return m.unshelve(ctx, s, false) })
		}},
		{"v", "View", "show the shelved diff", func() tea.Cmd {
			patchFile, _, err := m.shelfFiles(s)
			if err != nil {
				return errCmd(err)
			}
			data, err := os.ReadFile(patchFile)
			if err != nil {
				return errCmd(err)
			}
			m.mainKey, m.mainTitle = "shelf:"+s.ID, "Shelf · "+s.Name
			m.setMainText(string(data))
			m.focus = panelMain
			return nil
		}},
		{"d", "Drop", "delete the shelf", func() tea.Cmd {
			m.popup = &confirmPopup{heading: "Drop shelf", text: fmt.Sprintf("Delete shelf %q? Its changes are lost.", s.Name),
				onYes: m.op(func(context.Context) (string, error) {
					patchFile, metaFile, err := m.shelfFiles(s)
					if err != nil {
						return "", err
					}
					os.Remove(metaFile)
					return fmt.Sprintf("Dropped shelf %q", s.Name), os.Remove(patchFile)
				})}
			return nil
		}},
	}}
}
