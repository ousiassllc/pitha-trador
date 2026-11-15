// Package airoles is the application side of the AI provider layer that the
// Settings screen uses (issue #1080): it reads which provider and model each AI
// role is set to (and which pair each role runs on since the last start), whether
// each provider's key is set, and saves or resets a role's setting.
//
// Only the Settings screen reaches Save and Reset: nothing in the MCP writes
// these keys (they are not on the mcpwritesettings whitelist), because the live
// Jev role decides what is traded. A key's value is never read out: the secrets
// store is only asked whether a key exists.
package airoles

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/airole"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
)

// ErrInvalidInput is wrapped by every rejected operator input; the message is
// an operator-facing reason.
var ErrInvalidInput = errors.New("airoles: invalid input")

// historyLimit is how many recent history rows State returns.
const historyLimit = 20

// KeyStore is the secrets access State needs to tell whether a provider's key
// is set. Only the "found" flag is ever used; the value is discarded unread.
// *system.SecretsRepository implements it.
type KeyStore interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
}

// Store is the settings and history access of the roles;
// *system.AIRoleRepository implements it.
type Store interface {
	Save(ctx context.Context, role string, s airole.Setting) error
	Reset(ctx context.Context, role string) error
	History(ctx context.Context, limit int) ([]system.AIRoleChange, error)
	LastStarted(ctx context.Context, role string) (system.AIRoleChange, bool, error)
}

// Getter reads runtime_settings; *system.RuntimeSettingsRepository implements it.
type Getter = airole.Getter

// Provider is one provider with whether its key is set.
type Provider struct {
	ID   string
	Name string
	// KeyKnown is true when the secrets store could be asked; KeySet then says
	// whether the provider's key is set. The value is never read out.
	KeyKnown bool
	KeySet   bool
}

// Role is one role's setting.
type Role struct {
	airole.Info
	// Setting is the setting in force once the app restarts: the stored one, or
	// the default.
	Setting airole.Setting
	// Stored is true when Setting is a saved value, not the default.
	Stored bool
	// Damaged says why a stored value is ignored ("" when it is fine).
	Damaged string
	// Running is the pair the role has run on since the last start, nil when no
	// start is recorded yet. It differs from Setting between a save and a restart.
	Running *airole.Setting
}

// Change is one history row.
type Change struct {
	At     time.Time
	Saved  bool // false: a start record
	Role   string
	Before airole.Setting
	After  airole.Setting
}

// State is everything the Settings panel shows.
type State struct {
	Providers []Provider
	Roles     []Role
	History   []Change
}

// Service reads and changes the roles' settings.
type Service struct {
	settings Getter
	store    Store
	keys     KeyStore
}

// New returns a Service. keys may be nil (State then reports every key as
// unknown).
func New(settings Getter, store Store, keys KeyStore) *Service {
	return &Service{settings: settings, store: store, keys: keys}
}

// State reads the current state.
func (s *Service) State(ctx context.Context) (State, error) {
	loaded := airole.Load(ctx, s.settings)
	st := State{Providers: s.providers(ctx)}
	for _, info := range airole.Infos() {
		r := Role{Info: info, Setting: loaded.Settings[info.ID], Stored: loaded.Stored[info.ID], Damaged: loaded.Damaged[info.ID]}
		last, ok, err := s.store.LastStarted(ctx, info.ID)
		if err != nil {
			return State{}, err
		}
		if ok {
			running := last.After
			r.Running = &running
		}
		st.Roles = append(st.Roles, r)
	}
	rows, err := s.store.History(ctx, historyLimit)
	if err != nil {
		return State{}, err
	}
	for _, c := range rows {
		st.History = append(st.History, Change{At: c.At, Saved: c.Kind == system.AIRoleSaved, Role: c.Role, Before: c.Before, After: c.After})
	}
	return st, nil
}

func (s *Service) providers(ctx context.Context) []Provider {
	keyOf := map[string]string{airole.ProviderCustom: config.KeyJevAPIKey, airole.ProviderOpenRouter: config.KeyOpenRouterAPIKey}
	var out []Provider
	for _, id := range airole.Providers() {
		p := Provider{ID: id, Name: airole.ProviderName(id)}
		if s.keys != nil {
			if _, ok, err := s.keys.Get(ctx, keyOf[id]); err == nil {
				p.KeyKnown, p.KeySet = true, ok
			}
		}
		out = append(out, p)
	}
	return out
}

// Validate checks the input for role and returns the setting to store.
func Validate(role, provider, model string) (airole.Setting, error) {
	set, err := airole.Normalize(role, airole.Setting{Provider: provider, Model: model})
	if err != nil {
		var inv *airole.InvalidError
		if errors.As(err, &inv) {
			return airole.Setting{}, fmt.Errorf("%w: %s", ErrInvalidInput, strings.TrimSuffix(inv.Message, "。"))
		}
		return airole.Setting{}, err
	}
	return set, nil
}

// Save stores the provider and model of role. The change is recorded in the
// role history in the same transaction.
func (s *Service) Save(ctx context.Context, role, provider, model string) error {
	set, err := Validate(role, provider, model)
	if err != nil {
		return err
	}
	return s.store.Save(ctx, role, set)
}

// Reset removes role's stored setting: the default applies again.
func (s *Service) Reset(ctx context.Context, role string) error {
	if !airole.IsRole(role) {
		return fmt.Errorf("%w: 役割が正しくありません", ErrInvalidInput)
	}
	return s.store.Reset(ctx, role)
}
