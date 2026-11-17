package codexwire

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
)

const (
	// loginTTL is how long a login check is remembered.
	loginTTL = time.Minute
	// loginTimeout bounds `codex login status`.
	loginTimeout = 10 * time.Second
	// maxLoginOutput bounds what is read of its output.
	maxLoginOutput = 4 << 10
)

// Login checks, with `codex login status` against the dedicated CODEX_HOME,
// whether the CLI is logged in with a ChatGPT account. It never opens the
// credential file: only the exit code and a short look at the status text decide,
// and the text is neither stored nor logged. It is safe for concurrent use.
type Login struct {
	cfg Config
	now func() time.Time

	mu    sync.Mutex
	state string
	at    time.Time
}

// NewLogin returns the login check of cfg's CODEX_HOME.
func NewLogin(cfg Config) *Login { return &Login{cfg: cfg, now: time.Now} }

// State is the login state (a jevtrialsettings.CodexLogin* constant) and when it
// was checked; a check younger than a minute is reused.
func (l *Login) State(ctx context.Context) (string, time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now := l.now(); l.state != "" && now.Sub(l.at) < loginTTL {
		return l.state, l.at
	}
	l.state = l.check(ctx)
	l.at = l.now()
	return l.state, l.at
}

// check runs the status command. Exit 0 with a status text that names ChatGPT
// is the subscription login; exit 0 otherwise (an API key, or a wording this
// code does not know) is not used, because an API key is billed per use; a
// non-zero exit is logged out; a command that could not run or time out is
// unavailable.
func (l *Login) check(ctx context.Context) string {
	if l.cfg.Home == "" {
		return jevtrialsettings.CodexLoginUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, l.cfg.bin(), "login", "status") // #nosec G204 -- the configured Codex CLI, fixed arguments
	cmd.Env = childEnv(l.cfg.Home)
	var out bytes.Buffer
	cmd.Stdout = &limitedWriter{w: &out, left: maxLoginOutput}
	cmd.Stderr = cmd.Stdout
	ownProcessGroup(cmd)
	cmd.Cancel = func() error { return killGroup(cmd) }
	cmd.WaitDelay = waitDelay
	err := cmd.Run()
	switch {
	case ctx.Err() != nil:
		return jevtrialsettings.CodexLoginUnavailable
	case err == nil:
		text := strings.ToLower(out.String())
		if strings.Contains(text, "chatgpt") && !strings.Contains(text, "api key") {
			return jevtrialsettings.CodexLoginChatGPT
		}
		return jevtrialsettings.CodexLoginOtherAuth
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return jevtrialsettings.CodexLoginLoggedOut
	}
	return jevtrialsettings.CodexLoginUnavailable
}
