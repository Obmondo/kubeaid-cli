// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt_test

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/stretchr/testify/require"
)

var (
	binaryPath string
	buildOnce  sync.Once
	buildErr   error
)

// buildTestBinary compiles the test harness binary once per test run.
func buildTestBinary(t *testing.T) string {
	t.Helper()

	buildOnce.Do(func() {
		binaryPath = filepath.Join(os.TempDir(), "kubeaid-cli-testcmd")
		//nolint:gosec // test-only build with a fixed binary path.
		cmd := exec.Command("go", "build", "-o", binaryPath, "./e2e/prompt/promptrunner")
		cmd.Dir = repoRoot(t)
		out, err := cmd.CombinedOutput()
		if err != nil {
			buildErr = fmt.Errorf("build failed: %s\n%s", err, out)
		}
	})

	require.NoError(t, buildErr, "test binary build must succeed")
	return binaryPath
}

// repoRoot returns the repository root directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (go.mod)")
		}
		dir = parent
	}
}

// setupDummySSHKey copies the pre-generated dummy PEM key to a temp directory
// and returns its absolute path.
func setupDummySSHKey(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	src := filepath.Join(root, "e2e", "prompt", "testdata", "dummy_key.pem")
	data, err := os.ReadFile(src)
	require.NoError(t, err, "dummy key must exist at %s", src)

	dst := filepath.Join(t.TempDir(), "dummy_key.pem")
	require.NoError(t, os.WriteFile(dst, data, 0o600)) //nolint:gosec // dst is under t.TempDir().
	return dst
}

// console wraps a PTY for driving interactive prompts in tests.
type console struct {
	t    *testing.T
	ptmx *os.File
	buf  bytes.Buffer
	mu   sync.Mutex
	done chan struct{}
}

// newConsole starts the binary under a PTY and returns the console for driving prompts.
func newConsole(t *testing.T, binary, outputDir string) (*console, *exec.Cmd) {
	t.Helper()

	cmd := exec.Command(binary, outputDir) //nolint:gosec // test-only fixed binary path.
	cmd.Env = filterEnv(os.Environ(), "SSH_AUTH_SOCK")

	ptmx, err := pty.Start(cmd)
	require.NoError(t, err)

	c := &console{
		t:    t,
		ptmx: ptmx,
		done: make(chan struct{}),
	}

	// Read all output from the PTY into the buffer in the background.
	// Also respond to Device Status Report (DSR) queries (\x1b[6n)
	// that survey uses to detect cursor position.
	go func() {
		defer close(c.done)
		tmp := make([]byte, 4096)
		for {
			n, err := ptmx.Read(tmp)
			if n > 0 {
				chunk := tmp[:n]
				c.mu.Lock()
				c.buf.Write(chunk)
				c.mu.Unlock()

				// Respond to cursor position queries (DSR).
				// survey sends \x1b[6n and expects \x1b[<row>;<col>R.
				if bytes.Contains(chunk, []byte("\x1b[6n")) {
					_, _ = ptmx.Write([]byte("\x1b[1;1R"))
				}
			}
			if err != nil {
				return
			}
		}
	}()

	t.Cleanup(func() { _ = ptmx.Close() })
	return c, cmd
}

// expectString waits until the accumulated output contains the given substring.
func (c *console) expectString(s string) {
	c.t.Helper()
	deadline := time.After(30 * time.Second)
	for {
		c.mu.Lock()
		found := strings.Contains(c.buf.String(), s)
		c.mu.Unlock()
		if found {
			return
		}
		select {
		case <-deadline:
			c.mu.Lock()
			output := c.buf.String()
			c.mu.Unlock()
			c.t.Fatalf("timed out waiting for %q\n\naccumulated output:\n%s", s, output)
		case <-time.After(50 * time.Millisecond):
			// poll again
		}
	}
}

// expectAnyString waits until output contains any provided substring and
// returns the first matched value.
func (c *console) expectAnyString(values ...string) string {
	c.t.Helper()
	deadline := time.After(30 * time.Second)
	for {
		c.mu.Lock()
		out := c.buf.String()
		c.mu.Unlock()
		for _, v := range values {
			if strings.Contains(out, v) {
				return v
			}
		}

		select {
		case <-deadline:
			c.t.Fatalf("timed out waiting for one of %q\n\naccumulated output:\n%s", values, out)
		case <-time.After(50 * time.Millisecond):
			// poll again
		}
	}
}

// send writes raw bytes to the PTY.
func (c *console) send(s string) {
	c.t.Helper()
	_, err := io.WriteString(c.ptmx, s)
	require.NoError(c.t, err)
}

// sendLine writes text followed by a newline to the PTY.
func (c *console) sendLine(s string) {
	c.t.Helper()
	c.send(s + "\r")
}

// selectOption sends N down-arrow keystrokes then enter.
func (c *console) selectOption(index int) {
	c.t.Helper()
	for range index {
		c.send("\x1b[B")
		time.Sleep(50 * time.Millisecond) // let survey process the keystroke
	}
	time.Sleep(100 * time.Millisecond)
	c.sendLine("")
}

// acceptDefault sends enter to accept the default value.
func (c *console) acceptDefault() {
	c.t.Helper()
	c.sendLine("")
}

// filterEnv removes the named variable from the environment slice.
func filterEnv(env []string, name string) []string {
	prefix := name + "="
	filtered := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

// readGeneratedFile reads a file from the output directory.
func readGeneratedFile(t *testing.T, outputDir, filename string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(outputDir, filename))
	require.NoError(t, err, "generated %s must exist", filename)
	return string(data)
}
