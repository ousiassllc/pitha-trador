package opsettings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
)

// Store is the runtime_settings access Service needs;
// *system.RuntimeSettingsRepository implements it.
type Store interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
	Set(ctx context.Context, key, value string, updatedAt time.Time) error
	Delete(ctx context.Context, key string) error
}

var (
	// ErrUnknownKey is returned for a key Service does not manage.
	ErrUnknownKey = errors.New("opsettings: unknown setting key")
	// ErrEmptyValue is returned by Save for a blank value; removing a
	// setting is Reset's job.
	ErrEmptyValue = errors.New("opsettings: empty value")
)

// InvalidValueError reports why Save rejected a value; the store is left
// untouched.
type InvalidValueError struct{ Reason string }

func (e *InvalidValueError) Error() string { return "opsettings: invalid value: " + e.Reason }

// Value is one setting as the Settings screen shows it. Current is the
// effective value as text: the stored value when Overridden, otherwise the
// config/strategy.yaml value ("" for the path settings, which have no yaml
// value). Default describes what applies while nothing is stored: the yaml
// value for thresholds, the default log directory for the log directory, ""
// (disabled) for the backup directory. Warning is a non-empty operator hint
// when the stored value looks unusable (e.g. a backup directory that is not
// available right now).
type Value struct {
	Key        string
	Current    string
	Default    string
	Overridden bool
	Warning    string
}

// Service reads and writes the settings listed in Keys.
type Service struct {
	store         Store
	policy        config.PolicyConfig
	screener      config.FastScreenerConfig
	defaultLogDir string
	now           func() time.Time
}

// New returns a Service over store with strategy's policy / fast_screener
// sections as the yaml baseline and defaultLogDir as the log directory used
// while none is configured.
func New(store Store, strategy config.StrategyConfig, defaultLogDir string) *Service {
	return &Service{store: store, policy: strategy.Policy, screener: strategy.FastScreener, defaultLogDir: defaultLogDir, now: time.Now}
}

// Keys returns every key Service manages: the backup and log directories,
// the policy thresholds, then the screener thresholds.
func Keys() []string {
	keys := []string{config.KeyBackupDir, config.KeyLogDir}
	keys = append(keys, config.PolicySettingKeys()...)
	return append(keys, config.FastScreenerSettingKeys()...)
}

// IsKnownKey reports whether key is one of Keys.
func IsKnownKey(key string) bool { return slices.Contains(Keys(), key) }

type kind int

const (
	kindPath kind = iota
	kindEntryQuality
	kindNumber
	kindInteger
)

func kindOf(key string) kind {
	switch {
	case key == config.KeyBackupDir || key == config.KeyLogDir:
		return kindPath
	case strings.HasSuffix(key, ".min_entry_quality"):
		return kindEntryQuality
	case key == "screener.top_n":
		return kindInteger
	default:
		return kindNumber
	}
}

// Get returns key's effective value.
func (s *Service) Get(ctx context.Context, key string) (Value, error) {
	if !IsKnownKey(key) {
		return Value{}, ErrUnknownKey
	}
	raw, stored, err := s.store.Get(ctx, key)
	if err != nil {
		return Value{}, fmt.Errorf("opsettings: read %s: %w", key, err)
	}
	v := Value{Key: key, Overridden: stored, Default: s.defaultText(key)}
	switch {
	case kindOf(key) == kindPath:
		if stored {
			if v.Current, err = decodeString(key, raw); err != nil {
				return Value{}, err
			}
		}
		if key == config.KeyBackupDir && v.Current != "" {
			v.Warning = backupDirWarning(v.Current)
		}
	case stored:
		if v.Current, err = decodeText(key, raw); err != nil {
			return Value{}, err
		}
	default:
		v.Current = v.Default
	}
	return v, nil
}

// Save validates raw for key and stores it. A blank raw is ErrEmptyValue; a
// value that is malformed, out of range or would make the effective
// thresholds invalid (config.ValidatePolicyOverrides /
// config.ValidateFastScreenerOverrides) is an *InvalidValueError. The store
// is only written on success.
func (s *Service) Save(ctx context.Context, key, raw string) error {
	if !IsKnownKey(key) {
		return ErrUnknownKey
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ErrEmptyValue
	}
	encoded, err := encode(key, raw)
	if err != nil {
		return err
	}
	if err := s.validateEffective(ctx, key, encoded); err != nil {
		return err
	}
	if err := s.store.Set(ctx, key, encoded, s.now()); err != nil {
		return fmt.Errorf("opsettings: save %s: %w", key, err)
	}
	return nil
}

// Reset removes key's stored value so the lower-priority source applies
// again (yaml for thresholds, "disabled" / the default directory for the
// paths). Resetting an unset key is not an error.
func (s *Service) Reset(ctx context.Context, key string) error {
	if !IsKnownKey(key) {
		return ErrUnknownKey
	}
	if err := s.store.Delete(ctx, key); err != nil {
		return fmt.Errorf("opsettings: reset %s: %w", key, err)
	}
	return nil
}

// defaultText is what applies to key while nothing is stored.
func (s *Service) defaultText(key string) string {
	switch {
	case key == config.KeyBackupDir:
		return ""
	case key == config.KeyLogDir:
		return s.defaultLogDir
	case kindOf(key) == kindEntryQuality:
		v, _ := config.PolicySettingValue(s.policy, key)
		return v.(string)
	case strings.HasPrefix(key, "policy."):
		v, _ := config.PolicySettingValue(s.policy, key)
		return formatNumber(v.(float64))
	default:
		v, _ := config.FastScreenerSettingValue(s.screener, key)
		return formatNumber(v)
	}
}

func decodeString(key, raw string) (string, error) {
	var v string
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return "", fmt.Errorf("opsettings: stored %s=%q is not a JSON string: %w", key, raw, err)
	}
	return v, nil
}

// decodeText renders a stored threshold row as text.
func decodeText(key, raw string) (string, error) {
	if kindOf(key) == kindEntryQuality {
		return decodeString(key, raw)
	}
	var f float64
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		return "", fmt.Errorf("opsettings: stored %s=%q is not a JSON number: %w", key, raw, err)
	}
	return formatNumber(f), nil
}

func formatNumber(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func backupDirWarning(dir string) string {
	info, err := os.Stat(dir)
	switch {
	case err != nil:
		return "このディレクトリは現在利用できません（外付けドライブ未接続・パス誤りなど）。存在しない間、バックアップは失敗します。"
	case !info.IsDir():
		return "このパスはディレクトリではありません。バックアップは失敗します。"
	}
	return ""
}
