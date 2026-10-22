// Package entryqualitypanel builds the status panel of the Settings "Policy
// Engine" groups (issue #861): the Paper and the Live minimum entry quality
// side by side, and which one the Policy Engine is using in the running mode.
package entryqualitypanel

import (
	"context"
	"log/slog"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/paperpolicysettings"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
)

// Title is the panel's title.
const Title = "いま効いている最低エントリー品質"

// Getter reads the effective value of one setting; *opsettings.Service
// implements it.
type Getter interface {
	Get(ctx context.Context, key string) (opsettings.Value, error)
}

// Load renders the panel of the Settings group groupID ("policy-long" or
// "policy-short"). The Policy Engine uses the Paper value when
// metalabelsettings.RuntimeMode is Paper and the Live value (policy.*) in any
// other mode. A read failure is logged and shown as such instead of failing
// the screen.
func Load(ctx context.Context, ops Getter, groupID string) []molecules.SettingDerivedItem {
	liveKey := "policy." + strings.TrimPrefix(groupID, "policy-") + ".min_entry_quality"
	live, lerr := ops.Get(ctx, liveKey)
	paper, perr := ops.Get(ctx, paperpolicysettings.PaperKeyOf(liveKey))
	if lerr != nil || perr != nil {
		slog.Error("settings: read min_entry_quality for the status panel; showing it as unreadable", "live_error", lerr, "paper_error", perr)
		return []molecules.SettingDerivedItem{{Key: "error", Label: "最低エントリー品質を読み取れませんでした", Off: true}}
	}
	paperRuns := metalabelsettings.RuntimeMode == metalabelsettings.ModePaper
	mode, effective, which := "Live", live.Current, "Live の値"
	if paperRuns {
		mode, effective, which = "Paper", paper.Current, "Paper の値"
	}
	const inUse, unused = "← 判定に使われています", "使われていません"
	paperDetail, liveDetail := unused, inUse
	if paperRuns {
		paperDetail, liveDetail = inUse, unused
	}
	return []molecules.SettingDerivedItem{
		{Key: "mode", Label: "実行中のモード", Value: mode},
		{Key: "effective", Label: "いま判定に使われる最低エントリー品質", Value: effective, Detail: "（" + which + "）"},
		{Key: "paper", Label: "Paper の値", Value: paper.Current, Detail: paperDetail, Off: !paperRuns},
		{Key: "live", Label: "Live の値", Value: live.Current, Detail: liveDetail, Off: paperRuns},
	}
}
