package jevprompt

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevshadow"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
)

var (
	// ErrUnknownVersion is returned by Promote for a version this build does not define.
	ErrUnknownVersion = errors.New("jevprompt: unknown trader prompt version")
	// ErrAlreadyLive is returned by Promote when the version is the live one already.
	ErrAlreadyLive = errors.New("jevprompt: the version is already live")
)

// Promote switches the live Trader prompt version to version inside tx (a
// transaction) and appends the audit row (who, when, previous version, new version) in the same
// transaction: both are stored or neither is. It is the only writer of
// jevshadowsettings.KeyLiveVersion, and only an explicit GUI or MCP operation
// calls it (Writer.PromotePrompt, Admin.Promote): nothing promotes
// automatically, and the self-improvement loop (Sol/Opus) has no path to it
// (domain.ValidatePolicyChanges accepts policy.* keys only).
//
// The version must be registered (ErrUnknownVersion) and differ from the live
// one (ErrAlreadyLive). actor is jevshadow.ActorMCP or ...GUI; client
// is the MCP caller's host ("" for the GUI).
func Promote(ctx context.Context, tx sqlutil.Executor, now time.Time, actor, client, version string) (jevshadow.Promotion, error) {
	if !jev.IsTraderVersion(version) {
		return jevshadow.Promotion{}, fmt.Errorf("%w: %q", ErrUnknownVersion, version)
	}
	store := system.NewRuntimeSettingsRepositoryOn(tx)
	from, err := jev.ResolveLiveVersion(ctx, store)
	if err != nil {
		return jevshadow.Promotion{}, err
	}
	if from == version {
		return jevshadow.Promotion{}, fmt.Errorf("%w: %q", ErrAlreadyLive, version)
	}
	value, err := jevshadowsettings.EncodeVersion(version)
	if err != nil {
		return jevshadow.Promotion{}, err
	}
	if err := store.Set(ctx, jevshadowsettings.KeyLiveVersion, value, now); err != nil {
		return jevshadow.Promotion{}, err
	}
	return judgement.InsertPromotion(ctx, tx, jevshadow.Promotion{At: now, Actor: actor, Client: client, FromVersion: from, ToVersion: version})
}
