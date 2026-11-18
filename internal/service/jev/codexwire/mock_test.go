//go:build !windows

package codexwire_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/systemone"
)

// mockCLI is a stand-in for the Codex CLI: a shell script that prints the
// contents of $CODEX_HOME/stdout and exits with $CODEX_HOME/exit, or, for
// `login status`, prints login_stdout and exits with login_exit. It records the
// arguments, the environment and the stdin of the run in the home directory. No
// real CLI, login or network is involved.
type mockCLI struct {
	t    *testing.T
	Bin  string
	Home string
}

const mockScript = `#!/bin/sh
home="$CODEX_HOME"
[ -n "$home" ] || exit 99
if [ "$1" = login ]; then
  cat "$home/login_stdout"
  exit "$(cat "$home/login_exit")"
fi
env > "$home/seen_env"
printf '%s\n' "$@" > "$home/seen_args"
pwd > "$home/seen_cwd"
cat > "$home/seen_stdin"
if [ -f "$home/hang" ]; then
  sleep 300 &
  echo $! > "$home/child_pid"
  wait
fi
cat "$home/stdout"
exit "$(cat "$home/exit")"
`

func newMockCLI(t *testing.T) *mockCLI {
	t.Helper()
	dir := t.TempDir()
	m := &mockCLI{t: t, Bin: filepath.Join(dir, "codex"), Home: filepath.Join(dir, "home")}
	if err := os.WriteFile(m.Bin, []byte(mockScript), 0o755); err != nil { // #nosec G306 -- a test script that must be executable
		t.Fatal(err)
	}
	if err := os.Mkdir(m.Home, 0o700); err != nil {
		t.Fatal(err)
	}
	m.put("exit", "0")
	m.put("login_exit", "0")
	m.put("login_stdout", "Logged in using ChatGPT")
	return m
}

func (m *mockCLI) put(name, content string) {
	m.t.Helper()
	if err := os.WriteFile(filepath.Join(m.Home, name), []byte(content), 0o600); err != nil {
		m.t.Fatal(err)
	}
}

func (m *mockCLI) read(name string) string {
	m.t.Helper()
	b, err := os.ReadFile(filepath.Join(m.Home, name))
	if err != nil {
		m.t.Fatalf("the mock CLI did not record %s: %v", name, err)
	}
	return string(b)
}

func (m *mockCLI) exists(name string) bool {
	_, err := os.Stat(filepath.Join(m.Home, name))
	return err == nil
}

// stdout sets the JSONL the mock prints.
func (m *mockCLI) stdout(lines ...string) {
	out := ""
	for _, l := range lines {
		out += l + "\n"
	}
	m.put("stdout", out)
}

func (m *mockCLI) exit(code int) { m.put("exit", strconv.Itoa(code)) }

// validAnswers is a valid answer object for the questions of version: the first
// option of every choice (confidence 0.7) and 0.5 for every noul question.
func validAnswers(t *testing.T, version string) map[string]any {
	t.Helper()
	questions, err := jev.TraderQuestionsFor(version)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]any{}
	for id, q := range questions {
		switch c := q.Criteria.(type) {
		case systemone.Options:
			out[id] = map[string]any{"choice": c[0].Name, "confidence": 0.7}
		default:
			out[id] = 0.5
		}
	}
	return out
}

func event(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// okRun makes the mock answer with answers (a successful run).
func (m *mockCLI) okRun(answers map[string]any) {
	m.t.Helper()
	msg, err := json.Marshal(answers)
	if err != nil {
		m.t.Fatal(err)
	}
	m.stdout(
		`{"type":"thread.started","thread_id":"th-1"}`,
		`{"type":"turn.started"}`,
		event(m.t, map[string]any{"type": "item.completed", "item": map[string]any{"id": "item_0", "type": "agent_message", "text": string(msg)}}),
		`{"type":"turn.completed","usage":{"input_tokens":1200,"cached_input_tokens":100,"output_tokens":80,"reasoning_output_tokens":20}}`,
	)
	m.exit(0)
}

func (m *mockCLI) failedRun(message string, code int) {
	m.stdout(
		`{"type":"thread.started","thread_id":"th-2"}`,
		`{"type":"turn.started"}`,
		event(m.t, map[string]any{"type": "turn.failed", "error": map[string]any{"message": message}}),
	)
	m.exit(code)
}
