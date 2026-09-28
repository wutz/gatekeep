// Package executor runs argv on a target without any shell interpretation on
// the gatekeep side.
package executor

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"

	"github.com/wutz/gatekeep/internal/config"
)

// Result of a command run.
type Result struct {
	ExitCode  int
	Stdout    string
	Stderr    string
	Truncated bool
	Duration  time.Duration
}

// Executor runs commands.
type Executor struct {
	Timeout   time.Duration
	MaxOutput int
}

type capBuffer struct {
	buf       bytes.Buffer
	max       int
	truncated bool
}

func (c *capBuffer) Write(p []byte) (int, error) {
	if room := c.max - c.buf.Len(); room < len(p) {
		c.truncated = true
		if room > 0 {
			c.buf.Write(p[:room])
		}
		return len(p), nil
	}
	return c.buf.Write(p)
}

// shellQuote quotes an argument for the remote shell that sshd spawns.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// Command builds the exec.Cmd for argv on target.
func Command(ctx context.Context, t config.Target, argv []string) *exec.Cmd {
	if t.Transport == "ssh" {
		dest := t.Host
		if t.User != "" {
			dest = t.User + "@" + dest
		}
		quoted := make([]string, len(argv))
		for i, a := range argv {
			quoted[i] = shellQuote(a)
		}
		// BatchMode: never prompt; the remote side receives a fully quoted
		// command so no argument can be reinterpreted by the remote shell.
		return exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10",
			dest, "--", strings.Join(quoted, " "))
	}
	return exec.CommandContext(ctx, argv[0], argv[1:]...)
}

// Run executes argv on target.
func (e *Executor) Run(ctx context.Context, t config.Target, argv []string) Result {
	if e.Timeout == 0 {
		e.Timeout = 60 * time.Second
	}
	if e.MaxOutput == 0 {
		e.MaxOutput = 1 << 20
	}
	ctx, cancel := context.WithTimeout(ctx, e.Timeout)
	defer cancel()
	cmd := Command(ctx, t, argv)
	out := &capBuffer{max: e.MaxOutput}
	errb := &capBuffer{max: e.MaxOutput / 4}
	cmd.Stdout, cmd.Stderr = out, errb
	start := time.Now()
	err := cmd.Run()
	res := Result{Duration: time.Since(start), Truncated: out.truncated || errb.truncated}
	res.Stdout, res.Stderr = out.buf.String(), errb.buf.String()
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		res.ExitCode = ee.ExitCode()
	default:
		res.ExitCode = -1
		res.Stderr += "\ngatekeep: " + err.Error()
	}
	if ctx.Err() == context.DeadlineExceeded {
		res.ExitCode = -1
		res.Stderr += "\ngatekeep: timed out after " + e.Timeout.String()
	}
	return res
}
