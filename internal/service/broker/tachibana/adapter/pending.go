package adapter

import (
	"context"
	"errors"

	"github.com/ousiassllc/pitha-trador/internal/service/broker"
)

// errPending marks the data paths the follow-up issues add (#735 マスタ・時価,
// #737 EVENT).
var errPending = errors.New("tachibana: not available yet")

func (a *Adapter) Quote(context.Context, string) (broker.Quote, error) {
	return broker.Quote{}, errPending
}

func (a *Adapter) SymbolInfo(context.Context, string) (broker.SymbolInfo, error) {
	return broker.SymbolInfo{}, errPending
}

func (a *Adapter) SetWatch(context.Context, []string) error { return errPending }

func (a *Adapter) UseWatchlist() {}

func (a *Adapter) Latest(context.Context, string) (broker.Quote, error) {
	return broker.Quote{}, errPending
}
