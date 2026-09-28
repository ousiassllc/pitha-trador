package updater_test

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/updater"
)

func TestBuildSilentInstallCommand_ArgsIncludeSilentFlag(t *testing.T) {
	cmd := updater.BuildSilentInstallCommand(`C:\Temp\pitha-trador-installer.exe`)

	if cmd.Path != `C:\Temp\pitha-trador-installer.exe` {
		t.Fatalf("Path = %q, want the installer path", cmd.Path)
	}
	want := []string{`C:\Temp\pitha-trador-installer.exe`, "/S"}
	if len(cmd.Args) != len(want) {
		t.Fatalf("Args = %v, want %v", cmd.Args, want)
	}
	for i, arg := range want {
		if cmd.Args[i] != arg {
			t.Fatalf("Args[%d] = %q, want %q", i, cmd.Args[i], arg)
		}
	}
}
