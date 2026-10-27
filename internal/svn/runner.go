// Package svn wraps the svn command-line client. It is the only package
// that executes svn; everything else consumes the typed results.
package svn

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ErrNotInstalled is returned when no svn binary can be found on PATH.
var ErrNotInstalled = errors.New("svn executable not found on PATH")

// ErrAuth is returned when svn fails because credentials are required.
var ErrAuth = errors.New("svn authentication required")

// Credentials are passed to svn on the command line and never logged.
type Credentials struct {
	Username string
	Password string
}

// LogEntry records one executed command for the command log panel.
type LogEntry struct {
	Args     []string
	Duration time.Duration
	Err      error
}

// Client runs svn commands against a single working copy.
type Client struct {
	Dir     string
	Bin     string
	Timeout time.Duration

	mu    sync.Mutex
	creds *Credentials
	log   []LogEntry
}

// NewClient locates the svn binary and returns a client rooted at dir.
func NewClient(dir string) (*Client, error) {
	bin, err := exec.LookPath("svn")
	if err != nil {
		return nil, ErrNotInstalled
	}
	return &Client{Dir: dir, Bin: bin, Timeout: 5 * time.Minute}, nil
}

// SetCredentials stores credentials used for subsequent commands.
func (c *Client) SetCredentials(creds *Credentials) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.creds = creds
}

// CommandLog returns a copy of the executed command history.
func (c *Client) CommandLog() []LogEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]LogEntry(nil), c.log...)
}

// Error carries svn's stderr alongside the exit error.
type Error struct {
	Args   []string
	Stderr string
	Err    error
}

func (e *Error) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if msg == "" {
		msg = e.Err.Error()
	}
	return fmt.Sprintf("svn %s: %s", strings.Join(e.Args, " "), msg)
}

func (e *Error) Unwrap() error { return e.Err }

// IsOutOfDate reports whether err is a commit rejected because the working
// copy is behind the repository (fixed by updating first).
func IsOutOfDate(err error) bool {
	var e *Error
	return errors.As(err, &e) && (strings.Contains(e.Stderr, "E155011") || strings.Contains(e.Stderr, "E160028"))
}

// Run executes svn with args in the working copy and returns stdout.
func (c *Client) Run(ctx context.Context, args ...string) ([]byte, error) {
	return c.run(ctx, nil, args...)
}

// RunWithInput is like Run but feeds stdin (used for commit messages).
func (c *Client) RunWithInput(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	return c.run(ctx, stdin, args...)
}

func (c *Client) run(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()

	full := append([]string{"--non-interactive"}, args...)
	c.mu.Lock()
	if c.creds != nil {
		full = append(full, "--username", c.creds.Username, "--password", c.creds.Password)
	}
	c.mu.Unlock()

	cmd := exec.CommandContext(ctx, c.Bin, full...)
	cmd.Dir = c.Dir
	cmd.Env = commandEnv(os.Environ(), runtime.GOOS)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	if err != nil {
		if isAuthError(stderr.String()) {
			err = fmt.Errorf("%w: %s", ErrAuth, strings.TrimSpace(stderr.String()))
		} else {
			err = &Error{Args: args, Stderr: stderr.String(), Err: err}
		}
	}

	c.mu.Lock()
	c.log = append(c.log, LogEntry{Args: args, Duration: time.Since(start), Err: err})
	c.mu.Unlock()

	return stdout.Bytes(), err
}

// commandEnv forces untranslated messages (we match English output such as
// "Committed revision N.") and a UTF-8 character encoding, without which svn
// cannot handle non-ASCII paths. LC_ALL would override both, so it is dropped.
//
// On Linux the user's locale may be missing (e.g. LANG=en_US.UTF-8 without
// generated locales), so LC_CTYPE is pinned to C.UTF-8, which glibc >= 2.35
// and musl always provide. macOS and Windows svn builds use UTF-8 natively.
func commandEnv(environ []string, goos string) []string {
	env := make([]string, 0, len(environ)+2)
	for _, kv := range environ {
		if strings.HasPrefix(kv, "LC_ALL=") || strings.HasPrefix(kv, "LC_MESSAGES=") ||
			(goos == "linux" && strings.HasPrefix(kv, "LC_CTYPE=")) {
			continue
		}
		env = append(env, kv)
	}
	if goos == "linux" {
		env = append(env, "LC_CTYPE=C.UTF-8")
	}
	return append(env, "LC_MESSAGES=C")
}

// isAuthError reports whether stderr contains an authorization failure
// (E170001) or exhausted credential providers (E215004).
func isAuthError(stderr string) bool {
	return strings.Contains(stderr, "E170001") || strings.Contains(stderr, "E215004")
}
