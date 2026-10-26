package home

import (
	"context"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/orderproposal"
	"github.com/ousiassllc/pitha-trador/internal/service/activityfeed"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/settings/entryqualitypanel"
	"github.com/ousiassllc/pitha-trador/internal/web/labels"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms/homepanels"
	"github.com/ousiassllc/pitha-trador/internal/web/pages"
)

const (
	// recentTradeLimit is how many closed trades 最近の取引 lists.
	recentTradeLimit = 5
	// killEventLimit is how many Kill Switch events 最近の Kill Switch lists.
	killEventLimit = 3

	errRead = "を読み取れませんでした。"
)

// positionsData is the result of one positions read, shared by the P&L and the
// 保有ポジション cards.
type positionsData struct {
	open []domain.Position
	err  error
}

// loadPositions reads the open positions (closed_at IS NULL) from the Source,
// not from a "newest N rows" list: with many newer closed rows an old open
// position would fall out of such a list, understating the unrealized total
// and the count the daily guard sees.
func (h *Handler) loadPositions(ctx context.Context) positionsData {
	open, err := h.deps.Source.OpenPositions(ctx)
	logRead(ctx, "positions", err)
	return positionsData{open: open, err: err}
}

// positions is the 保有ポジション card.
func positions(d positionsData, now time.Time) homepanels.PositionsView {
	if d.err != nil {
		return homepanels.PositionsView{Err: "保有ポジション" + errRead}
	}
	v := homepanels.PositionsView{Rows: make([]homepanels.PositionRow, 0, len(d.open))}
	for _, p := range d.open {
		v.Rows = append(v.Rows, homepanels.PositionRow{
			Symbol: p.Symbol, Side: p.Side, Quantity: p.Quantity,
			EntryPrice: p.EntryPrice, CurrentPrice: p.CurrentPrice, Unrealized: p.UnrealizedPnL,
			Held: max(now.Sub(p.OpenedAt), 0),
		})
	}
	return v
}

// pnl is the 今日の Paper 損益 card: what closed since 00:00 JST (the figure
// the Paper results MCP's paper_daily_pnl reports for today) plus the unrealized
// P&L of the open positions. The sum is what the daily guard measures against
// its loss limit (risk.DailyActivity.PnLJPY), so 損失上限までの残り is that limit
// plus the sum, as display arithmetic only.
func (h *Handler) pnl(ctx context.Context, in statusInputs, d positionsData) homepanels.PnLView {
	realized, err := h.deps.Source.RealizedToday(ctx, in.now)
	logRead(ctx, "realized pnl", err)
	if err != nil {
		return homepanels.PnLView{Err: "今日の損益" + errRead}
	}
	v := homepanels.PnLView{Realized: realized.PnLJPY, ClosedCount: realized.ClosedCount}
	if in.healthErr == nil {
		v.OpenedCount, v.OpenedKnown = in.health.OpenedToday, true
	}
	if d.err != nil {
		return v
	}
	for _, p := range d.open {
		v.Unrealized += p.UnrealizedPnL
	}
	v.UnrealizedKnown = true
	v.Total = v.Realized + v.Unrealized
	if h.deps.Settings != nil {
		resolved, err := h.deps.Settings.DailyGuard(ctx)
		logRead(ctx, "daily guard settings", err)
		if err == nil && resolved.Limits.LossLimitEnabled() {
			v.LossLimit = &homepanels.LossLimitView{LimitJPY: resolved.Limits.LossLimitJPY, RemainingJPY: resolved.Limits.LossLimitJPY + v.Total}
		}
	}
	return v
}

const rejectEntryQuality = "entry_quality_below_threshold"

// rejectReasons is the エントリーしなかった理由 block: today's NONE signals by
// reason, and, when entry quality is (one of) the most frequent reason(s), the
// minimum entry quality in use (Paper / Live) with the link to change it.
func (h *Handler) rejectReasons(ctx context.Context, now time.Time) homepanels.RejectReasonsView {
	counts, err := h.deps.Source.RejectReasons(ctx, now)
	logRead(ctx, "reject reasons", err)
	if err != nil {
		return homepanels.RejectReasonsView{Err: "見送り理由" + errRead}
	}
	v := homepanels.RejectReasonsView{Rows: make([]homepanels.RejectRow, 0, len(counts))}
	for _, c := range counts {
		v.Total += c.Count
		v.Rows = append(v.Rows, homepanels.RejectRow{Code: c.Reason, Label: labels.RejectReason(c.Reason), Count: c.Count})
	}
	// Rows are most frequent first, so the first row holds the maximum count.
	for _, row := range v.Rows {
		if row.Count < v.Rows[0].Count {
			break
		}
		if row.Code == rejectEntryQuality {
			v.EntryQuality = h.entryQuality(ctx)
			break
		}
	}
	return v
}

// entryQuality reads the minimum entry quality of both sides through the
// Settings screen's own status panel, so it shows what that panel shows.
func (h *Handler) entryQuality(ctx context.Context) *homepanels.EntryQualityNote {
	note := &homepanels.EntryQualityNote{Link: linkEntryQuality}
	if h.deps.Settings == nil {
		note.Err = "最低エントリー品質" + errRead
		return note
	}
	for _, side := range []struct{ group, name string }{{"policy-long", "ロング"}, {"policy-short", "ショート"}} {
		row := homepanels.EntryQualityRow{Side: side.name}
		for _, it := range entryqualitypanel.Load(ctx, h.deps.Settings, side.group) {
			switch it.Key {
			case "error":
				note.Err = "最低エントリー品質" + errRead
				return note
			case "mode":
				row.Mode = it.Value
			case "effective":
				row.Effective = it.Value
			case "paper":
				row.Paper = it.Value
			case "live":
				row.Live = it.Value
			}
		}
		note.Rows = append(note.Rows, row)
	}
	return note
}

// scan is the スキャン状況 block: the latest cycle's funnel.
func (h *Handler) scan(ctx context.Context) homepanels.ScanView {
	if h.deps.Scan == nil {
		return homepanels.ScanView{}
	}
	cycle, ok, err := h.deps.Scan.Scan(ctx)
	logRead(ctx, "scan cycle", err)
	if err != nil {
		return homepanels.ScanView{Err: "スキャン状況" + errRead}
	}
	if !ok {
		return homepanels.ScanView{}
	}
	return homepanels.ScanView{Panel: organisms.ScanPanelView{HasCycle: true, Cycle: cycle, Summary: cycle.Summary()}}
}

var proposalActionLabels = map[string]string{orderproposal.ActionOpenLong: "買い（新規）", orderproposal.ActionClose: "決済"}

// proposals is the AIの注文提案（今日） block.
func (h *Handler) proposals(ctx context.Context, now time.Time) homepanels.ProposalsView {
	p, err := h.deps.Source.Proposals(ctx, now)
	logRead(ctx, "order proposals", err)
	if err != nil {
		return homepanels.ProposalsView{Err: "AIの注文提案" + errRead}
	}
	v := homepanels.ProposalsView{Total: p.Total}
	for _, s := range p.ByStatus {
		v.Counts = append(v.Counts, homepanels.StatusCount{Label: pages.ProposalStatusLabel(s.Status), Count: s.Count})
	}
	for _, it := range p.Latest {
		action, ok := proposalActionLabels[it.Action]
		if !ok {
			action = it.Action
		}
		v.Latest = append(v.Latest, homepanels.ProposalRow{
			ID: it.ID, Symbol: it.Symbol, Action: action, Status: pages.ProposalStatusLabel(it.Status), ReceivedAt: it.ReceivedAt,
		})
	}
	return v
}

// trades is the 最近の取引 block: the newest closed trades.
func (h *Handler) trades(ctx context.Context, now time.Time) homepanels.TradesView {
	trades, err := h.deps.Source.RecentTrades(ctx, now, recentTradeLimit)
	logRead(ctx, "recent trades", err)
	if err != nil {
		return homepanels.TradesView{Err: "最近の取引" + errRead}
	}
	v := homepanels.TradesView{Rows: make([]homepanels.TradeRow, 0, len(trades))}
	for _, t := range trades {
		v.Rows = append(v.Rows, homepanels.TradeRow{
			Symbol: t.Symbol, Side: t.Side, Quantity: t.Quantity, ClosedAt: t.ClosedAt, PnL: t.PnLJPY, ExitReason: t.ExitReason,
		})
	}
	return v
}

// model is the モデル状態 block: the adopted meta-labeling model and a skipped
// retraining.
func model(in statusInputs) homepanels.ModelView {
	if in.healthErr != nil {
		return homepanels.ModelView{Err: "モデルの状態" + errRead}
	}
	h := in.health
	v := homepanels.ModelView{Adopted: h.MetaAdopted, Version: h.MetaVersion, TrainedAt: h.MetaTrainedAt}
	if h.RetrainSkipped {
		v.RetrainNotice = "夜間の再学習を見送りました"
		if h.RetrainSkipReason != "" {
			reason := labels.RetrainSkip(h.RetrainSkipReason)
			if reason == labels.Unknown {
				reason = "理由コードあり" // no brackets inside the brackets
			}
			v.RetrainNotice += "（" + reason + "）"
		}
	}
	return v
}

// killEvents is the 最近の Kill Switch block, read from the activity feed the
// same way `GET /api/v1/activity?type=kill_switch` does.
func (h *Handler) killEvents(ctx context.Context) homepanels.KillEventsView {
	if h.deps.Activity == nil {
		return homepanels.KillEventsView{}
	}
	snap, err := h.deps.Activity.Snapshot(ctx, activityfeed.Query{Type: domain.ActivityTypeKillSwitch, Limit: killEventLimit})
	logRead(ctx, "kill switch events", err)
	if err != nil {
		return homepanels.KillEventsView{Err: "Kill Switch の履歴" + errRead}
	}
	v := homepanels.KillEventsView{}
	for _, e := range snap.Events {
		if len(v.Rows) == killEventLimit {
			break
		}
		v.Rows = append(v.Rows, homepanels.KillEventRow{At: e.Timestamp, Detail: labels.ActivityDetail(domain.ActivityTypeKillSwitch, e.Detail)})
	}
	return v
}
