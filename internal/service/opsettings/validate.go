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
	}
	return nil
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
