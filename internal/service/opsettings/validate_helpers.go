package opsettings

import (
	"context"
	"fmt"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/config/barriersettings"
	"github.com/ousiassllc/pitha-trador/internal/config/bucketsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/costsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/orderflowsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/papersettings"
	"github.com/ousiassllc/pitha-trador/internal/config/watchsettings"
)

// crossValidated reports whether key's validity depends on other stored rows,
// i.e. validateEffective checks it against them rather than on its own.
func crossValidated(key string) bool {
	return strings.HasPrefix(key, "policy.") || strings.HasPrefix(key, "screener.") ||
		orderflowsettings.IsKey(key) || barriersettings.IsKey(key) || costsettings.IsKey(key) ||
		bucketsettings.IsKey(key) || key == papersettings.KeyInitialCapitalJPY
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

// validateWatch checks one watchsettings value (issue #1055) the way the
// application will read it back.
func validateWatch(key, encoded string) error {
	var err error
	switch key {
	case watchsettings.KeyMaxWatched:
		_, err = watchsettings.ParseMaxWatched(encoded)
	case watchsettings.KeyObservationEnabled:
		_, err = watchsettings.ParseEnabled(encoded)
	case watchsettings.KeyObservationSymbols:
		_, err = watchsettings.ParseSymbols(encoded)
	default:
		_, err = watchsettings.ParseCap(encoded)
	}
	if err != nil {
		return &InvalidValueError{Reason: err.Error()}
	}
	return nil
}
