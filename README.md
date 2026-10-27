# seven

A fast, keyboard-driven terminal UI for **Subversion**, in the spirit of
[lazygit](https://github.com/jesseduffield/lazygit). Written in Go; runs on
Linux, macOS and Windows.

```
╭─ [1] Status ───────────────╮╭─ Diff · src/app/view.go · hunk 1/2 ─────────────────╮
│ branch: feature-login      ││ Index: src/app/view.go                               │
│ r1042 ↓3 behind HEAD r1045 ││ --- src/app/view.go    (revision 1042)               │
╰────────────────────────────╯│ +++ src/app/view.go    (working copy)                │
╭─ [2] Files (4, 2 selected) ╮│▌@@ -12,7 +12,8 @@ func render() {                    │
│ ◐ ▼ src/                   ││▌-    title := "Login"                                │
│ ●     M   view.go          ││▌+    title := i18n("login.title")                    │
│       M   model.go         ││▌+    subtitle := i18n("login.subtitle")              │
│   M   README.md            ││  @@ -40,3 +41,3 @@ func layout() {                  │
│   ?   notes.txt            ││ …                                                    │
╭─ [3] Branches ─────────────╮│                                                      │
│  trunk r1045 alice         ││                                                      │
│* feature-login r1042 you   ││                                                      │
╭─ [4] Log ──────────────────╮│                                                      │
│↓r1045 10-27 alice  Fix CI  ││                                                      │
╰────────────────────────────╯╰──────────────────────────────────────────────────────╯
 space select · c commit · a add · d discard · enter fold · b blame · l log · ? help
```

## Features

- **Files**: a collapsible tree (or flat list) of changes, grouped by **changelist** when you use
  them. Select files, folders or whole changelists for commit; add, revert, delete, ignore, blame,
  view a file's log. Discarding always saves a **backup** first. Refreshes by itself, and as soon
  as you switch back to the terminal.
- **Diffs**: unified or **side by side** (`v`), colourised, with **search** (`/`, `n`/`N`), per-file
  **folding** (`z`, `Z`, `}`/`{`; diffs with more than 10 files open folded) and **hunk-level
  revert** (`]`/`[` then `d`).
- **Any svn command** from inside seven with `:` — the output is shown in the main view and can be
  searched.
- **Commit** from a message box or your `$EDITOR`.
- **Conflicts**: resolve with mine / theirs / base (full or only conflicting parts), or pick
  **hunk by hunk** in a lazygit-style picker with undo.
- **Branches & tags**: list, switch, create, delete, merge, see what's not merged yet, view any
  branch's log and **cherry-pick** from it.
- **Log**: lazily paged history, filter by text / path / revision range, stop-on-copy, revert a
  revision (reverse merge), update the working copy or a single file to any revision.
- **Blame** with jump-to-revision; uncommitted lines are marked.
- **Shelves**: stash changes and bring them back later (pop / apply / view / drop), including
  binary files, added files and property changes.
- **Properties** editor for files, folders and the working-copy root (`svn:ignore`,
  `svn:externals`, `svn:eol-style`, …).
- **Locks**: lock, unlock, see who holds a lock, break a lock.
- **Incoming changes** (`*`): what changed on the server since your last update.
- **Repository browser**: browse any path at any revision, view files, export, check out.
- **Maintenance**: cleanup (optionally removing unversioned / ignored files), relocate.
- **Config file** with custom commands, themes, rebindable keys; mouse support; credentials prompt;
  a command log (`@`) showing every svn command seven runs.

## Requirements

- The **`svn` command-line client**, version 1.9 or newer, on your `PATH`:
  - Debian/Ubuntu: `sudo apt install subversion`
  - Arch: `sudo pacman -S subversion`
  - macOS: `brew install subversion`
  - Windows: [SlikSVN](https://sliksvn.com/download/) or TortoiseSVN with
    "command line client tools" enabled
- A terminal at least 50×16.

seven drives the `svn` binary (using its `--xml` output), so it works with any server svn can
talk to (`svn://`, `http(s)://`, `svn+ssh://`, `file://`) and uses your existing svn credentials.

## Install

With Go 1.22+:

```sh
go install github.com/cupskrrtt/seven/cmd/seven@latest
```

This puts `seven` in `~/go/bin` (make sure it's on your `PATH`). Prebuilt binaries for Linux, macOS
and Windows are attached to each [release](https://github.com/cupskrrtt/seven/releases).

### Build from source

```sh
git clone https://github.com/cupskrrtt/seven
cd seven
go build -o seven ./cmd/seven      # or: go install ./cmd/seven
```

Cross-compile (no CGO needed):

```sh
GOOS=windows GOARCH=amd64 go build -o seven.exe ./cmd/seven
GOOS=darwin  GOARCH=arm64 go build -o seven     ./cmd/seven
```

## Usage

```sh
cd path/to/working-copy && seven
seven --path path/to/working-copy
seven --version
seven --print-config > ~/.config/seven/config.yml   # start a config file
```

Press `?` inside seven for every key binding. The bottom bar always shows the most useful keys for
the focused panel.

### Key bindings

**Global**

| Key | Action |
|---|---|
| `tab` / `shift+tab`, `1`–`4` | switch panel (Status, Files, Branches, Log) |
| `enter` / `esc` | focus the main view / go back |
| `r` | refresh |
| `u` / `U` | update to HEAD / to a revision |
| `*` | toggle incoming changes (`svn status -u`) |
| `B` | repository browser |
| `:` | run an svn command (e.g. `: info`, `: log -l 5 -v`) |
| `x` | actions: cleanup, relocate, shelves, … |
| `@` | command log |
| `?` / `q` | help / quit |

**Files**

| Key | Action |
|---|---|
| `space` | select for commit (on a folder or changelist header: everything inside) |
| `A` | select all / clear |
| `c` / `C` | commit (selected, or all changes) / commit with `$EDITOR` |
| `a` | add unversioned file or folder contents |
| `d` | discard changes (file or folder) — backup saved first |
| `D` | `svn rm` |
| `i` | add to `svn:ignore` |
| `enter` | open/close folder · focus diff · on a conflict: hunk picker |
| `-` / `=` / `` ` `` | collapse all / expand all / tree ↔ flat |
| `b` / `l` | blame / file log |
| `L` | changelists |
| `R` | resolve conflict |
| `S` | shelves |
| `K` | lock / unlock |
| `P` | properties |
| `e` / `w` | open in editor / ignore whitespace in diff |

**Branches**: `space` switch · `n` new branch · `t` new tag · `m` merge into working copy ·
`e` unmerged revisions · `enter` branch log · `D` delete

**Log**: `c` cherry-pick (from another branch's log) · `R` revert revision · `U` update to
revision · `/` filter · `s` stop-on-copy · `esc` reset

**Diff / main view**: `v` side by side ↔ unified · `/` search · `n`/`N` next/previous match ·
`z`/`Z` fold file/all · `}`/`{` next/previous file · `]`/`[` select hunk · `d` revert hunk ·
`h`/`l` pan · `ctrl+d`/`ctrl+u` page. Side by side needs a terminal at least 100 columns wide and
shows line numbers for both versions.

**Conflict picker**: `n`/`p` next/previous conflict · `m` mine · `t` theirs · `b` both ·
`B` base · `z` undo · `e` edit

## Changelists

`L` on a file moves it into a changelist (new or existing) or out of one. As soon as any changelist
exists, the Files panel is split into one section per changelist plus **Changes** for everything
else. On a section header, `space` selects the whole changelist for commit, `enter` folds it, `d`
discards it (with a backup) and the main view shows its combined diff.

## Safety

- Discarding a file, folder or hunk and deleting an unversioned file first copy what's lost to a
  backup folder (the path is shown in the status bar). The newest 50 backups are kept:
  - Linux: `$XDG_STATE_HOME/seven/backups` (default `~/.local/state/seven/backups`)
  - macOS: `~/Library/Application Support/seven/backups`
  - Windows: `%LocalAppData%\seven\backups`

  Each backup holds the original files under `files/` and, for edits, a `changes.patch` you can
  re-apply with `svn patch`.
- Merges, cherry-picks and revision reverts only change your working copy; review and commit them
  yourself.
- Destructive actions (revert, delete, breaking locks, deleting branches, cleanup with removal) ask
  for confirmation.

## Shelves

`S` in the Files panel shelves the selected files (or all local changes) and reverts them; pop or
apply them later, even on another branch. Shelves are stored as git-format patches next to the
backups (in `shelves/`), so they work with any svn ≥ 1.9 and don't depend on svn's experimental
shelving. Unversioned files are not shelved — add them first.

## Configuration

seven reads `config.yml` from your user config directory (`~/.config/seven/config.yml` on Linux,
`~/Library/Application Support/seven/config.yml` on macOS, `%AppData%\seven\config.yml` on
Windows), or the file given with `--config`. Every setting is optional; unknown keys are reported
as errors so typos don't go unnoticed. `seven --print-config` prints a commented example.

```yaml
editor: "code --wait"     # default: $SVN_EDITOR, $VISUAL, $EDITOR, then vi / notepad
refreshSeconds: 10        # status auto-refresh; negative disables
flatFileList: false       # start with a flat list instead of the tree
mouse: true               # click to select, wheel to scroll

theme:                    # "#rrggbb" or ANSI colour numbers; empty = default
  accent: "#ff79c6"
  selectedBg: "#44475a"

sideBySide: false         # start diffs in two columns (toggle with v)

keybindings:              # rebind actions by name: context → action → key
  files:
    commit: "ctrl+s"      # c no longer commits; ctrl+s does
  global:
    quit: "Q"

remap:                    # low-level: "your key": "seven key" (the old key keeps working)
  "ctrl+p": "?"

customCommands:
  - key: "X"
    context: files        # global (default), files, branches or log
    description: "svn info for the selected file"
    command: "svn info {{quote .File}}"
    showOutput: true      # show the output in a popup
  - key: "V"
    context: log
    command: "xdg-open 'https://svn.example.com/viewvc?view=revision&revision={{.Revision}}'"
  - key: "T"
    command: "svn log -v {{quote .File}} | less"   # any interactive program
    context: files
    subprocess: true      # hand it the terminal
    confirm: false        # ask before running
```

### Key bindings

Every rebindable action has a name: the help screen (`?`) shows it next to each key, e.g.
`files.commit` or `diff.side-by-side`. Contexts are `global`, `status`, `files`, `branches`,
`log`, `diff`, `props`, `browser`, `blame` and `conflict`. A rebound action's old key stops
working in that context, and the help screen shows the new key. Unknown contexts or actions and
keys bound twice are reported when seven starts. Keys use Bubble Tea names: `a`, `A`, `ctrl+s`,
`alt+x`, `enter`, `space`, `tab`, `f2`, …

### Custom commands

Custom commands run in the working-copy root through `sh -c` (`cmd /C` on Windows). The command
is a Go template with these fields:

| Field | Value |
|---|---|
| `{{.File}}` | file under the cursor in the Files panel |
| `{{.Files}}` | selected files, or the file under the cursor |
| `{{.Dir}}` | working-copy root |
| `{{.URL}}`, `{{.RepoRoot}}` | working-copy URL, repository root URL |
| `{{.Branch}}` | current location, e.g. `trunk` or `branch: x` |
| `{{.Revision}}` | revision under the cursor in the Log panel |
| `{{.BranchURL}}` | URL of the branch under the cursor in the Branches panel |

Helpers: `{{quote .File}}` shell-quotes a value, `{{quoteAll .Files}}` quotes and joins a list,
`{{join .Files " "}}` joins without quoting. Global commands also appear in the `x` menu.

Custom keys take precedence over built-in keys in their context.

## Tips

- With mouse support on, hold `shift` while dragging to select text in most terminals.
- seven forces English svn messages (`LC_MESSAGES=C`) and, on Linux, a UTF-8 character set so
  non-ASCII file names work even if your locale isn't installed.
- If svn asks for credentials, seven prompts for them and retries; the password is passed to svn
  and never shown in the command log.
- Working copies of a sub-folder (e.g. `trunk/src`) switch and merge with the same sub-folder of
  other branches.

## Development

```sh
go test ./...      # needs svn and svnadmin; svn tests are skipped without them
go vet ./...
```

Tests create throwaway repositories with `svnadmin` and drive both the svn layer
(`internal/svn`) and the UI model (`internal/app`) against them.

Layout:

```
cmd/seven/          entry point and flags
internal/svn/       the only package that runs svn: typed results from --xml output
internal/app/       Bubble Tea model: one file per panel / view
internal/config/    config file
internal/ui/style/  colours and drawing helpers
```

Releases: pushing a `v*` tag builds binaries for all platforms with GoReleaser
(`.github/workflows/release.yml`).
