package opsettings

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/ousiassllc/pitha-trador/internal/config"
)

// decodeBroker renders a stored broker setting row as text: a JSON string,
// or the number of config.KeyTachibanaRequestMaxPerSecond.
func decodeBroker(key, raw string) (string, error) {
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return "", fmt.Errorf("opsettings: stored %s=%q is not JSON: %w", key, raw, err)
	}
	switch x := v.(type) {
	case string:
		return x, nil
	case float64:
		return formatNumber(x), nil
	}
	return "", fmt.Errorf("opsettings: stored %s=%q is neither a JSON string nor a number", key, raw)
}

// Broker returns the effective broker selection and 立花 settings: each stored
// row, else its default. A stored row that no longer parses or validates is
// logged and replaced by the default, so a damaged row cannot keep the app
// from starting (the Settings screen is where it gets fixed); only a store
// read failure is an error.
func (s *Service) Broker(ctx context.Context) (config.BrokerSettings, error) {
	return LoadBroker(ctx, s.store)
}

// LoadBroker is Service.Broker over store alone, for the composition root's
// start-up read.
func LoadBroker(ctx context.Context, store Store) (config.BrokerSettings, error) {
	text := make(map[string]string, len(config.BrokerSettingKeys()))
	for _, key := range config.BrokerSettingKeys() {
		raw, ok, err := store.Get(ctx, key)
		if err != nil {
			return config.BrokerSettings{}, fmt.Errorf("opsettings: read %s: %w", key, err)
		}
		text[key] = config.BrokerSettingDefault(key)
		if !ok {
			continue
		}
		v, err := decodeStoredBroker(key, raw)
		if err != nil {
			slog.Warn("opsettings: ignoring a malformed stored broker setting; using the default", "key", key, "error", err)
			continue
		}
		text[key] = v
	}
	perSecond, err := strconv.Atoi(text[config.KeyTachibanaRequestMaxPerSecond])
	if err != nil { // unreachable: stored values are validated by decodeStoredBroker
		perSecond = config.DefaultTachibanaRequestMaxPerSecond
	}
	return config.BrokerSettings{
		Provider: text[config.KeyBrokerProvider],
		Tachibana: config.TachibanaSettings{
			Environment:         text[config.KeyTachibanaEnvironment],
			DemoBaseURL:         text[config.KeyTachibanaDemoBaseURL],
			ProdBaseURL:         text[config.KeyTachibanaProdBaseURL],
			DemoPrivateKeyPath:  text[config.KeyTachibanaDemoPrivateKeyPath],
			ProdPrivateKeyPath:  text[config.KeyTachibanaProdPrivateKeyPath],
			RequestMaxPerSecond: perSecond,
			ReauthTime:          text[config.KeyTachibanaReauthTime],
		},
	}, nil
}

// decodeStoredBroker decodes and re-validates a stored row (private key
// paths are only decoded: their file may live on a drive that is not
// mounted yet at start-up).
func decodeStoredBroker(key, raw string) (string, error) {
	if config.IsPrivateKeyPathKey(key) {
		return decodeString(key, raw)
	}
	text, err := decodeBroker(key, raw)
	if err != nil {
		return "", err
	}
	normalized, err := config.NormalizeBrokerSetting(key, text)
	if err != nil {
		return "", err
	}
	return fmt.Sprint(normalized), nil
}
