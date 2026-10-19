package mcpwire_test

import (
	"runtime"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/mcpwire"
)

func TestNew_PerUserConfigDirDecidesWhetherTheCardIsWired(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	svc := mcpwire.New()
	if svc == nil || len(mcpwire.RouterOptions(svc)) != 1 {
		t.Fatalf("with a config directory: service %v, want a service and 1 option", svc)
	}
	if runtime.GOOS != "linux" {
		t.Skip("only Linux lets the test make os.UserConfigDir fail through the environment")
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	if got := mcpwire.New(); got != nil || len(mcpwire.RouterOptions(got)) != 0 {
		t.Fatalf("without a config directory: service %v, want none (card hidden)", got)
	}
}
