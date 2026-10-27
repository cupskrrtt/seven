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
	"github.com/cupskrrtt/seven/internal/config"
	"github.com/cupskrrtt/seven/internal/svn"
	"github.com/cupskrrtt/seven/internal/ui/style"
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
	configPath := flag.String("config", "", "config file (default: "+defaultConfigPath()+")")
	printConfig := flag.Bool("print-config", false, "print an example config file and exit")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	switch {
	case *showVersion:
		fmt.Println("seven", resolveVersion())
		return
	case *printConfig:
		fmt.Print(config.Example)
		return
	}
	if err := run(*path, *configPath); err != nil {
		fmt.Fprintln(os.Stderr, "seven:", err)
		os.Exit(1)
	}
}

func defaultConfigPath() string {
	p, err := config.DefaultPath()
	if err != nil {
		return "none"
	}
	return p
}

func run(path, configPath string) error {
	if configPath == "" {
		configPath = defaultConfigPath()
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	t := cfg.Theme
	style.SetColors(style.Colors{Accent: t.Accent, Green: t.Green, Red: t.Red, Yellow: t.Yellow,
		Cyan: t.Cyan, Magenta: t.Magenta, Muted: t.Muted, SelectedBg: t.SelectedBg})

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

	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if cfg.MouseEnabled() {
		opts = append(opts, tea.WithMouseCellMotion())
	}
	_, err = tea.NewProgram(app.New(client, cfg), opts...).Run()
	return err
}
