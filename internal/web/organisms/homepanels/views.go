package homepanels

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
)

// Link is a 「どこを見るか」 link: where the operator looks or changes
// something for the condition next to it. The zero value is no link.
type Link struct {
	Href  string
	Label string
}

// TradeStatusView is the 取引の状態 card (issue #896 §5.1). Sentence is the
// whole one-line verdict ("新規エントリー: 可能" / "新規エントリー: 停止中 — …");
// Detail and Link explain it, Notice says part of the state could not be read.
// A non-empty Err replaces the card body: the read it depends on failed.
// State feeds the existing pitha-kill-switch-panel.
type TradeStatusView struct {
	Err      string
	State    domain.SystemState
	Allowed  bool
	Sentence string
	Detail   string
	Link     Link
	Notice   string
}

// PnLView is the 今日の Paper 損益 card. Realized is what closed since 00:00
// JST (the paper_daily_pnl figure of today), Unrealized the sum over the open
// positions; Total is their sum and only meaningful with UnrealizedKnown.
type PnLView struct {
	Err             string
	Realized        float64
	ClosedCount     int
	OpenedCount     int
	OpenedKnown     bool
	Unrealized      float64
	UnrealizedKnown bool
	Total           float64
	// LossLimit is the room left before the daily loss limit halts new entries
	// (display arithmetic only); nil while the limit is off or unreadable.
	LossLimit *LossLimitView
}

// LossLimitView is 日次ガード（1日の損失上限）までの残り.
type LossLimitView struct {
	LimitJPY     float64
	RemainingJPY float64
}

// PositionRow is one open position of the 保有ポジション table.
type PositionRow struct {
	Symbol       string
	Side         string
	Quantity     int64
	EntryPrice   float64
	CurrentPrice float64
	Unrealized   float64
	Held         time.Duration
}

// PositionsView is the 保有ポジション card.
type PositionsView struct {
	Err  string
	Rows []PositionRow
}

// AttentionLevel picks an item's colour.
type AttentionLevel string

// Attention levels: Alert (red) stops or endangers trading, Warn (amber)
// needs a look, Info (blue) is a notice.
const (
	LevelAlert AttentionLevel = "alert"
	LevelWarn  AttentionLevel = "warn"
	LevelInfo  AttentionLevel = "info"
)

// AttentionItem is one thing that needs the operator. Key is its stable id
// (the attention code or banner name), exposed as `data-key`.
type AttentionItem struct {
	Key    string
	Level  AttentionLevel
	Title  string
	Detail string
	Link   Link
}

// AttentionView is the 要対応 section. Notice says some sources could not be
// read, so an empty list is not proof that nothing is wrong.
type AttentionView struct {
	Items  []AttentionItem
	Notice string
}

// RejectRow is one reject_reason head of today's NONE signals.
type RejectRow struct {
	Code  string
	Label string
	Count int
}

// EntryQualityRow is the minimum entry quality of one side (ロング / ショート):
// Effective is the value the Policy Engine uses in the running mode (Mode),
// next to the Paper and the Live setting.
type EntryQualityRow struct {
	Side      string
	Mode      string // "Paper" / "Live"
	Effective string
	Paper     string
	Live      string
}

// EntryQualityNote is the minimum entry quality in use, shown when
// entry_quality_below_threshold is the top reason. Err says it could not be read.
type EntryQualityNote struct {
	Err  string
	Rows []EntryQualityRow
	Link Link
}

// RejectReasonsView is the エントリーしなかった理由 block.
type RejectReasonsView struct {
	Err          string
	Total        int
	Rows         []RejectRow
	EntryQuality *EntryQualityNote
}

// ScanView is the スキャン状況 block: the latest cycle's funnel.
type ScanView struct {
	Err   string
	Panel organisms.ScanPanelView
}

// StatusCount is how many order proposals of one status arrived today.
type StatusCount struct {
	Label string
	Count int
}

// ProposalRow is one of the newest order proposals of today.
type ProposalRow struct {
	ID         int64
	Symbol     string
	Action     string
	Status     string
	ReceivedAt time.Time
}

// ProposalsView is the AIの注文提案（今日） block.
type ProposalsView struct {
	Err    string
	Total  int
	Counts []StatusCount
	Latest []ProposalRow
}

// TradeRow is one of the latest closed trades.
type TradeRow struct {
	Symbol     string
	Side       string
	Quantity   int64
	ClosedAt   time.Time
	PnL        float64
	ExitReason string
}

// TradesView is the 最近の取引 block.
type TradesView struct {
	Err  string
	Rows []TradeRow
}

// ModelView is the モデル状態 block.
type ModelView struct {
	Err           string
	Adopted       bool
	Version       string
	TrainedAt     time.Time // zero when unknown
	RetrainNotice string    // non-empty while the newest retraining was skipped
}

// KillEventRow is one of the latest Kill Switch events.
type KillEventRow struct {
	At     time.Time
	Detail string
}

// KillEventsView is the 最近の Kill Switch block.
type KillEventsView struct {
	Err  string
	Rows []KillEventRow
}

// View is everything the Home screen shows; each card carries its own Err, so
// one failed read never takes the screen down.
type View struct {
	TradeStatus   TradeStatusView
	PnL           PnLView
	Positions     PositionsView
	Attention     AttentionView
	RejectReasons RejectReasonsView
	Scan          ScanView
	Proposals     ProposalsView
	Trades        TradesView
	Model         ModelView
	KillEvents    KillEventsView
}
