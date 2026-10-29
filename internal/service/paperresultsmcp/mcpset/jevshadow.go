package mcpset

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jevprompt"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
)

// The Jev shadow's switch and candidate version are on the settings_set
// whitelist (issue #929) but are not opsettings keys: the Settings screen
// saves them through jevprompt.Service, not the generic operational settings.
// getSetting and saveSetting route those two keys here and every other
// whitelisted key to opsettings, so settings_get / settings_set / the audit log
// treat all of them alike.
//
// Only these two keys are handled. The shadow's cost cap, failure cap, halt and
// throttle records and the live version (the promotion) are never written here.

// isJevShadowKey reports whether key is one of the two shadow keys.
func isJevShadowKey(key string) bool {
	return key == jevshadowsettings.KeyEnabled || key == jevshadowsettings.KeyCandidateVersion
}

// getSetting returns key's effective value through svc, or, for a shadow key,
// from store.
func getSetting(ctx context.Context, store opsettings.Store, svc *opsettings.Service, key string) (opsettings.Value, error) {
	if isJevShadowKey(key) {
		return jevShadowValue(ctx, store, key)
	}
	return svc.Get(ctx, key)
}

// saveSetting validates raw for key and stores it through svc, or, for a shadow
// key, into store. now stamps a shadow key's row.
func saveSetting(ctx context.Context, store opsettings.Store, svc *opsettings.Service, key, raw string, now time.Time) error {
	if isJevShadowKey(key) {
		return saveJevShadow(ctx, store, key, raw, now)
	}
	return svc.Save(ctx, key, raw)
}

// jevShadowValue reads a shadow key the way the shadow itself does
// (jevshadowsettings.Load): a stored row that does not parse counts as the
// default (switch off, no candidate), so a damaged row cannot show the shadow
// as on.
func jevShadowValue(ctx context.Context, store opsettings.Store, key string) (opsettings.Value, error) {
	raw, stored, err := store.Get(ctx, key)
	if err != nil {
		return opsettings.Value{}, fmt.Errorf("mcpset: read %s: %w", key, err)
	}
	v := opsettings.Value{Key: key, Overridden: stored}
	switch key {
	case jevshadowsettings.KeyEnabled:
		v.Default = strconv.FormatBool(jevshadowsettings.DefaultEnabled)
	case jevshadowsettings.KeyCandidateVersion:
		v.Default = ""
	}
	v.Current = v.Default
	if !stored {
		return v, nil
	}
	switch key {
	case jevshadowsettings.KeyEnabled:
		on, perr := jevshadowsettings.ParseEnabled(raw)
		if perr != nil {
			v.Warning = "保存された値が壊れているため、オフとして扱っています"
			return v, nil
		}
		v.Current = strconv.FormatBool(on)
	case jevshadowsettings.KeyCandidateVersion:
		version, perr := jevshadowsettings.ParseVersion(key, raw)
		if perr != nil {
			v.Warning = "保存された値が壊れているため、候補なしとして扱っています"
			return v, nil
		}
		v.Current = version
	}
	return v, nil
}

// saveJevShadow validates raw with the Settings screen's checks and stores it.
// A rejection is an *opsettings.InvalidValueError (the MCP's invalid_value
// with the reason) and stores nothing.
//
//   - candidate_version: jevprompt.ValidateCandidate, the same call as
//     jevprompt.Service.SetCandidate: a registered version that is not the live
//     one. The live version is read strictly (jev.ResolveLiveVersion): a failed
//     read is an error, never a guess.
//   - enabled: true / false (1 / 0); true needs a candidate to be stored.
func saveJevShadow(ctx context.Context, store opsettings.Store, key, raw string, now time.Time) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return opsettings.ErrEmptyValue
	}
	switch key {
	case jevshadowsettings.KeyCandidateVersion:
		live, err := jev.ResolveLiveVersion(ctx, store)
		if err != nil {
			return fmt.Errorf("mcpset: read the live prompt version: %w", err)
		}
		encoded, err := jevprompt.ValidateCandidate(raw, live)
		if err != nil {
			return &opsettings.InvalidValueError{Reason: err.Error()}
		}
		return store.Set(ctx, key, encoded, now)
	case jevshadowsettings.KeyEnabled:
		on, ok := parseSwitchText(raw)
		if !ok {
			return &opsettings.InvalidValueError{Reason: fmt.Sprintf("%q は真偽として読めません。true / false（1 / 0 も可）で指定してください", raw)}
		}
		if on {
			cand, err := jevShadowValue(ctx, store, jevshadowsettings.KeyCandidateVersion)
			if err != nil {
				return err
			}
			if cand.Current == "" {
				return &opsettings.InvalidValueError{Reason: "候補の版（" + jevshadowsettings.KeyCandidateVersion + "）が未設定のため、オンにできません。先に候補の版を設定してください"}
			}
		}
		return store.Set(ctx, key, jevshadowsettings.Switch(on), now)
	}
	return opsettings.ErrUnknownKey
}

// parseSwitchText reads the text of a switch: true / false or 1 / 0.
func parseSwitchText(s string) (on, ok bool) {
	switch strings.ToLower(s) {
	case "true", "1":
		return true, true
	case "false", "0":
		return false, true
	}
	return false, false
}
