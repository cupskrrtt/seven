package app

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// keepBackups is how many backup folders are kept; older ones are pruned.
const keepBackups = 50

// backupRootDir is where backups go: <state dir>/backups.
func (m *Model) backupRootDir() (string, error) {
	if m.backupRoot != "" {
		return m.backupRoot, nil
	}
	base, err := stateDir()
	return filepath.Join(base, "backups"), err
}

// stateDir holds seven's data (backups, shelves): $XDG_STATE_HOME/seven
// (~/.local/state/seven) on Linux, the per-user app data folder elsewhere.
func stateDir() (string, error) {
	var base string
	switch runtime.GOOS {
	case "windows":
		dir, err := os.UserCacheDir() // %LocalAppData%
		if err != nil {
			return "", err
		}
		base = dir
	case "darwin":
		dir, err := os.UserConfigDir() // ~/Library/Application Support
		if err != nil {
			return "", err
		}
		base = dir
	default:
		base = os.Getenv("XDG_STATE_HOME")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(home, ".local", "state")
		}
	}
	return filepath.Join(base, "seven"), nil
}

// backup saves copies of relPaths (files or directories) and, if
// non-empty, their diff as changes.patch (re-apply with `svn patch`). It
// returns the backup folder, or "" when there was nothing to save.
func (m *Model) backup(relPaths []string, diff string) (string, error) {
	if len(relPaths) == 0 {
		return "", nil
	}
	root, err := m.backupRootDir()
	if err != nil {
		return "", err
	}
	label := sanitize(filepath.Base(relPaths[0]))
	if len(relPaths) > 1 {
		label += fmt.Sprintf("+%d", len(relPaths)-1)
	}
	dir := filepath.Join(root, time.Now().Format("20060102-150405.000")+"-"+label)
	saved := false

	for _, rel := range relPaths {
		src := m.abs(rel)
		st, err := os.Stat(src)
		if err != nil {
			continue // missing or already deleted: nothing to copy
		}
		if err := copyTree(src, filepath.Join(dir, "files", rel), st); err != nil {
			return "", err
		}
		saved = true
	}
	if strings.TrimSpace(diff) != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(dir, "changes.patch"), []byte(diff), 0o644); err != nil {
			return "", err
		}
		saved = true
	}
	if !saved {
		return "", nil
	}
	pruneBackups(root, keepBackups)
	return dir, nil
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 32 {
			return '_'
		}
		return r
	}, s)
}

// copyTree copies a file or directory, skipping .svn administrative areas.
func copyTree(src, dst string, st fs.FileInfo) error {
	if !st.IsDir() {
		return copyFile(src, dst, st.Mode().Perm())
	}
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".svn" {
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil // symlinks, devices: not worth backing up
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return copyFile(p, target, info.Mode().Perm())
	})
}

func copyFile(src, dst string, perm fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("copy %s: %w", src, err)
	}
	return out.Close()
}

// pruneBackups deletes all but the newest keep folders (names sort by time).
func pruneBackups(root string, keep int) {
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) <= keep {
		return
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, n := range names[:max(0, len(names)-keep)] {
		os.RemoveAll(filepath.Join(root, n))
	}
}
