package risk

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// setBoolSetting/getBoolSetting and setStringSetting/getTimeSetting
// JSON-encode runtime_settings.value (er.md: "value | text | NOT NULL |
// JSON文字列"), matching the schema's documented convention for every
// key this package owns (system.paused, system.killed,
// SettingKeyLastUIHeartbeatAt).

func (e *Engine) setBoolSetting(ctx context.Context, key string, value bool) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("risk: encode setting %s: %w", key, err)
	}
	if err := e.settings.Set(ctx, key, string(data), e.now()); err != nil {
		return fmt.Errorf("risk: set %s: %w", key, err)
	}
	return nil
}

func (e *Engine) getBoolSetting(ctx context.Context, key string) (bool, error) {
	raw, ok, err := e.settings.Get(ctx, key)
	if err != nil {
		return false, fmt.Errorf("risk: get %s: %w", key, err)
	}
	if !ok {
		return false, nil
	}
	var value bool
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return false, fmt.Errorf("risk: decode setting %s=%q: %w", key, raw, err)
	}
	return value, nil
}

func (e *Engine) setStringSetting(ctx context.Context, key, value string) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("risk: encode setting %s: %w", key, err)
	}
	if err := e.settings.Set(ctx, key, string(data), e.now()); err != nil {
		return fmt.Errorf("risk: set %s: %w", key, err)
	}
	return nil
}

func (e *Engine) getTimeSetting(ctx context.Context, key string) (time.Time, bool, error) {
	raw, ok, err := e.settings.Get(ctx, key)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("risk: get %s: %w", key, err)
	}
	if !ok {
		return time.Time{}, false, nil
	}
	var s string
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return time.Time{}, false, fmt.Errorf("risk: decode setting %s=%q: %w", key, raw, err)
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("risk: parse setting %s=%q: %w", key, s, err)
	}
	return t.UTC(), true, nil
}
