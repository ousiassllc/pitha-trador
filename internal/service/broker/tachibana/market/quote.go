package market

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
)

const clmMarketPrice = "CLMMfdsGetMarketPrice"

// MaxSymbolsPerRequest is how many symbols one 時価 request may name (the
// broker ignores the rest).
const MaxSymbolsPerRequest = 120

// depthLevels is the number of 板 levels asked for (GAP/GAV, GBP/GBV 1..10).
// Whether the REQUEST I/F fills them is for the live check (#725): a missing
// level is simply missing (FR-FE-2).
const depthLevels = 10

// 特別気配 codes of QAS/QBS: 0102 (特別気配) and 0108 (停止前特別気配).
const (
	quoteKindSpecial        = "0102"
	quoteKindSpecialPreStop = "0108"
)

// columns is sTargetColumn: the 情報コード（型＋コード） of the EVENT I/F's FD
// notification (EVENT I/F 利用方法・データ仕様 §3(3)).
var columns = func() string {
	cols := []string{"pDPP", "tDPP:T", "pDOP", "pDHP", "pDLP", "pDV", "pDJ", "pVWAP", "pPRP",
		"pQAP", "pQAS", "pQBP", "pQBS", "pAV", "pBV"}
	for _, side := range []string{"GAP", "GAV", "GBP", "GBV"} {
		for i := 1; i <= depthLevels; i++ {
			cols = append(cols, "p"+side+strconv.Itoa(i))
		}
	}
	return strings.Join(cols, ",")
}()

// FetchQuotes fetches 時価 for symbols (duplicates and blanks dropped) in
// requests of at most MaxSymbolsPerRequest, at priority prio, and returns the
// quotes keyed by symbol. A symbol the broker answers with no row or no
// 現在値 is absent from the map: that is a per-symbol outcome (the caller
// reports tachibana.ErrNoData), never a feed failure. On a request error the
// quotes of the chunks that did succeed are returned together with the error.
func FetchQuotes(ctx context.Context, client *tachibana.Client, prio tachibana.Priority, symbols []string) (map[string]broker.Quote, error) {
	out := make(map[string]broker.Quote, len(symbols))
	for _, chunk := range chunks(dedupe(symbols), MaxSymbolsPerRequest) {
		var resp struct {
			Rows []row `json:"aCLMMfdsMarketPrice"`
		}
		fields := map[string]string{"sTargetIssueCode": strings.Join(chunk, ","), "sTargetColumn": columns}
		if err := client.Call(ctx, tachibana.TargetPrice, prio, clmMarketPrice, fields, &resp); err != nil {
			return out, err
		}
		for _, r := range resp.Rows {
			if q, ok := toQuote(r); ok {
				out[q.Symbol] = q
			}
		}
	}
	return out, nil
}

// FetchQuote is FetchQuotes for one symbol: tachibana.ErrNoData when the
// broker has no 現在値 for it.
func FetchQuote(ctx context.Context, client *tachibana.Client, prio tachibana.Priority, symbol string) (broker.Quote, error) {
	quotes, err := FetchQuotes(ctx, client, prio, []string{symbol})
	if err != nil {
		return broker.Quote{}, err
	}
	q, ok := quotes[symbol]
	if !ok {
		return broker.Quote{}, fmt.Errorf("%w: no price for %s", tachibana.ErrNoData, symbol)
	}
	return q, nil
}

// toQuote translates one 時価 row into the neutral Quote. Unlike kabu, no
// Bid/Ask swap is needed: QBP is the best buy quote (Bid) and QAP the best
// sell quote (Ask). Raw is the row itself, which becomes raw_data_json. ok is
// false for a row without a symbol or without a positive 現在値.
func toQuote(r row) (broker.Quote, bool) {
	symbol := r.text("sIssueCode")
	price := r.num("pDPP")
	if symbol == "" || price == nil || *price <= 0 {
		return broker.Quote{}, false
	}
	q := broker.Quote{
		Symbol: symbol,
		Price:  *price,
		High:   r.num("pDHP"),
		Low:    r.num("pDLP"),
		Bid:    r.num("pQBP"), BidQty: r.num("pBV"),
		Ask: r.num("pQAP"), AskQty: r.num("pAV"),
		SpecialQuote: isSpecial(r.text("pQAS")) || isSpecial(r.text("pQBS")),
		Raw:          r,
	}
	if v := r.num("pVWAP"); v != nil {
		q.VWAP = *v
	}
	if v := r.num("pDV"); v != nil {
		q.Volume = *v
	}
	if v := r.num("pDJ"); v != nil {
		q.Turnover = *v
	}
	q.BidDepth = depth(r, "pGBV")
	q.AskDepth = depth(r, "pGAV")
	return q, true
}

func isSpecial(kind string) bool {
	return kind == quoteKindSpecial || kind == quoteKindSpecialPreStop
}

// depth is the total quantity over the reported levels of prefix+1..10, nil
// when the broker reported none.
func depth(r row, prefix string) *float64 {
	var total float64
	found := false
	for i := 1; i <= depthLevels; i++ {
		if v := r.num(prefix + strconv.Itoa(i)); v != nil {
			total += *v
			found = true
		}
	}
	if !found {
		return nil
	}
	return &total
}

func dedupe(symbols []string) []string {
	seen := make(map[string]struct{}, len(symbols))
	out := make([]string, 0, len(symbols))
	for _, s := range symbols {
		s = strings.TrimSpace(s)
		if _, dup := seen[s]; s == "" || dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func chunks(symbols []string, size int) [][]string {
	var out [][]string
	for len(symbols) > size {
		out = append(out, symbols[:size])
		symbols = symbols[size:]
	}
	if len(symbols) > 0 {
		out = append(out, symbols)
	}
	return out
}
