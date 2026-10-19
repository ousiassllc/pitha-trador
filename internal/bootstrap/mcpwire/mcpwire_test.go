package mcpwire_test

import (
	"runtime"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/mcpwire"
)

func TestRouterOptions_PerUserConfigDirDecidesWhetherTheCardIsWired(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if got := mcpwire.RouterOptions(); len(got) != 1 {
		t.Fatalf("with a config directory: %d options, want 1", len(got))
	}
	if runtime.GOOS != "linux" {
		t.Skip("only Linux lets the test make os.UserConfigDir fail through the environment")
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	if got := mcpwire.RouterOptions(); len(got) != 0 {
		t.Fatalf("without a config directory: %d options, want none (card hidden)", len(got))
	}
}
