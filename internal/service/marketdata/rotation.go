package marketdata

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
)

const (
	// MaxRegisteredSymbols is kabuステーションAPI's cap on the API登録銘柄
	// リスト. It is shared by PUSH (/register) and REST: every symbol
	// requested through an information API (/board, /symbol) is registered
	// automatically (kabu_STATION_API.yaml, tag "info"), so polling more than
	// 50 distinct symbols fails with 4002006 unless slots are freed.
	MaxRegisteredSymbols = 50
	// RestRotationSlots is how many of those slots PUSH registration leaves
	// free for REST polling, which frees them again in batches (see
	// withRestSlot).
	RestRotationSlots = 10
)

const (
	codeRegisterLimit    = 4002006 // レジスト数エラー: API登録銘柄リストが上限
	codeUnregisterFailed = 4001020 // 銘柄が解除できませんでした (not registered)
	codeUnregisterSome   = 4001021 // 一部の銘柄が解除できませんでした
)

// errNoRestSlots reports a full registration list with no REST-registered
// symbol of ours to release (the slots are held by PUSH or by the operator).
var errNoRestSlots = errors.New("marketdata: registration list is full and holds no REST-registered symbol to release")

func isRegisterLimit(err error) bool {
	var api *APIError
	return errors.As(err, &api) && api.Code == codeRegisterLimit
}

// withRestSlot runs call, an information-API request for sym that kabu
// station registers as a side effect. When the registration list is full
// (4002006) it unregisters every symbol this Client registered that way in
// one request, then retries call once. One /unregister thus buys up to
// RestRotationSlots further symbols, so the extra call costs a fraction of
// the REST budget instead of one call per symbol.
func (c *Client) withRestSlot(ctx context.Context, sym RegisterSymbol, call func() error) error {
	err := call()
	if isRegisterLimit(err) {
		if relErr := c.releaseRestSlots(ctx); relErr != nil {
			slog.Error("marketdata: registration list is full and could not be freed", "symbol", sym.Symbol, "error", relErr)
			return err
		}
		err = call()
	}
	if err == nil {
		c.noteRestRegistered(sym)
	}
	return err
}

func (c *Client) noteRestRegistered(sym RegisterSymbol) {
	c.regMu.Lock()
	defer c.regMu.Unlock()
	if _, pinned := c.pinned[sym]; pinned {
		return
	}
	if c.transient == nil {
		c.transient = make(map[RegisterSymbol]struct{})
	}
	c.transient[sym] = struct{}{}
}

// releaseRestSlots unregisters the REST-registered symbols. kabu answers
// 4001020/4001021 when some are already gone (e.g. /register replaced the
// list); the slots are free then too, so those count as released.
func (c *Client) releaseRestSlots(ctx context.Context) error {
	c.regMu.Lock()
	syms := make([]RegisterSymbol, 0, len(c.transient))
	for s := range c.transient {
		syms = append(syms, s)
	}
	c.regMu.Unlock()
	if len(syms) == 0 {
		return errNoRestSlots
	}

	token, ok := c.Token()
	if !ok {
		return ErrNoToken
	}
	err := c.doInfo(ctx, http.MethodPut, "/unregister", token, registerRequest{Symbols: syms}, nil)
	var api *APIError
	alreadyGone := errors.As(err, &api) && (api.Code == codeUnregisterFailed || api.Code == codeUnregisterSome)
	if err != nil && !alreadyGone {
		return err
	}
	c.regMu.Lock()
	for _, s := range syms {
		delete(c.transient, s)
	}
	c.regMu.Unlock()
	return nil
}

// UnregisterAll empties the API登録銘柄リスト (PUT /unregister/all). Startup
// calls it before /register so registrations left by a previous run (kabu
// station keeps them across app restarts) cannot fill the 50 slots.
func (c *Client) UnregisterAll(ctx context.Context) error {
	token, ok := c.Token()
	if !ok {
		return ErrNoToken
	}
	if err := c.doInfo(ctx, http.MethodPut, "/unregister/all", token, nil, nil); err != nil {
		return err
	}
	c.regMu.Lock()
	c.pinned, c.transient = nil, nil
	c.regMu.Unlock()
	return nil
}

// notePinned records symbols registered for PUSH: they stay registered and
// are never released by withRestSlot.
func (c *Client) notePinned(symbols []RegisterSymbol) {
	c.regMu.Lock()
	defer c.regMu.Unlock()
	if c.pinned == nil {
		c.pinned = make(map[RegisterSymbol]struct{}, len(symbols))
	}
	for _, s := range symbols {
		c.pinned[s] = struct{}{}
		delete(c.transient, s)
	}
}
