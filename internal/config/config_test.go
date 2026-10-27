package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadMissingAndEmpty(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.yml"))
	if err != nil || !cfg.MouseEnabled() || cfg.RefreshSeconds != 0 {
		t.Fatalf("missing: %+v %v", cfg, err)
	}
	if _, err := Load(writeConfig(t, "")); err != nil {
		t.Fatalf("empty file: %v", err)
	}
}

func TestLoadExample(t *testing.T) {
	cfg, err := Load(writeConfig(t, Example))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.CustomCommands) != 1 || cfg.CustomCommands[0].Context != "files" || !cfg.CustomCommands[0].ShowOutput {
		t.Fatalf("example custom commands = %+v", cfg.CustomCommands)
	}
}

func TestLoadErrors(t *testing.T) {
	cases := map[string]string{
		"refreshSecnds: 5\n":                                                    "field refreshSecnds not found",
		"customCommands:\n  - key: x\n":                                         "needs both key and command",
		"customCommands:\n  - {key: x, command: ls, context: diff}\n":           "unknown context",
		"customCommands:\n  - {key: x, command: a}\n  - {key: x, command: b}\n": "bound twice",
	}
	for body, want := range cases {
		_, err := Load(writeConfig(t, body))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err = %v, want %q", body, err, want)
		}
	}
}

func TestMouseOff(t *testing.T) {
	cfg, err := Load(writeConfig(t, "mouse: false\nremap:\n  ctrl+s: c\n"))
	if err != nil || cfg.MouseEnabled() || cfg.Remap["ctrl+s"] != "c" {
		t.Fatalf("%+v %v", cfg, err)
	}
}
