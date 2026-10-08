package adapter

import (
	"context"

	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/market"
)

// Quote implements broker.QuoteSource: one 時価 request for symbol at the
// watch-quote priority. Callers that need several symbols should fetch them
// together through Quotes (one request, up to 120 symbols).
func (a *Adapter) Quote(ctx context.Context, symbol string) (broker.Quote, error) {
	return market.FetchQuote(ctx, a.client, tachibana.PriorityWatchQuote, symbol)
}

// Quotes fetches 時価 for many symbols in as few requests as possible (120
// per request). Symbols without data are absent from the result.
func (a *Adapter) Quotes(ctx context.Context, prio tachibana.Priority, symbols []string) (map[string]broker.Quote, error) {
	return market.FetchQuotes(ctx, a.client, prio, symbols)
}

// SymbolInfo implements broker.SymbolInfoSource from today's market master
// (fetched once in the morning, after the login).
func (a *Adapter) SymbolInfo(_ context.Context, symbol string) (broker.SymbolInfo, error) {
	return a.master.Info(symbol)
}

// Issues fetches the 全銘柄マスタ (for the universe import to come).
func (a *Adapter) Issues(ctx context.Context) ([]market.Issue, error) {
	return market.FetchIssues(ctx, a.client)
}
