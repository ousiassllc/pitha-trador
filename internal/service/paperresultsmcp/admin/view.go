package admin

import (
	"context"
	"errors"
	"os"

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
)

// Reason says why a State is StateStopped; it is empty for the other states.
type Reason string

const (
	// ReasonDisabled: the switch is off (the default).
	ReasonDisabled Reason = "disabled"
	// ReasonSettingsUnreadable: the settings file cannot be used (LoadError
	// says how), so the MCP process would not listen either.
	ReasonSettingsUnreadable Reason = "settings_unreadable"
	// ReasonListenNotAllowed: the stored address is outside the loopback /
	// tailnet rule, so the process refuses to bind it.
	ReasonListenNotAllowed Reason = "listen_not_allowed"
	// ReasonNotRunning: configured to listen, but the MCP process is not
	// reachable at the address (not started yet, or started before the
	// settings changed).
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
}

// View reports the current settings and state. It dials only when the MCP is
// enabled, has a key and a valid address, so a default install never makes a
// network attempt.
func (s *Service) View(ctx context.Context) View {
	cfg, loadErr := settings.Load(s.opts.Path)
	v := View{
		Enabled:       cfg.Enabled,
		Listen:        cfg.Listen,
		KeyConfigured: cfg.APIKey != "",
		URL:           "http://" + cfg.Listen + "/",
		AuthHeader:    MaskedAuthHeader,
		SettingsPath:  s.opts.Path,
		LoadError:     loadErrorKind(loadErr),
	}
	v.ExePath, v.ExeExists = s.exe()
	v.State, v.Reason = s.state(ctx, cfg, loadErr)
	return v
}

func (s *Service) state(ctx context.Context, cfg settings.Settings, loadErr error) (State, Reason) {
	switch {
	case loadErr != nil:
		return StateStopped, ReasonSettingsUnreadable
	case !cfg.Enabled:
		return StateStopped, ReasonDisabled
	case cfg.APIKey == "":
		return StateNoKey, ""
	case settings.ValidateListen(cfg.Listen, s.opts.InterfaceAddrs) != nil:
		return StateStopped, ReasonListenNotAllowed
	case s.opts.Dial(ctx, cfg.Listen) != nil:
		return StateStopped, ReasonNotRunning
	}
	return StateListening, ""
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
