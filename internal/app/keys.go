package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cupskrrtt/seven/internal/ui/style"
)

// binding is one key binding. Named bindings can be rebound in the config
// (keybindings: {context: {name: key}}); unnamed ones are fixed.
type binding struct {
	name string
	key  string
	desc string
}

// keySection groups the bindings of one context; it drives both the help
// screen and rebinding.
type keySection struct {
	title   string
	context string
	binds   []binding
}

var keySections = []keySection{
	{"Global", "global", []binding{
		{"next-panel", "tab", "next panel"},
		{"prev-panel", "shift+tab", "previous panel"},
		{"", "1 2 3 4", "jump to Status / Files / Branches / Log"},
		{"", "enter", "focus main view"},
		{"refresh", "r", "refresh"},
		{"update", "u", "update to HEAD"},
		{"update-to-revision", "U", "update to revision"},
		{"actions", "x", "actions: cleanup, relocate, shelves, browser, …"},
		{"incoming", "*", "toggle incoming changes (svn status -u)"},
		{"browser", "B", "repository browser"},
		{"command", ":", "run an svn command"},
		{"command-log", "@", "toggle command log"},
		{"help", "?", "this help"},
		{"quit", "q", "quit"},
	}},
	{"Status", "status", []binding{
		{"properties", "P", "properties of the working-copy root"},
	}},
	{"Files", "files", []binding{
		{"", "enter", "folder: collapse / expand · file: focus diff (conflict: hunk picker)"},
		{"collapse-all", "-", "collapse all"},
		{"expand-all", "=", "expand all"},
		{"toggle-tree", "`", "toggle tree / flat list"},
		{"select", "space", "select / deselect for commit (folder, changelist: all inside)"},
		{"select-all", "A", "select all / clear selection"},
		{"commit", "c", "commit selected (or all changes)"},
		{"commit-editor", "C", "commit using $SVN_EDITOR / $EDITOR"},
		{"add", "a", "add unversioned file (folder: all inside)"},
		{"discard", "d", "discard changes, file or whole folder (backup saved first)"},
		{"remove", "D", "svn rm (schedule deletion)"},
		{"ignore", "i", "add to svn:ignore"},
		{"blame", "b", "blame"},
		{"file-log", "l", "show this file's log"},
		{"changelist", "L", "changelist: move / remove / select all"},
		{"resolve", "R", "resolve conflict (mine / theirs / base / pick hunks…)"},
		{"edit", "e", "open file in editor"},
		{"ignore-whitespace", "w", "toggle ignore whitespace in diff"},
		{"shelves", "S", "shelves: shelve changes / pop / apply / drop"},
		{"lock", "K", "lock / unlock (break someone else's lock)"},
		{"properties", "P", "properties of file or folder"},
	}},
	{"Branches", "branches", []binding{
		{"switch", "space", "switch working copy to branch / tag"},
		{"new-branch", "n", "new branch from current location"},
		{"new-tag", "t", "new tag from current location"},
		{"merge", "m", "merge branch into working copy"},
		{"unmerged", "e", "list revisions not merged yet"},
		{"branch-log", "enter", "show branch log (then c to cherry-pick)"},
		{"delete", "D", "delete branch / tag"},
	}},
	{"Log", "log", []binding{
		{"", "j / k", "move (older pages load automatically)"},
		{"", "enter", "focus revision diff"},
		{"cherry-pick", "c", "cherry-pick revision (from another branch's log)"},
		{"revert-revision", "R", "revert revision (reverse merge, then commit)"},
		{"update-to-revision", "U", "update working copy / file to revision"},
		{"filter", "/", "filter: search text, path, revision range"},
		{"stop-on-copy", "s", "toggle stop-on-copy"},
		{"reset", "esc", "reset to working copy log"},
	}},
	{"Diff (main view)", "diff", []binding{
		{"search", "/", "search (smart case); esc clears"},
		{"next-match", "n", "next match (no search: next hunk)"},
		{"prev-match", "N", "previous match (no search: previous hunk)"},
		{"side-by-side", "v", "toggle side-by-side / unified"},
		{"fold", "z", "fold / unfold the current file"},
		{"fold-all", "Z", "fold / unfold all files (10+ files open folded)"},
		{"next-file", "}", "next file"},
		{"prev-file", "{", "previous file"},
		{"next-hunk", "]", "select next hunk"},
		{"prev-hunk", "[", "select previous hunk"},
		{"revert-hunk", "d", "revert selected hunk (backup saved first)"},
		{"", "j / k", "scroll"},
		{"", "ctrl+d / ctrl+u", "half page down / up"},
		{"", "h / l", "pan left / right"},
		{"", "g / G", "top / bottom"},
		{"", "esc", "back to panel"},
	}},
	{"Properties", "props", []binding{
		{"edit", "e", "edit value in $EDITOR"},
		{"add", "a", "add (svn:ignore, svn:externals, … or custom)"},
		{"delete", "d", "delete"},
	}},
	{"Repository browser", "browser", []binding{
		{"", "enter / l", "open folder / view file (then / to search)"},
		{"", "h / backspace", "parent folder"},
		{"revision", "R", "browse at a revision"},
		{"log", "L", "log of the entry"},
		{"export", "x", "export"},
		{"checkout", "c", "check out"},
		{"lock", "K", "lock / unlock file"},
		{"properties", "P", "properties (read-only)"},
	}},
	{"Blame", "blame", []binding{
		{"", "j / k", "move"},
		{"show-revision", "enter", "show the revision of this line"},
		{"", "esc", "back"},
	}},
	{"Conflict picker", "conflict", []binding{
		{"next", "n", "next conflict"},
		{"prev", "p", "previous conflict"},
		{"mine", "m", "keep mine for this conflict"},
		{"theirs", "t", "keep theirs for this conflict"},
		{"both", "b", "keep both (mine, then theirs)"},
		{"base", "B", "keep base (common ancestor)"},
		{"undo", "z", "undo last pick"},
		{"edit", "e", "edit file in $EDITOR"},
		{"", "esc", "back (picks are already saved)"},
	}},
}

// keymap applies user keybindings: in each context, a rebound action's new
// key is translated to its built-in key, and the old key stops working.
type keymap struct {
	to      map[string]map[string]string // context → user key → built-in key
	blocked map[string]map[string]bool   // context → built-in keys that were moved
	shown   map[string]map[string]string // context → action → effective key (help)
}

// newKeymap validates bindings ({context: {action: key}}) and builds the map.
func newKeymap(bindings map[string]map[string]string) (*keymap, error) {
	km := &keymap{to: map[string]map[string]string{}, blocked: map[string]map[string]bool{}, shown: map[string]map[string]string{}}
	for ctx, actions := range bindings {
		var sec *keySection
		for i := range keySections {
			if keySections[i].context == ctx {
				sec = &keySections[i]
			}
		}
		if sec == nil {
			return nil, fmt.Errorf("keybindings: unknown context %q (use %s)", ctx, strings.Join(keyContexts(), ", "))
		}
		km.to[ctx], km.blocked[ctx], km.shown[ctx] = map[string]string{}, map[string]bool{}, map[string]string{}
		for action, key := range actions {
			def := ""
			for _, b := range sec.binds {
				if b.name == action && action != "" {
					def = b.key
				}
			}
			if def == "" {
				return nil, fmt.Errorf("keybindings: %s has no action %q (use %s)", ctx, action, strings.Join(actionNames(*sec), ", "))
			}
			key, def = keyString(key), keyString(def)
			if key == "" || key == def {
				continue
			}
			if prev, dup := km.to[ctx][key]; dup {
				return nil, fmt.Errorf("keybindings: %s: key %q is bound to two actions (%s)", ctx, key, prev)
			}
			km.to[ctx][key] = def
			km.blocked[ctx][def] = true
			km.shown[ctx][action] = key
		}
		// A key moved away and then reused by another action is not blocked.
		for key := range km.to[ctx] {
			delete(km.blocked[ctx], key)
		}
	}
	return km, nil
}

// keyString converts a key as written in the table or config to what Bubble
// Tea reports: the space bar arrives as " ", not "space".
func keyString(k string) string {
	if k == "space" {
		return " "
	}
	return k
}

// translate maps a pressed key to the built-in key for the given context
// (falling back to global). ok=false means the key was rebound away.
func (km *keymap) translate(ctx, key string) (string, bool) {
	if km == nil {
		return key, true
	}
	for _, c := range []string{ctx, "global"} {
		if def, ok := km.to[c][key]; ok {
			return def, true
		}
		if km.blocked[c][key] {
			return "", false
		}
	}
	return key, true
}

func keyContexts() []string {
	var out []string
	for _, s := range keySections {
		out = append(out, s.context)
	}
	return out
}

func actionNames(s keySection) []string {
	var out []string
	for _, b := range s.binds {
		if b.name != "" {
			out = append(out, b.name)
		}
	}
	sort.Strings(out)
	return out
}

// keyContext names the context whose bindings apply right now.
func (m *Model) keyContext() string {
	if m.focus == panelMain {
		switch {
		case m.conflict != nil:
			return "conflict"
		case m.blame != nil:
			return "blame"
		case m.props != nil:
			return "props"
		case m.browser != nil && !m.browser.viewing:
			return "browser"
		}
		return "diff"
	}
	return map[panel]string{panelStatus: "status", panelFiles: "files", panelBranches: "branches", panelLog: "log"}[m.focus]
}

func (m *Model) helpLines() []string {
	var lines []string
	for _, s := range keySections {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, style.Title.Render(s.title))
		for _, b := range s.binds {
			key := b.key
			if m.keys != nil {
				if k, ok := m.keys.shown[s.context][b.name]; ok && b.name != "" {
					key = k
				}
			}
			name := ""
			if b.name != "" {
				name = style.Dim.Render("  " + s.context + "." + b.name)
			}
			lines = append(lines, fmt.Sprintf("  %s %s%s", style.Key.Render(fmt.Sprintf("%-16s", key)), b.desc, name))
		}
	}
	return lines
}
