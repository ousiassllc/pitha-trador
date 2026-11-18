//go:build !windows

package codexwire_test

import (
	"context"
	"os"
	"strconv"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/codexwire"
)

func TestLogin_StatesFromTheStatusCommand(t *testing.T) {
	for name, tc := range map[string]struct {
		stdout string
		exit   int
		want   string
	}{
		"chatgpt":       {"Logged in using ChatGPT", 0, jevtrialsettings.CodexLoginChatGPT},
		"an api key":    {"Logged in using an API key", 0, jevtrialsettings.CodexLoginOtherAuth},
		"unknown words": {"ok", 0, jevtrialsettings.CodexLoginOtherAuth},
		"logged out":    {"Not logged in", 1, jevtrialsettings.CodexLoginLoggedOut},
	} {
		t.Run(name, func(t *testing.T) {
			m := newMockCLI(t)
			m.put("login_stdout", tc.stdout)
			m.put("login_exit", strconv.Itoa(tc.exit))
			got, at := codexwire.NewLogin(codexwire.Config{Bin: m.Bin, Home: m.Home}).State(context.Background())
			if got != tc.want || at.IsZero() {
				t.Errorf("State = %q at %v, want %q", got, at, tc.want)
			}
		})
	}
	if got, _ := codexwire.NewLogin(codexwire.Config{Bin: "/nonexistent/codex", Home: t.TempDir()}).State(context.Background()); got != jevtrialsettings.CodexLoginUnavailable {
		t.Errorf("a missing CLI = %q, want unavailable", got)
	}
	if got, _ := codexwire.NewLogin(codexwire.Config{}).State(context.Background()); got != jevtrialsettings.CodexLoginUnavailable {
		t.Errorf("no CODEX_HOME = %q, want unavailable", got)
	}
}

func TestLogin_NeverOpensTheCredentialFile(t *testing.T) {
	m := newMockCLI(t)
	auth := m.Home + "/auth.json"
	if err := os.WriteFile(auth, []byte(`{"tokens":{"access_token":"secret-token-value"}}`), 0o000); err != nil {
		t.Fatal(err)
	}
	m.okRun(validAnswers(t, version))
	// With mode 000 any attempt to read it would fail loudly; the client must neither need it nor echo it.
	if _, _, _, err := newClient(m, 0).Trader(context.Background(), request()); err != nil {
		t.Fatalf("Trader: %v", err)
	}
	if st, _ := newClient(m, 0).Login(context.Background()); st != jevtrialsettings.CodexLoginChatGPT {
		t.Errorf("login = %q", st)
	}
}
