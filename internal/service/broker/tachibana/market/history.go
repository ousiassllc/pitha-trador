package market

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
)

const clmPriceHistory = "CLMMfdsGetMarketPriceHistory"

// FetchHistory fetches symbol's 日足 (CLMMfdsGetMarketPriceHistory: one
// symbol per request, the whole history of up to about 20 years, oldest
// first) at the history priority, which never leaves the queue between 8:00
// and 15:30 JST. A symbol the broker has no history for (delisted) answers
// normally with no list: that is an empty result, not an error.
//
// Each bar carries the raw values (pDOP...pDV) and the 株式分割換算係数
// adjusted ones (pDOPxK...pDVxK; the raw values where the broker sends none).
// A day without a usable 4本値 is dropped. The broker does not report 売買代金.
func FetchHistory(ctx context.Context, client *tachibana.Client, symbol string) ([]domain.DailyBar, error) {
	var resp struct {
		Rows []row `json:"aCLMMfdsMarketPriceHistory"`
	}
	fields := map[string]string{"sIssueCode": symbol, "sSizyouC": marketTSE}
	if err := client.Call(ctx, tachibana.TargetPrice, tachibana.PriorityHistory, clmPriceHistory, fields, &resp); err != nil {
		return nil, err
	}
	bars := make([]domain.DailyBar, 0, len(resp.Rows))
	for _, r := range resp.Rows {
		if b, ok := toDailyBar(symbol, r); ok {
			bars = append(bars, b)
		}
	}
	sort.SliceStable(bars, func(i, j int) bool { return bars[i].TradeDate < bars[j].TradeDate })
	return bars, nil
}

// toDailyBar translates one history row; ok is false without a valid sDate
// (YYYYMMDD) or a positive open/high/low/close.
func toDailyBar(symbol string, r row) (domain.DailyBar, bool) {
	day, err := time.Parse("20060102", r.text("sDate"))
	if err != nil {
		return domain.DailyBar{}, false
	}
	open, high, low, closing := r.num("pDOP"), r.num("pDHP"), r.num("pDLP"), r.num("pDPP")
	if open == nil || high == nil || low == nil || closing == nil ||
		*open <= 0 || *high <= 0 || *low <= 0 || *closing <= 0 {
		return domain.DailyBar{}, false
	}
	volume := numOr(r.num("pDV"), 0)
	return domain.DailyBar{
		Symbol: symbol, TradeDate: day.Format("2006-01-02"),
		Open: *open, High: *high, Low: *low, Close: *closing, Volume: volume,
		AdjOpen:   numOr(r.num("pDOPxK"), *open),
		AdjHigh:   numOr(r.num("pDHPxK"), *high),
		AdjLow:    numOr(r.num("pDLPxK"), *low),
		AdjClose:  numOr(r.num("pDPPxK"), *closing),
		AdjVolume: numOr(r.num("pDVxK"), volume),
	}, true
}

func numOr(f *float64, fallback float64) float64 {
	if f == nil {
		return fallback
	}
	return *f
}

// segmentOf maps the master's 上場区分 (sZyouzyouKubun) to a 市場区分 of the
// nightly universe (tachibanasource.TachibanaMarkets): 01/03 プライム,
// 02/04 スタンダード, 09/11 グロース; everything else (ETF・REIT・新興・プロ向け
// 等) is "other".
func segmentOf(listingKind string) string {
	switch strings.TrimSpace(listingKind) {
	case "01", "03":
		return tachibanasource.MarketPrime
	case "02", "04":
		return tachibanasource.MarketStandard
	case "09", "11":
		return tachibanasource.MarketGrowth
	}
	return tachibanasource.MarketOther
}
