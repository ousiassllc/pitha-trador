package proc

import "strings"

// passedEnv are the only environment variables the MCP process receives.
// The application's own environment is never forwarded wholesale: the child
// gets what it needs to find its settings file, the database and risk.yaml
// (paper capital) and what Windows needs to start any process. The API key
// is not among them, and never will be: the process reads it from the 0600
// settings file (issue #795, safety condition 3).
var passedEnv = []string{
	// Locating the per-user config directory (settings.DefaultPath).
	"APPDATA", "LOCALAPPDATA", "USERPROFILE", "HOME", "XDG_CONFIG_HOME",
	// Windows cannot start a process (sockets, DLL search) without these.
	"SYSTEMROOT", "WINDIR", "PATH", "TMP", "TEMP", "TMPDIR", "TZ",
	// bootstrap.EnvDBPath and bootstrap.EnvRiskPath (cmd/paper-results-mcp
	// resolves the same files as the application).
	"PITHA_DB_PATH", "PITHA_RISK_PATH",
}

// PassedEnvNames lists the variable names ChildEnv lets through.
func PassedEnvNames() []string { return append([]string(nil), passedEnv...) }

// ChildEnv filters environ ("NAME=value" entries, as os.Environ returns) down
// to passedEnv. Names are compared case-insensitively, as Windows does.
func ChildEnv(environ []string) []string {
	var out []string
	for _, kv := range environ {
		// A leading "=" is a Windows per-drive entry ("=C:=C:\"): skip it.
		i := strings.IndexByte(kv[min(1, len(kv)):], '=')
		if i < 0 {
			continue
		}
		name := kv[:i+1]
		for _, want := range passedEnv {
			if strings.EqualFold(name, want) {
				out = append(out, kv)
				break
			}
		}
	}
	return out
}
