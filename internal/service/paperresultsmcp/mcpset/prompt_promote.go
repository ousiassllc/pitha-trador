package mcpset

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/domain/jevshadow"
	"github.com/ousiassllc/pitha-trador/internal/service/jevprompt"

	"github.com/ousiassllc/pitha-trador/internal/repository/system"
)

// ErrPromoteUnknownVersion is returned by PromotePrompt for a version this build
// does not define; ErrPromoteAlreadyLive when it is the live version already.
// Nothing was stored.
var (
	ErrPromoteUnknownVersion = errors.New("mcpset: unknown trader prompt version")
	ErrPromoteAlreadyLive    = errors.New("mcpset: the version is already live")
)

// PromotePrompt switches the live Jev Trader prompt version to version (issue
// #872: the MCP's jev_prompt_promote). It is the sixth kind of change the
// Writer makes: one transaction that upserts the live-version runtime_settings
// row (jevshadowsettings.KeyLiveVersion) and appends one jev_prompt_promotions
// row (who: this MCP client's host, when, previous version, new version).
//
// It follows the write-permission switch of settings_set (docs/architecture/
// paper-results-mcp.md §7.2): while writes are off it returns ErrWriteDisabled
// and stores nothing. It is an explicit operation only; nothing calls it on a
// schedule or on a result, and the self-improvement loop (Sol/Opus) cannot reach
// it. client is the caller's host (audit "who").
func (w *Writer) PromotePrompt(ctx context.Context, client, version string) (jevshadow.Promotion, error) {
	var out jevshadow.Promotion
	err := runInTx(ctx, w.db, "write transaction", func(tx *sql.Tx) error {
		on, err := WriteEnabled(ctx, system.NewRuntimeSettingsRepositoryOn(tx))
		if err != nil {
			return err
		}
		if !on {
			return ErrWriteDisabled
		}
		out, err = jevprompt.Promote(ctx, tx, w.now().UTC(), jevshadow.ActorMCP, client, version)
		return err
	})
	switch {
	case errors.Is(err, jevprompt.ErrUnknownVersion):
		return jevshadow.Promotion{}, fmt.Errorf("%w: %q", ErrPromoteUnknownVersion, version)
	case errors.Is(err, jevprompt.ErrAlreadyLive):
		return jevshadow.Promotion{}, fmt.Errorf("%w: %q", ErrPromoteAlreadyLive, version)
	}
	return out, err
}
