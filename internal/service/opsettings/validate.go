package opsettings

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/barriersettings"
	"github.com/ousiassllc/pitha-trador/internal/config/costsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/logsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/orderflowsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/papersettings"
	"github.com/ousiassllc/pitha-trador/internal/config/regimesettings"
	"github.com/ousiassllc/pitha-trador/internal/config/snapshotagesettings"
	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
)

// encode parses raw (already trimmed, non-empty) for key into the JSON
// scalar runtime_settings stores.
func encode(key, raw string) (string, error) {
	var value any
	switch kindOf(key) {
	case kindPath:
		if err := validatePath(raw); err != nil {
			return "", err
		}
		value = raw
	case kindPrivateKey:
		if err := validatePath(raw); err != nil {
			return "", err
		}
		if err := validatePrivateKeyFile(raw); err != nil {
			return "", err
		}
		value = raw
	case kindBroker:
		v, err := config.NormalizeBrokerSetting(key, raw)
		if err != nil {
			return "", &InvalidValueError{Reason: err.Error()}
		}
		value = v
	case kindSource:
		v, err := tachibanasource.NormalizeTachibanaSourceSetting(key, raw)
		if err != nil {
			return "", &InvalidValueError{Reason: err.Error()}
		}
		value = v
	case kindMetaLabel:
		if !metalabelsettings.IsStringKey(key) {
			f, err := strconv.ParseFloat(raw, 64)
			if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
				return "", &InvalidValueError{Reason: fmt.Sprintf("%q は数値ではありません", raw)}
			}
			value = f
			break
		}
		v, err := metalabelsettings.Normalize(key, raw)
		if err != nil {
			return "", &InvalidValueError{Reason: err.Error()}
		}
		value = v
	case kindLog:
		if key == config.KeyLogLevel {
			v, err := logsettings.NormalizeLevel(raw)
			if err != nil {
				return "", &InvalidValueError{Reason: err.Error()}
			}
			value = v
			break
		}
		n, err := strconv.ParseInt(raw, 10, 32)
		if err != nil {
			return "", &InvalidValueError{Reason: fmt.Sprintf("%q は整数ではありません", raw)}
		}
		value = n
	case kindEntryQuality:
		value = raw // the allowed values are checked with the effective config
	case kindInteger:
		n, err := strconv.ParseInt(raw, 10, 32)
		if err != nil {
			return "", &InvalidValueError{Reason: fmt.Sprintf("%q は整数ではありません", raw)}
		}
		value = n
	default:
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return "", &InvalidValueError{Reason: fmt.Sprintf("%q は数値ではありません", raw)}
		}
		value = f
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("opsettings: encode %s: %w", key, err)
	}
	return string(data), nil
}

func validatePath(p string) error {
	if strings.IndexFunc(p, unicode.IsControl) >= 0 {
		return &InvalidValueError{Reason: "パスに制御文字は使えません"}
	}
	if !filepath.IsAbs(p) {
		return &InvalidValueError{Reason: "絶対パスで指定してください"}
	}
	return nil
}

// validateEffective applies the new value on top of the effective
// thresholds (yaml + every stored row) and runs the FR-POLICY / FR-FS-4
// validation, so the Settings screen cannot store a value the engines would
// misuse. Path settings need no further check.
func (s *Service) validateEffective(ctx context.Context, key, encoded string) error {
	switch {
	case strings.HasPrefix(key, "policy."):
		cfg := s.policy
		if err := s.overlay(ctx, config.PolicySettingKeys(), func(k, raw string) error { return config.ApplyPolicySetting(&cfg, k, raw) }); err != nil {
			return err
		}
		if err := config.ApplyPolicySetting(&cfg, key, encoded); err != nil {
			return &InvalidValueError{Reason: err.Error()}
		}
		return invalidIf(config.ValidatePolicyOverrides(cfg))
	case strings.HasPrefix(key, "screener."):
		cfg := s.screener
		if err := s.overlay(ctx, config.FastScreenerSettingKeys(), func(k, raw string) error { return config.ApplyFastScreenerSetting(&cfg, k, raw) }); err != nil {
			return err
		}
		if err := config.ApplyFastScreenerSetting(&cfg, key, encoded); err != nil {
			return &InvalidValueError{Reason: err.Error()}
		}
		return invalidIf(config.ValidateFastScreenerOverrides(cfg))
	case orderflowsettings.IsKey(key):
		windows := orderflowsettings.Default()
		if err := s.overlay(ctx, orderflowsettings.Keys(), func(k, raw string) error { return orderflowsettings.Apply(&windows, k, raw) }); err != nil {
			return err
		}
		if err := orderflowsettings.Apply(&windows, key, encoded); err != nil {
			return &InvalidValueError{Reason: err.Error()}
		}
		return invalidIf(windows.Validate())
	case snapshotagesettings.IsKey(key):
		// A single stand-alone value: only its own range is checked.
		if _, err := snapshotagesettings.Parse(encoded); err != nil {
			return &InvalidValueError{Reason: err.Error()}
		}
		return nil
	case key == config.KeyLogRetentionDays:
		// A single stand-alone value: only its own range is checked.
		if _, err := logsettings.ParseRetentionDays(encoded); err != nil {
			return &InvalidValueError{Reason: err.Error()}
		}
		return nil
	case dailyguardsettings.IsKey(key):
		// Each threshold stands alone (0 disables it), so only its own range is checked.
		var stored dailyguardsettings.Settings
		if err := dailyguardsettings.Apply(&stored, key, encoded); err != nil {
			return &InvalidValueError{Reason: err.Error()}
		}
		return nil
	case papersettings.IsKey(key):
		// Each value stands alone (capital > 0, percentages in (0, 100]), so
		// only its own range is checked.
		var stored papersettings.Overrides
		if err := papersettings.Apply(&stored, key, encoded); err != nil {
			return &InvalidValueError{Reason: err.Error()}
		}
		return nil
	case barriersettings.IsKey(key):
		barriers := barriersettings.Default()
		if err := s.overlay(ctx, barriersettings.Keys(), func(k, raw string) error { return barriersettings.Apply(&barriers, k, raw) }); err != nil {
			return err
		}
		if err := barriersettings.Apply(&barriers, key, encoded); err != nil {
			return &InvalidValueError{Reason: err.Error()}
		}
		if err := barriers.Validate(); err != nil {
			return invalidIf(err)
		}
		return s.validateLabelCostAgainst(ctx, barriers)
	case costsettings.IsKey(key):
		return s.validateCost(ctx, key, encoded)
	case metalabelsettings.IsKey(key):
		// Each field stands alone: Apply checks the single value's range.
		meta := metalabelsettings.Default()
		if err := metalabelsettings.Apply(&meta, key, encoded); err != nil {
			return &InvalidValueError{Reason: err.Error()}
		}
		return nil
	case regimesettings.IsKey(key):
		// Each field stands alone: Apply checks the single value's range.
		regime := regimesettings.Default()
		if err := regimesettings.Apply(&regime, key, encoded); err != nil {
			return &InvalidValueError{Reason: err.Error()}
		}
		return nil
	case strings.HasPrefix(key, "broker.tachibana.screen."):
		return s.validateTachibanaSource(ctx, key, encoded)
	}
	return nil
}

// crossValidated reports whether key's validity depends on other stored rows,
// i.e. validateEffective checks it against them rather than on its own.
func crossValidated(key string) bool {
	return strings.HasPrefix(key, "policy.") || strings.HasPrefix(key, "screener.") ||
		orderflowsettings.IsKey(key) || barriersettings.IsKey(key) || costsettings.IsKey(key)
}

// overlay feeds every stored row among keys to apply.
func (s *Service) overlay(ctx context.Context, keys []string, apply func(key, raw string) error) error {
	for _, k := range keys {
		raw, ok, err := s.store.Get(ctx, k)
		if err != nil {
			return fmt.Errorf("opsettings: read %s: %w", k, err)
		}
		if !ok {
			continue
		}
		if err := apply(k, raw); err != nil {
			return fmt.Errorf("opsettings: stored %s: %w", k, err)
		}
	}
	return nil
}

func invalidIf(err error) error {
	if err == nil {
		return nil
	}
	return &InvalidValueError{Reason: strings.ReplaceAll(err.Error(), "\n", "; ")}
}
