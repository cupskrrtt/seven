// Command seven is a terminal UI for Subversion working copies.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cupskrrtt/seven/internal/app"
	"github.com/cupskrrtt/seven/internal/svn"
)

// version is set by release builds via -ldflags "-X main.version=v1.2.3".
var version = ""

// resolveVersion reports the release version if stamped, else the module
// version recorded by `go install ...@v1.2.3`, else the VCS commit of a
// local build.
func resolveVersion() string {
	if version != "" {
		return version
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	if v := bi.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	var rev, modified string
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			if s.Value == "true" {
				modified = ", modified"
			}
		}
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	if rev == "" {
		return "dev"
	}
	return fmt.Sprintf("dev (%s%s)", rev, modified)
}

func main() {
	path := flag.String("path", ".", "path to an svn working copy")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("seven", resolveVersion())
		return
	}
	if err := run(*path); err != nil {
		fmt.Fprintln(os.Stderr, "seven:", err)
		os.Exit(1)
	}
}

func run(path string) error {
	dir, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	client, err := svn.NewClient(dir)
	if errors.Is(err, svn.ErrNotInstalled) {
		return fmt.Errorf("%w\ninstall Subversion (e.g. apt install subversion, brew install subversion, or SlikSVN/TortoiseSVN command-line tools on Windows)", err)
	}
	if err != nil {
		return err
	}
	// Fail fast outside a working copy, and start at its root.
	info, err := client.Info(context.Background())
	if err != nil {
		return err
	}
	if info.WCRoot != "" {
		client.Dir = info.WCRoot
	}

	_, err = tea.NewProgram(app.New(client), tea.WithAltScreen()).Run()
	return err
}
