package codexwire

import (
	"os"
	"time"
)

// DefaultBin is the executable looked up on PATH when Config.Bin is empty.
const DefaultBin = "codex"

// DefaultCallTimeout bounds one `codex exec` run when Config.CallTimeout is 0. The
// run time of a call has not been measured (nothing was run against a real login).
const DefaultCallTimeout = 150 * time.Second

// Config configures a Client and a Login.
type Config struct {
	// Bin is the Codex CLI executable (name on PATH or a path). Empty: DefaultBin.
	Bin string
	// Home is the dedicated CODEX_HOME of the trial: a directory a person logged in
	// to beforehand (`CODEX_HOME=<Home> codex login`). It is required: without it
	// the client is not Configured, so the person's own Codex home is never used.
	Home string
	// Model is the model slug passed with -m (e.g. "gpt-6.1-sol").
	Model string
	// CallTimeout bounds one run (DefaultCallTimeout when 0).
	CallTimeout time.Duration
}

func (c Config) bin() string {
	if c.Bin == "" {
		return DefaultBin
	}
	return c.Bin
}

func (c Config) callTimeout() time.Duration {
	if c.CallTimeout <= 0 {
		return DefaultCallTimeout
	}
	return c.CallTimeout
}

// allowedEnv are the only variables of the parent environment a child gets:
// where to find programs and a home, the locale and the temp directory. Nothing
// else is passed on: in particular no API key or token variable.
var allowedEnv = []string{
	"PATH", "HOME", "LANG", "LC_ALL", "LC_CTYPE", "TZ", "TMPDIR",
	// Windows: the CLI cannot start without these.
	"SYSTEMROOT", "SYSTEMDRIVE", "USERPROFILE", "TEMP", "TMP", "APPDATA", "LOCALAPPDATA",
}

// childEnv is the environment of a child: the allow-listed variables of the
// parent plus CODEX_HOME = home (always the dedicated one, even when the parent
// has a CODEX_HOME of its own).
func childEnv(home string) []string {
	env := make([]string, 0, len(allowedEnv)+1)
	for _, name := range allowedEnv {
		if v, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+v)
		}
	}
	return append(env, "CODEX_HOME="+home)
}
