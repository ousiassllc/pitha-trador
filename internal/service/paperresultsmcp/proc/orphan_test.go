//go:build linux || windows

package proc_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/proc"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
)

// These tests run real processes. The test binary re-executes itself: the
// "application" and the "MCP" are both TestHelperProcess, told apart by an
// environment variable the application hands the MCP through Options.Env.
const (
	helperEnv   = "PROC_TEST_HELPER" // "app" or "mcp"
	configEnv   = "PROC_TEST_CONFIG"
	mcpPIDEnv   = "PROC_TEST_MCP_PIDFILE"
	appReadyEnv = "PROC_TEST_APP_READYFILE"
)

func TestHelperProcess(t *testing.T) {
	switch os.Getenv(helperEnv) {
	case "mcp":
		// Report what the application handed over: stdin and stdout must be
		// the null device (a character device), not a pipe or a file. Written
		// before the pid file, which is the readiness signal.
		var stdio string
		for _, f := range []*os.File{os.Stdin, os.Stdout} {
			info, err := f.Stat()
			stdio += strconv.FormatBool(err == nil && info.Mode()&os.ModeCharDevice != 0) + " "
		}
		if err := os.WriteFile(os.Getenv(mcpPIDEnv)+".stdio", []byte(stdio), 0o600); err != nil {
			os.Exit(3)
		}
		if err := os.WriteFile(os.Getenv(mcpPIDEnv), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			os.Exit(3)
		}
		time.Sleep(time.Minute) // an MCP that listens until it is stopped
		os.Exit(0)
	case "app":
		runHelperApp()
	}
}

// runHelperApp is the application: it starts the MCP through a real Manager,
// reports readiness, then runs until it is killed.
func runHelperApp() {
	exe, _ := os.Executable()
	m := proc.New(proc.Options{
		ConfigPath: os.Getenv(configEnv),
		ExePath:    func() (string, error) { return exe, nil },
		Dial:       func(context.Context, string) error { return errors.New("closed") },
		Args:       func(string) []string { return []string{"-test.run=^TestHelperProcess$"} },
		Env: func() []string {
			return append(proc.ChildEnv(os.Environ()), helperEnv+"=mcp", mcpPIDEnv+"="+os.Getenv(mcpPIDEnv))
		},
		ReadyWait: 100 * time.Millisecond,
	})
	if err := m.Start(context.Background()); err != nil {
		os.Exit(2)
	}
	if err := os.WriteFile(os.Getenv(appReadyEnv), []byte("ready"), 0o600); err != nil {
		os.Exit(4)
	}
	time.Sleep(time.Minute)
	os.Exit(0)
}

type realRig struct {
	cfg, mcpPID, appReady string
}

func newRealRig(t *testing.T) realRig {
	t.Helper()
	dir := t.TempDir()
	r := realRig{cfg: filepath.Join(dir, settings.FileName), mcpPID: filepath.Join(dir, "mcp.pid"), appReady: filepath.Join(dir, "app.ready")}
	if err := settings.Save(r.cfg, settings.Settings{Enabled: true, Listen: settings.DefaultListen, APIKey: secretKey}); err != nil {
		t.Fatal(err)
	}
	return r
}

func (r realRig) startApp(t *testing.T) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestHelperProcess$")
	cmd.Env = append(os.Environ(), helperEnv+"=app", configEnv+"="+r.cfg, mcpPIDEnv+"="+r.mcpPID, appReadyEnv+"="+r.appReady)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	eventually(t, "the application to start the MCP", func() bool { _, err := os.Stat(r.appReady); return err == nil })
	return cmd
}

func (r realRig) pid(t *testing.T) int {
	t.Helper()
	var pid int
	eventually(t, "the MCP to report its pid", func() bool {
		b, err := os.ReadFile(r.mcpPID)
		if err != nil {
			return false
		}
		pid, err = strconv.Atoi(string(b))
		return err == nil
	})
	return pid
}

// The application is killed (no shutdown code runs): the MCP must not be left
// behind as an orphan. Linux: Pdeathsig. Windows: the kill-on-close Job Object.
func TestAKilledApplicationLeavesNoOrphanMCP(t *testing.T) {
	r := newRealRig(t)
	app := r.startApp(t)
	pid := r.pid(t)
	if !pidAlive(pid) {
		t.Fatal("setup: the MCP is not running while the application is")
	}
	if err := app.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = app.Wait()
	eventually(t, "the orphaned MCP to be killed by the operating system", func() bool { return !pidAlive(pid) })
}

// The application exits normally: Shutdown ends the real process.
func TestShutdownEndsTheRealProcess(t *testing.T) {
	r := newRealRig(t)
	exe, _ := os.Executable()
	m := proc.New(proc.Options{
		ConfigPath: r.cfg,
		ExePath:    func() (string, error) { return exe, nil },
		Dial:       func(context.Context, string) error { return errors.New("closed") },
		Args:       func(string) []string { return []string{"-test.run=^TestHelperProcess$"} },
		Env: func() []string {
			return append(proc.ChildEnv(os.Environ()), helperEnv+"=mcp", mcpPIDEnv+"="+r.mcpPID)
		},
		ReadyWait: 100 * time.Millisecond,
	})
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	pid := r.pid(t)
	if !pidAlive(pid) || !m.Status().Running {
		t.Fatal("setup: the process is not running")
	}
	if got, err := os.ReadFile(r.mcpPID + ".stdio"); err != nil || string(got) != "true true " {
		t.Fatalf("stdin/stdout char-device = %q (%v), want the null device for both", got, err)
	}
	m.Shutdown()
	if pidAlive(pid) {
		t.Fatal("the process outlived Shutdown")
	}
	if m.Status().Running {
		t.Fatal("still Running after Shutdown")
	}
}
