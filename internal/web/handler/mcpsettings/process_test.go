package mcpsettings

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/proc"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
)

// A toast is one short line (design guide O3): the reason and the remedy are
// worded in the panel, so every processFailure message stays within one
// sentence of about 20 characters.
func TestProcessFailureIsAOneLineToast(t *testing.T) {
	const maxRunes = 22
	errs := []error{
		proc.ErrAlreadyRunning, proc.ErrAlreadyListening, proc.ErrNotManaged,
		proc.ErrNotRunning, proc.ErrExeMissing, proc.ErrClosed, proc.ErrDisabled,
		proc.ErrNoKey, proc.ErrSettingsUnusable, proc.ErrAccessIncomplete,
		settings.ErrListenNotAllowed, proc.ErrExitedEarly, errors.New("unexpected"),
	}
	for _, err := range errs {
		_, msg := processFailure(err)
		if n := utf8.RuneCountInString(msg); n > maxRunes {
			t.Errorf("%v: %q is %d characters, want at most %d", err, msg, n, maxRunes)
		}
		if strings.Count(msg, "。") != 1 || !strings.HasSuffix(msg, "。") {
			t.Errorf("%v: %q is not a single sentence", err, msg)
		}
	}
}
