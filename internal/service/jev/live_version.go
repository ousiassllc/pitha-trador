package jev

import (
	"context"
	"log/slog"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
)

// ResolveLiveVersion returns the live Trader prompt version: the one a
// promotion stored in runtime_settings, else the build default
// TraderQuestionVersion. A stored version this build does not know (for example
// after a downgrade) or a damaged stored value falls back to the build default
// and is logged as an error: an unknown name must never be sent. A nil store is
// the build default. A failed READ of the setting is an error, never the build
// default: "could not read" must not silently turn a promoted version back into
// the old one.
func ResolveLiveVersion(ctx context.Context, store jevshadowsettings.Store) (string, error) {
	if store == nil {
		return TraderQuestionVersion, nil
	}
	stored, err := jevshadowsettings.LoadLiveVersion(ctx, store)
	switch {
	case err != nil:
		return "", err
	case stored == "":
		return TraderQuestionVersion, nil
	case !IsTraderVersion(stored):
		slog.Error("jev: the stored live trader prompt version is not registered in this build; using the build default",
			"stored", stored, "default", TraderQuestionVersion)
		return TraderQuestionVersion, nil
	}
	return stored, nil
}

// LiveVersion is ResolveLiveVersion for display (the Settings screen, the MCP):
// a failed read is logged and shows the build default. Never use it to decide
// what to send or record.
func LiveVersion(ctx context.Context, store jevshadowsettings.Store) string {
	v, err := ResolveLiveVersion(ctx, store)
	if err != nil {
		slog.Error("jev: read the live prompt version for display", "error", err)
		return TraderQuestionVersion
	}
	return v
}
