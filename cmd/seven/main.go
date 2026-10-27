// Command seven is a terminal UI for Subversion working copies.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cupskrrtt/seven/internal/app"
	"github.com/cupskrrtt/seven/internal/svn"
)

var version = "dev"

func main() {
	path := flag.String("path", ".", "path to an svn working copy")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("seven", version)
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
