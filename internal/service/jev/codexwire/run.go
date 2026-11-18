package codexwire

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// waitDelay is how long after the context ends the run waits for the child's
// pipes to close before it gives up on them.
const waitDelay = 5 * time.Second

// execArgs are the arguments of one run. The prompt goes in on stdin ("-"), so it
// is in no process listing. The flags keep the run small and read-only: no
// persisted session, none of the person's own config or rules, a read-only
// sandbox, no web search and no project documents. The combination has not been
// run against a real login.
func execArgs(model, schemaPath string) []string {
	return []string{
		"exec", "--json", "--output-schema", schemaPath,
		"--ephemeral", "--ignore-user-config", "--ignore-rules", "--skip-git-repo-check",
		"--sandbox", "read-only",
		"-c", `web_search="disabled"`, "-c", "project_doc_max_bytes=0",
		"-m", model, "-",
	}
}

// runExec runs `codex exec` for prompt with schema in an empty temporary
// directory and returns the events it printed. The error is a *CallError when
// the call failed; a run that succeeded has a nil error and an outcome with a
// message. ctx (with the call timeout) ends the run and kills the process group.
func (c *Client) runExec(ctx context.Context, prompt string, schema []byte) (outcome, error) {
	dir, err := os.MkdirTemp("", "pitha-codex-*")
	if err != nil {
		return outcome{}, startError(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	schemaPath := filepath.Join(dir, "schema.json")
	if err := os.WriteFile(schemaPath, schema, 0o600); err != nil {
		return outcome{}, startError(err)
	}
	ctx, cancel := context.WithTimeout(ctx, c.cfg.callTimeout())
	defer cancel()

	cmd := exec.CommandContext(ctx, c.cfg.bin(), execArgs(c.cfg.Model, schemaPath)...) // #nosec G204 -- the program is the configured Codex CLI and the arguments are fixed (the model slug is validated)
	cmd.Dir = dir
	cmd.Env = childEnv(c.cfg.Home)
	cmd.Stdin = bytes.NewReader([]byte(prompt))
	var stdout bytes.Buffer
	cmd.Stdout = &limitedWriter{w: &stdout, left: maxStdout}
	cmd.Stderr = nil // the CLI's progress text is not read: it may carry account details
	ownProcessGroup(cmd)
	cmd.Cancel = func() error { return killGroup(cmd) }
	cmd.WaitDelay = waitDelay

	if err := cmd.Start(); err != nil {
		return outcome{}, startError(err)
	}
	waitErr := cmd.Wait()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return outcome{}, &CallError{Reason: ReasonCancelled, cause: ctxErr}
	}
	out := parseEvents(&stdout)
	return out, judge(out, waitErr, c.now())
}

// judge decides a run from the exit status and the turn events: success needs a
// zero exit, a turn.completed and no turn.failed. Everything else is a
// *CallError, labelled as a usage limit only when the failure text matched.
func judge(out outcome, waitErr error, now time.Time) error {
	reason := ""
	switch {
	case out.failed:
		reason = ReasonTurn
	case waitErr != nil:
		reason = ReasonExit
	case !out.completed:
		reason = ReasonNoTurn
	default:
		return nil
	}
	text := out.failedText
	if text == "" {
		text = out.errorText
	}
	e := &CallError{Reason: reason, UsageLimit: MatchUsageLimit(text)}
	if e.UsageLimit {
		if until, ok := ParseResetTime(text, now, time.Local); ok {
			e.Until = until
		}
	}
	return e
}

// limitedWriter passes on at most left bytes and drops the rest (still reporting
// them written, so the child is never blocked on a full pipe).
type limitedWriter struct {
	w    io.Writer
	left int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	n := len(p)
	if l.left <= 0 {
		return n, nil
	}
	if len(p) > l.left {
		p = p[:l.left]
	}
	l.left -= len(p)
	if _, err := l.w.Write(p); err != nil {
		return 0, err
	}
	return n, nil
}
