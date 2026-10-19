package mcpwire_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/mcpwire"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/admin"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/proc"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
)

type child struct {
	done       chan struct{}
	once       sync.Once
	terminated chan struct{}
}

func (c *child) Done() <-chan struct{} { return c.done }
func (c *child) Exit() proc.Exit       { return proc.Exit{Code: -1} }
func (c *child) Terminate(time.Duration) {
	c.once.Do(func() { close(c.terminated); close(c.done) })
}

// service is an admin.Service over a temp settings file with a fake
// spawner: spawned receives each child the application starts.
func service(t *testing.T, s *settings.Settings) (*admin.Service, <-chan *child) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, settings.FileName)
	exe := filepath.Join(dir, "paper-results-mcp")
	if err := os.WriteFile(exe, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if s != nil {
		if err := settings.Save(path, *s); err != nil {
			t.Fatal(err)
		}
	}
	spawned := make(chan *child, 4)
	svc := admin.New(admin.Options{
		Path:    path,
		ExePath: func() (string, error) { return exe, nil },
		Dial:    func(context.Context, string) error { return errors.New("closed") },
		Proc: proc.Options{
			ReadyWait: 10 * time.Millisecond, PollInterval: time.Millisecond, HandoverWait: 10 * time.Millisecond,
			Spawn: func(proc.Spec) (proc.Child, error) {
				c := &child{done: make(chan struct{}), terminated: make(chan struct{})}
				spawned <- c
				return c, nil
			},
		},
	})
	return svc, spawned
}

// The application's start hook starts the MCP for enabled settings with a key
// and a permitted address; its exit hook ends it.
func TestStartAndStop_MCPFollowsTheApplication(t *testing.T) {
	svc, spawned := service(t, &settings.Settings{Enabled: true, Listen: settings.DefaultListen, APIKey: "pitha-mcp-k"})
	mcpwire.Start(context.Background(), svc)
	var c *child
	select {
	case c = <-spawned:
	case <-time.After(5 * time.Second):
		t.Fatal("the MCP was not started with the application")
	}
	mcpwire.Stop(svc)
	select {
	case <-c.terminated:
	default:
		t.Fatal("the MCP process was not terminated when the application exited")
	}
	if err := svc.Start(context.Background()); !errors.Is(err, proc.ErrClosed) {
		t.Fatalf("Start after the application exited = %v, want ErrClosed", err)
	}
}

func TestStart_DoesNothingWhenOffKeylessOrTheAddressIsNotPermitted(t *testing.T) {
	for name, s := range map[string]*settings.Settings{
		"no settings file (the default install)": nil,
		"disabled":                               {Listen: settings.DefaultListen, APIKey: "pitha-mcp-k"},
		"enabled, empty key":                     {Enabled: true, Listen: settings.DefaultListen},
		"enabled, key, 0.0.0.0":                  {Enabled: true, Listen: "0.0.0.0:48110", APIKey: "pitha-mcp-k"},
	} {
		t.Run(name, func(t *testing.T) {
			svc, spawned := service(t, s)
			mcpwire.Start(context.Background(), svc)
			mcpwire.Stop(svc) // waits for the start hook to finish
			select {
			case <-spawned:
				t.Fatal("an MCP process was started")
			default:
			}
		})
	}
}

func TestStartAndStop_NilServiceIsANoOp(t *testing.T) {
	mcpwire.Start(context.Background(), nil)
	mcpwire.Stop(nil)
}
