package admin

import (
	"context"
	"errors"
	"os"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/proc"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
)

// State is what the Settings screen shows as the MCP's status.
type State string

const (
	// StateListening: a TCP connect to the configured address succeeded.
	StateListening State = "listening"
	// StateStopped: nothing is listening, or the MCP is configured off
	// (Reason says which).
	StateStopped State = "stopped"
	// StateNoKey: enabled, but without an API key the MCP never listens.
	StateNoKey State = "no_key"
	// StateStarting: the application started the process moments ago and it
	// is not listening yet.
	StateStarting State = "starting"
	// StateError: the process failed to start, exited without being stopped,
	// or is alive but never listened (View.Failure says which).
	StateError State = "error"
)

// Reason says why a State is StateStopped, or why the MCP will not start with
// the application (View.AutoStartBlocked); it is empty otherwise.
type Reason string

const (
	// ReasonDisabled: the switch is off (the default).
	ReasonDisabled Reason = "disabled"
	// ReasonNoKey: enabled, but no API key is stored.
	ReasonNoKey Reason = "no_key"
	// ReasonSettingsUnreadable: the settings file cannot be used (LoadError
	// says how), so the MCP process would not listen either.
	ReasonSettingsUnreadable Reason = "settings_unreadable"
	// ReasonListenNotAllowed: the stored address is outside the loopback /
	// tailnet rule, so the process refuses to bind it.
	ReasonListenNotAllowed Reason = "listen_not_allowed"
	// ReasonAccessIncomplete: Cloudflare Access header verification is on
	// without an id or secret, so the process refuses to listen.
	ReasonAccessIncomplete Reason = "access_incomplete"
	// ReasonStopping: the application is ending the process it started.
	ReasonStopping Reason = "stopping"
	// ReasonExeMissing: paper-results-mcp is not beside the application, so
	// it cannot start (shown for AutoStartBlocked only).
	ReasonExeMissing Reason = "exe_missing"
	// ReasonNotRunning: configured to listen, but nothing answers at the
	// address (not started yet).
	ReasonNotRunning Reason = "not_running"
)

// LoadErrorKind classifies a settings file that could not be loaded. It never
// carries the file's content (that holds the API key).
type LoadErrorKind string

const (
	LoadOK           LoadErrorKind = ""
	LoadInsecure     LoadErrorKind = "insecure_permissions"
	LoadUnusableFile LoadErrorKind = "unusable"
)

// MaskedAuthHeader is the Authorization header form shown on screen: the key
// itself is only ever displayed once, right after it is generated.
const MaskedAuthHeader = "Authorization: Bearer ●●●●●●●●"

// View is the Settings screen's MCP status. It never holds the API key.
type View struct {
	Enabled       bool
	Listen        string
	KeyConfigured bool
	State         State
	Reason        Reason
	// URL is the address to register in the Grok Bot, AuthHeader the masked
	// header form (always MaskedAuthHeader).
	URL        string
	AuthHeader string
	// ExePath is where the installer places paper-results-mcp; ExeExists
	// tells whether a file is there. ExePath is "" when the application's own
	// location cannot be determined.
	ExePath      string
	ExeExists    bool
	SettingsPath string
	LoadError    LoadErrorKind

	// Managed: the process is one the application started (the screen can
	// stop and restart it). External: the address answers but the
	// application did not start the process, so the screen leaves it alone.
	Managed, External bool
	// Failure and ExitCode describe StateError ("" and 0 otherwise).
	Failure  proc.FailureKind
	ExitCode int
	// ConfigChanged: the settings were saved after the process started; it
	// keeps the old ones until restarted.
	ConfigChanged bool
	// AutoStart: the stored settings are enabled, hold an API key and a
	// permitted address, so the MCP starts with the application next time
	// too. Otherwise AutoStartBlocked says why not.
	AutoStart        bool
	AutoStartBlocked Reason
}

// View reports the current settings and state. It dials only when the MCP is
// enabled, has a key and a valid address (or the application started a
// process), so a default install never makes a network attempt.
func (s *Service) View(ctx context.Context) View {
	cfg, loadErr := settings.Load(s.opts.Path)
	check := s.proc.Check(cfg, loadErr)
	st := s.proc.Status()
	v := View{
		Enabled:          cfg.Enabled,
		Listen:           cfg.Listen,
		KeyConfigured:    cfg.APIKey != "",
		URL:              "http://" + cfg.Listen + "/",
		AuthHeader:       MaskedAuthHeader,
		SettingsPath:     s.opts.Path,
		LoadError:        loadErrorKind(loadErr),
		Managed:          st.Running,
		ConfigChanged:    st.ConfigChanged,
		AutoStart:        check == nil,
		AutoStartBlocked: blockedReason(check),
	}
	v.ExePath, v.ExeExists = s.exe()
	if v.AutoStart && !v.ExeExists {
		// Enabled and valid, but nothing to launch: do not promise it.
		v.AutoStart, v.AutoStartBlocked = false, ReasonExeMissing
	}
	v.State, v.Reason = s.state(ctx, st, check, &v)
	return v
}

func (s *Service) state(ctx context.Context, st proc.Status, check error, v *View) (State, Reason) {
	if st.Stopping {
		return StateStopped, ReasonStopping
	}
	if st.Running {
		switch {
		case s.opts.Dial(ctx, st.Listen) == nil:
			return StateListening, ""
		case st.Starting:
			return StateStarting, ""
		}
		v.Failure = proc.FailNotListening
		return StateError, ""
	}
	if st.Failure != nil {
		v.Failure, v.ExitCode = st.Failure.Kind, st.Failure.ExitCode
		return StateError, ""
	}
	switch r := blockedReason(check); {
	case r == ReasonNoKey:
		return StateNoKey, ""
	case r != "":
		return StateStopped, r
	case s.opts.Dial(ctx, v.Listen) == nil:
		v.External = true
		return StateListening, ""
	}
	return StateStopped, ReasonNotRunning
}

// blockedReason maps a proc.Manager.Check error to the Reason the screen
// words, "" for nil.
func blockedReason(err error) Reason {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, proc.ErrSettingsUnusable):
		return ReasonSettingsUnreadable
	case errors.Is(err, proc.ErrDisabled):
		return ReasonDisabled
	case errors.Is(err, proc.ErrNoKey):
		return ReasonNoKey
	case errors.Is(err, proc.ErrAccessIncomplete):
		return ReasonAccessIncomplete
	default:
		return ReasonListenNotAllowed
	}
}

func (s *Service) exe() (path string, exists bool) {
	path, err := s.opts.ExePath()
	if err != nil {
		return "", false
	}
	info, err := os.Stat(path)
	return path, err == nil && info.Mode().IsRegular()
}

func loadErrorKind(err error) LoadErrorKind {
	switch {
	case err == nil:
		return LoadOK
	case errors.Is(err, settings.ErrInsecurePermissions):
		return LoadInsecure
	default:
		return LoadUnusableFile
	}
}
