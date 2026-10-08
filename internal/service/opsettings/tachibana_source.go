package opsettings

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
)

// TachibanaSource returns the effective 立花 監視銘柄ソース settings (issue
// #728): each stored row, else its default. A stored row that no longer
// parses or validates is logged and replaced by the default, so a damaged row
// cannot stop the nightly batch or the morning start (the Settings screen is
// where it gets fixed); only a store read failure is an error. The caller
// reads it per run, so a saved value applies from the next run.
func (s *Service) TachibanaSource(ctx context.Context) (tachibanasource.TachibanaSourceSettings, error) {
	return LoadTachibanaSource(ctx, s.store)
}

// LoadTachibanaSource is Service.TachibanaSource over store alone.
func LoadTachibanaSource(ctx context.Context, store Store) (tachibanasource.TachibanaSourceSettings, error) {
	text, err := tachibanaSourceText(ctx, store)
	if err != nil {
		return tachibanasource.TachibanaSourceSettings{}, err
	}
	settings := tachibanasource.BuildTachibanaSource(text)
	if err := settings.Validate(); err != nil {
		slog.Warn("opsettings: the stored 立花 screening settings are unusable; using the defaults", "error", err)
		for _, ind := range tachibanasource.TachibanaScreenIndicators {
			delete(text, tachibanasource.TachibanaScreenWeightKey(ind))
			delete(text, tachibanasource.TachibanaScreenTopNKey(ind))
		}
		settings = tachibanasource.BuildTachibanaSource(text)
	}
	return settings, nil
}

// tachibanaSourceText reads the valid stored rows as text by key.
func tachibanaSourceText(ctx context.Context, store Store) (map[string]string, error) {
	keys := tachibanasource.TachibanaSourceSettingKeys()
	text := make(map[string]string, len(keys))
	for _, key := range keys {
		raw, ok, err := store.Get(ctx, key)
		if err != nil {
			return nil, fmt.Errorf("opsettings: read %s: %w", key, err)
		}
		if !ok {
			continue
		}
		v, err := decodeStoredTachibanaSource(key, raw)
		if err != nil {
			slog.Warn("opsettings: ignoring a malformed stored 立花 source setting; using the default", "key", key, "error", err)
			continue
		}
		text[key] = v
	}
	return text, nil
}

// decodeStoredTachibanaSource decodes a stored row and re-validates it.
func decodeStoredTachibanaSource(key, raw string) (string, error) {
	text, err := decodeBroker(key, raw)
	if err != nil {
		return "", err
	}
	normalized, err := tachibanasource.NormalizeTachibanaSourceSetting(key, text)
	if err != nil {
		return "", err
	}
	return fmt.Sprint(normalized), nil
}

// withOverride is a Store whose key reads as encoded; it lets Save validate
// the effective settings as they would be after the write.
type withOverride struct {
	Store
	key, encoded string
}

func (o withOverride) Get(ctx context.Context, key string) (string, bool, error) {
	if key == o.key {
		return o.encoded, true, nil
	}
	return o.Store.Get(ctx, key)
}

// validateTachibanaSource runs the cross-key checks on the settings that
// would be effective once key=encoded is stored.
func (s *Service) validateTachibanaSource(ctx context.Context, key, encoded string) error {
	text, err := tachibanaSourceText(ctx, withOverride{Store: s.store, key: key, encoded: encoded})
	if err != nil {
		return err
	}
	return invalidIf(tachibanasource.BuildTachibanaSource(text).Validate())
}
