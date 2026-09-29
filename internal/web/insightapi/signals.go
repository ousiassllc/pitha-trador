package insightapi

import (
	"context"
	"errors"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
)

// signalOutput mirrors one `GET /api/v1/signals` item: a trade_signals
// row. Score/EntryPriceReference/RejectReason/JevDecisionID are null
// when the row has none (docs/architecture/er.md §trade_signals).
type signalOutput struct {
	ID                  int64    `json:"id"`
	Symbol              string   `json:"symbol"`
	Timestamp           string   `json:"timestamp"`
	Direction           string   `json:"direction" enum:"LONG,SHORT,NONE"`
	Score               *float64 `json:"score"`
	EntryPriceReference *float64 `json:"entry_price_reference"`
	PolicyVersion       string   `json:"policy_version"`
	RiskPassed          bool     `json:"risk_passed"`
	RejectReason        *string  `json:"reject_reason"`
	JevDecisionID       *int64   `json:"jev_decision_id"`
}

func toSignalOutputs(signals []domain.TradeSignal) []signalOutput {
	items := make([]signalOutput, len(signals))
	for i, s := range signals {
		items[i] = signalOutput{
			ID: s.ID, Symbol: s.Symbol, Timestamp: s.Timestamp.Format(time.RFC3339),
			Direction: s.Direction, Score: s.Score, EntryPriceReference: s.EntryPriceReference,
			PolicyVersion: s.PolicyVersion, RiskPassed: s.RiskPassed, RejectReason: s.RejectReason,
			JevDecisionID: s.JevDecisionID,
		}
	}
	return items
}

// SignalsAPIOutput is the Huma response body for `GET /api/v1/signals`
// and `GET /api/v1/signals/{symbol}`.
type SignalsAPIOutput struct {
	Body struct {
		Items []signalOutput `json:"items"`
	}
}

// SignalsInput is `GET /api/v1/signals`'s query parameters.
type SignalsInput struct {
	Limit int `query:"limit" default:"100" minimum:"1" maximum:"500" doc:"Maximum number of signals to return (1-500)."`
}

// SymbolSignalsInput is `GET /api/v1/signals/{symbol}`'s path+query
// parameters.
type SymbolSignalsInput struct {
	Symbol string `path:"symbol" minLength:"1" maxLength:"16" pattern:"^[0-9A-Za-z]+$" doc:"Instrument symbol (alphanumeric, e.g. 7203)."`
	Limit  int    `query:"limit" default:"100" minimum:"1" maximum:"500" doc:"Maximum number of signals to return (1-500)."`
}

// Signals implements `GET /api/v1/signals` (docs/api/endpoints.md §5):
// trade_signals across every symbol (LONG/SHORT/NONE, whether or not Risk
// Engine passed them), most recent first.
func (h *Handler) Signals(ctx context.Context, in *SignalsInput) (*SignalsAPIOutput, error) {
	signals, err := h.provider.ListSignals(ctx, in.Limit)
	if err != nil {
		return nil, huma.Error500InternalServerError("list signals failed", err)
	}

	out := &SignalsAPIOutput{}
	out.Body.Items = toSignalOutputs(signals)
	return out, nil
}

// SymbolSignals implements `GET /api/v1/signals/{symbol}`
// (docs/api/endpoints.md §5): one symbol's trade_signals history, most
// recent first, including risk_passed/reject_reason.
func (h *Handler) SymbolSignals(ctx context.Context, in *SymbolSignalsInput) (*SignalsAPIOutput, error) {
	signals, err := h.provider.RecentSignals(ctx, in.Symbol, in.Limit)
	if err != nil {
		if errors.Is(err, execution.ErrInstrumentUnknown) {
			return nil, huma.Error404NotFound("unknown symbol")
		}
		return nil, huma.Error500InternalServerError("list signals failed", err)
	}

	out := &SignalsAPIOutput{}
	out.Body.Items = toSignalOutputs(signals)
	return out, nil
}
