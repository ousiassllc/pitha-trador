// Package proposal is the order-proposal vocabulary of the Paper results MCP
// (issue #827): what a Paper order proposal looks like on the wire
// (order_proposal_submit), how its shape is validated and the canonical form
// its request hash is taken over (proposal.go, rules.go, hash.go), and the
// read model of one stored proposal (order_proposal_get, read.go).
//
// Only the shape is checked here. Whether the symbol exists, what the price is
// and whether Risk lets it through are the application's questions (issue
// #828); the MCP never looks at them. Writing is mcpset.Writer's job.
package proposal

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

// Values of the enumerated arguments (and of the order_proposals columns).
const (
	ActionOpenLong = "open_long"
	ActionClose    = "close"

	BucketStock   = "stock"
	BucketBondETF = "bond_etf"

	OrderMarket = "MARKET"
	OrderLimit  = "LIMIT"
)

// Statuses of a proposal. Only StatusReceived is written by the MCP; the rest
// are the application's (issue #828), listed here so the CHECK in migration
// 000042 and the code agree on the set.
const (
	StatusReceived       = "received"
	StatusProcessing     = "processing"
	StatusWaitingSession = "waiting_session"
	StatusRejected       = "rejected"
	StatusSubmitted      = "submitted"
	StatusFilled         = "filled"
	StatusExpired        = "expired"
	StatusCancelled      = "cancelled"
	StatusClosed         = "closed"
	StatusFailed         = "failed"
)

// Actors of an order_proposal_events row.
const (
	ActorMCP = "mcp"
	ActorApp = "app"
	ActorGUI = "gui"
)

// Limits fixed by the issue #827 specification.
const (
	// MaxPerDay is how many proposals are accepted per JST day.
	MaxPerDay = 20
	// MaxExpiry is how far ahead of the receipt time expires_at may be.
	MaxExpiry = 24 * time.Hour

	MinKeyLen         = 8
	MaxKeyLen         = 64
	MaxRationaleRunes = 2000
	MaxExitPlanRunes  = 500
	MinSources        = 1
	MaxSources        = 10
	MaxTitleRunes     = 200
	MaxURLRunes       = 2000
)

var (
	keyPattern    = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	symbolPattern = regexp.MustCompile(`^[0-9A-Z]{1,10}$`)
)

// Source is one entry of the information sources the proposal rests on.
type Source struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	RetrievedAt string `json:"retrieved_at"`
}

// Args are the arguments of order_proposal_submit as decoded from JSON
// (strictly: an unknown member is refused by the caller). There is no mode
// argument: every proposal is a Paper proposal.
type Args struct {
	IdempotencyKey  string   `json:"idempotency_key"`
	Symbol          string   `json:"symbol"`
	Bucket          string   `json:"bucket"`
	Action          string   `json:"action"`
	Quantity        *int64   `json:"quantity"`
	BudgetJPY       *float64 `json:"budget_jpy"`
	OrderType       string   `json:"order_type"`
	LimitPrice      *float64 `json:"limit_price"`
	StopLossPrice   *float64 `json:"stop_loss_price"`
	TakeProfitPrice *float64 `json:"take_profit_price"`
	ExitPlan        *string  `json:"exit_plan"`
	MaxLossJPY      *float64 `json:"max_loss_jpy"`
	Rationale       string   `json:"rationale"`
	Sources         []Source `json:"sources"`
	ExpiresAt       string   `json:"expires_at"`
}

// Request is a validated, normalized proposal: text trimmed, times in UTC
// RFC3339. Its Hash is what makes a re-sent proposal recognizable.
type Request struct {
	IdempotencyKey  string
	Symbol          string
	Bucket          string
	Action          string
	Quantity        *int64
	BudgetJPY       *float64
	OrderType       string
	LimitPrice      *float64
	StopLossPrice   *float64
	TakeProfitPrice *float64
	ExitPlan        *string
	MaxLossJPY      *float64
	Rationale       string
	Sources         []Source
	ExpiresAt       time.Time
}

// InvalidError is a validation failure. Reason names the argument and the rule
// and never quotes a value, so it is safe to show to the caller.
type InvalidError struct{ Reason string }

func (e *InvalidError) Error() string { return "proposal: " + e.Reason }

func invalid(format string, a ...any) error {
	return &InvalidError{Reason: fmt.Sprintf(format, a...)}
}

// IsInvalid reports whether err is an *InvalidError and returns its reason.
func IsInvalid(err error) (reason string, ok bool) {
	var ie *InvalidError
	if errors.As(err, &ie) {
		return ie.Reason, true
	}
	return "", false
}

// Validate checks the shape of the arguments and returns the normalized
// request. It needs no clock: whether expires_at is in the window is
// Request.CheckExpiry's question. The first rule that fails is the error.
func (a Args) Validate() (Request, error) {
	r := Request{
		IdempotencyKey: a.IdempotencyKey, Symbol: a.Symbol, Bucket: a.Bucket, Action: a.Action,
		Quantity: a.Quantity, BudgetJPY: a.BudgetJPY, OrderType: a.OrderType, LimitPrice: a.LimitPrice,
		StopLossPrice: a.StopLossPrice, TakeProfitPrice: a.TakeProfitPrice, MaxLossJPY: a.MaxLossJPY,
	}
	if n := len(a.IdempotencyKey); n < MinKeyLen || n > MaxKeyLen || !keyPattern.MatchString(a.IdempotencyKey) {
		return Request{}, invalid("idempotency_key は %d〜%d 文字の英数字・_・- で指定してください", MinKeyLen, MaxKeyLen)
	}
	if !symbolPattern.MatchString(a.Symbol) {
		return Request{}, invalid("symbol は銘柄コード（英大文字と数字、10 文字以内）で指定してください")
	}
	if a.Bucket != BucketStock && a.Bucket != BucketBondETF {
		return Request{}, invalid("bucket は %s か %s を指定してください", BucketStock, BucketBondETF)
	}
	if a.Action != ActionOpenLong && a.Action != ActionClose {
		return Request{}, invalid("action は %s か %s を指定してください", ActionOpenLong, ActionClose)
	}
	if a.OrderType != OrderMarket && a.OrderType != OrderLimit {
		return Request{}, invalid("order_type は %s か %s を指定してください", OrderMarket, OrderLimit)
	}
	if err := checkNumbers(a); err != nil {
		return Request{}, err
	}
	if err := checkOrderShape(a); err != nil {
		return Request{}, err
	}
	var err error
	if r.Rationale, err = text("rationale", a.Rationale, MaxRationaleRunes, true); err != nil {
		return Request{}, err
	}
	if a.ExitPlan != nil {
		plan, err := text("exit_plan", *a.ExitPlan, MaxExitPlanRunes, a.Action == ActionOpenLong)
		if err != nil {
			return Request{}, err
		}
		r.ExitPlan = &plan
	} else if a.Action == ActionOpenLong {
		return Request{}, invalid("exit_plan は open_long のとき必須です")
	}
	if r.Sources, err = sources(a.Sources); err != nil {
		return Request{}, err
	}
	t, err := time.Parse(time.RFC3339, a.ExpiresAt)
	if err != nil {
		return Request{}, invalid("expires_at は RFC3339 の時刻で指定してください")
	}
	r.ExpiresAt = t.UTC()
	return r, nil
}
