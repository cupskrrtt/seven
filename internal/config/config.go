// Package config loads seven's optional YAML configuration file.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config is the user configuration. Every field is optional.
type Config struct {
	// Editor overrides $SVN_EDITOR / $VISUAL / $EDITOR, e.g. "code --wait".
	Editor string `yaml:"editor"`
	// RefreshSeconds is the status auto-refresh interval; 0 means the
	// default (10), a negative value disables auto-refresh.
	RefreshSeconds int `yaml:"refreshSeconds"`
	// FlatFileList starts the Files panel as a flat list instead of a tree.
	FlatFileList bool `yaml:"flatFileList"`
	// Mouse enables clicking and scrolling (default true).
	Mouse *bool `yaml:"mouse"`
	// Theme overrides colours; values are "#rrggbb" or ANSI numbers ("12").
	Theme Theme `yaml:"theme"`
	// Remap translates keys before seven sees them: {"your key": "seven key"}.
	Remap map[string]string `yaml:"remap"`
	// CustomCommands bind keys to shell commands.
	CustomCommands []CustomCommand `yaml:"customCommands"`
}

// Theme colours. Empty values keep the built-in adaptive colours.
type Theme struct {
	Accent     string `yaml:"accent"`
	Green      string `yaml:"green"`
	Red        string `yaml:"red"`
	Yellow     string `yaml:"yellow"`
	Cyan       string `yaml:"cyan"`
	Magenta    string `yaml:"magenta"`
	Muted      string `yaml:"muted"`
	SelectedBg string `yaml:"selectedBg"`
}

// CustomCommand runs a shell command on a key press. Command is a Go
// text/template; see the README for the available fields.
type CustomCommand struct {
	Key         string `yaml:"key"`
	Context     string `yaml:"context"` // global (default), files, branches, log
	Command     string `yaml:"command"`
	Description string `yaml:"description"`
	Confirm     bool   `yaml:"confirm"`    // ask before running
	ShowOutput  bool   `yaml:"showOutput"` // show output in a popup
	Subprocess  bool   `yaml:"subprocess"` // hand the terminal to it (interactive)
}

// Contexts a custom command can be bound in.
var Contexts = map[string]bool{"global": true, "files": true, "branches": true, "log": true}

// MouseEnabled reports the effective mouse setting.
func (c *Config) MouseEnabled() bool { return c.Mouse == nil || *c.Mouse }

// DefaultPath is <user config dir>/seven/config.yml, e.g.
// ~/.config/seven/config.yml on Linux.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "seven", "config.yml"), nil
}

// Load reads the config at path. A missing file yields the defaults;
// unknown keys are errors so typos don't go unnoticed.
func Load(path string) (*Config, error) {
	cfg := &Config{}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(cfg); err != nil && !errors.Is(err, io.EOF) { // empty file is fine
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, cfg.validate(path)
}

func (c *Config) validate(path string) error {
	seen := map[string]bool{}
	for i, cc := range c.CustomCommands {
		if cc.Key == "" || cc.Command == "" {
			return fmt.Errorf("%s: customCommands[%d] needs both key and command", path, i)
		}
		ctx := cc.Context
		if ctx == "" {
			ctx = "global"
			c.CustomCommands[i].Context = ctx
		}
		if !Contexts[ctx] {
			return fmt.Errorf("%s: customCommands[%d]: unknown context %q (use global, files, branches or log)", path, i, cc.Context)
		}
		if seen[ctx+" "+cc.Key] {
			return fmt.Errorf("%s: customCommands: key %q is bound twice in %s", path, cc.Key, ctx)
		}
		seen[ctx+" "+cc.Key] = true
	}
	return nil
}

// Example is a commented sample configuration (seven --print-config).
const Example = `# seven configuration. Every setting is optional.

# Editor for commit messages and properties (default: $SVN_EDITOR, $VISUAL, $EDITOR).
# editor: "code --wait"

# Status auto-refresh in seconds; negative disables it.
refreshSeconds: 10

# Start the Files panel as a flat list instead of a tree.
flatFileList: false

# Mouse: click to select, wheel to scroll.
mouse: true

# Colours as "#rrggbb" or ANSI numbers; leave empty for the defaults.
theme:
  accent: ""
  green: ""
  red: ""
  yellow: ""
  cyan: ""
  magenta: ""
  muted: ""
  selectedBg: ""

# Remap keys: "your key": "seven key".
remap:
  # "ctrl+s": "c"   # commit with ctrl+s

# Custom commands. command is a Go template with:
#   {{.File}} {{.Files}} {{.Dir}} {{.URL}} {{.RepoRoot}} {{.Branch}}
#   {{.Revision}} {{.BranchURL}}   and helpers {{quote .File}} {{join .Files " "}}
customCommands:
  - key: "X"
    context: files            # global, files, branches or log
    description: "Show svn info for the selected file"
    command: "svn info {{quote .File}}"
    showOutput: true
  # - key: "V"
  #   context: log
  #   description: "Open the revision in the web UI"
  #   command: "xdg-open https://svn.example.com/viewvc?view=revision&revision={{.Revision}}"
`
