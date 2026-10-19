package proc_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/proc"
)

// Safety condition 3: the API key reaches the process only through its 0600
// settings file - never an argument, an environment variable, a log line or
// a status value.
func TestTheKeyNeverLeavesTheSettingsFile(t *testing.T) {
	r := newRig(t)
	r.seedValid()
	r.env = []string{"PATH=/bin"}
	ctx := context.Background()
	if err := r.m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	r.child(0).end(proc.Exit{Code: 1, Stderr: "boom"})
	eventually(t, "exit", func() bool { return r.m.Status().Failure != nil })
	_ = r.m.Restart(ctx)
	_ = r.m.Stop(ctx)
	r.m.AutoStart(ctx)
	r.m.Shutdown()

	for i, spec := range r.specs {
		all := append(append([]string{spec.Exe}, spec.Args...), spec.Env...)
		for _, s := range all {
			if strings.Contains(s, secretKey) || strings.Contains(s, "pitha-mcp-") {
				t.Errorf("spawn %d carries the key: %q", i, s)
			}
		}
		if len(spec.Args) != 2 || spec.Args[0] != "-config" || spec.Args[1] != r.path {
			t.Errorf("spawn %d args = %q, want only -config <settings path>", i, spec.Args)
		}
	}
	if r.spawns() == 0 {
		t.Fatal("setup: nothing was spawned")
	}
	if strings.Contains(r.logs.String(), secretKey) || strings.Contains(r.logs.String(), "pitha-mcp-") {
		t.Errorf("the log contains the key:\n%s", r.logs)
	}
	if strings.Contains(strings.Join([]string{r.m.Status().Listen}, ""), secretKey) {
		t.Error("status contains the key")
	}
}

// A child's own output is used to tell why it ended, never shown or logged:
// even a process that (wrongly) printed the key cannot leak it through us.
func TestChildOutputIsNeverLogged(t *testing.T) {
	r := newRig(t)
	r.seedValid()
	if err := r.m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.child(0).end(proc.Exit{Code: 1, Stderr: "panic: " + secretKey})
	eventually(t, "exit", func() bool { return r.m.Status().Failure != nil })
	if strings.Contains(r.logs.String(), secretKey) || strings.Contains(r.logs.String(), "panic") {
		t.Errorf("child output reached the log:\n%s", r.logs)
	}
}

// The child gets an allow-listed environment: the application's other
// variables (tokens, proxies, anything) are not forwarded.
func TestChildEnvIsAnAllowList(t *testing.T) {
	environ := []string{
		"PATH=/usr/bin", "path=/lower", "HOME=/home/x", "APPDATA=C:\\a", "SystemRoot=C:\\Windows",
		"PITHA_DB_PATH=/db", "PITHA_RISK_PATH=/risk",
		"JEV_API_KEY=jev-secret", "KABU_API_PASSWORD=pw", "SLACK_WEBHOOK_URL=https://hooks", "HTTPS_PROXY=http://p",
		"PITHA_MCP_KEY=" + secretKey, "=C:=C:\\", "NOEQUALS",
	}
	got := "\n" + strings.Join(proc.ChildEnv(environ), "\n")
	for _, want := range []string{"PATH=/usr/bin", "path=/lower", "HOME=/home/x", "APPDATA=C:\\a", "SystemRoot=C:\\Windows", "PITHA_DB_PATH=/db", "PITHA_RISK_PATH=/risk"} {
		if !strings.Contains(got, want) {
			t.Errorf("ChildEnv dropped %q", want)
		}
	}
	for _, bad := range []string{"JEV_API_KEY", "KABU_API_PASSWORD", "SLACK_WEBHOOK_URL", "HTTPS_PROXY", "PITHA_MCP_KEY", secretKey, "\n=C:", "NOEQUALS"} {
		if strings.Contains(got, bad) {
			t.Errorf("ChildEnv forwarded %q", bad)
		}
	}
}
