package jquants

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain/jquants"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// Dataset names of the ledger (research_jquants_fetch_log.dataset).
const (
	datasetSymbols = jquants.DatasetSymbols
	datasetDaily   = jquants.DatasetDaily
	datasetMinute  = jquants.DatasetMinute
)

type rawSymbol struct {
	Date     string `json:"Date"`
	Code     string `json:"Code"`
	CoName   string `json:"CoName"`
	CoNameEn string `json:"CoNameEn"`
	S17      string `json:"S17"`
	S33      string `json:"S33"`
	ScaleCat string `json:"ScaleCat"`
	Mkt      string `json:"Mkt"`
	MktNm    string `json:"MktNm"`
}

type rawDaily struct {
	Date      string   `json:"Date"`
	Code      string   `json:"Code"`
	O         *float64 `json:"O"`
	H         *float64 `json:"H"`
	L         *float64 `json:"L"`
	C         *float64 `json:"C"`
	UL        string   `json:"UL"`
	LL        string   `json:"LL"`
	Vo        *float64 `json:"Vo"`
	Va        *float64 `json:"Va"`
	AdjFactor *float64 `json:"AdjFactor"`
	AdjO      *float64 `json:"AdjO"`
	AdjH      *float64 `json:"AdjH"`
	AdjL      *float64 `json:"AdjL"`
	AdjC      *float64 `json:"AdjC"`
	AdjVo     *float64 `json:"AdjVo"`
}

type rawMinute struct {
	Date string   `json:"Date"`
	Time string   `json:"Time"`
	Code string   `json:"Code"`
	O    *float64 `json:"O"`
	H    *float64 `json:"H"`
	L    *float64 `json:"L"`
	C    *float64 `json:"C"`
	Vo   *float64 `json:"Vo"`
	Va   *float64 `json:"Va"`
}

// Symbols returns the listed issues as of the JST day (/equities/master).
func (c *Client) Symbols(ctx context.Context, day string) ([]jquants.Symbol, error) {
	raws, err := c.fetch(ctx, datasetSymbols, "/equities/master", url.Values{"date": {day}})
	if err != nil {
		return nil, err
	}
	out := make([]jquants.Symbol, 0, len(raws))
	for _, r := range raws {
		var v rawSymbol
		if err := json.Unmarshal(r, &v); err != nil {
			return nil, c.decodeFailed(ctx, datasetSymbols)
		}
		if v.Code == "" {
			continue
		}
		out = append(out, jquants.Symbol{AsOf: day, Code: v.Code, Symbol: jquants.SymbolOf(v.Code), CompanyName: v.CoName, CompanyNameEn: v.CoNameEn,
			Sector17Code: v.S17, Sector33Code: v.S33, ScaleCategory: v.ScaleCat, MarketCode: v.Mkt, MarketName: v.MktNm})
	}
	return out, nil
}

// DailyBars returns the daily bars of every listed issue on the JST day
// (/equities/bars/daily?date=). A day without trading returns none.
func (c *Client) DailyBars(ctx context.Context, day string) ([]jquants.DailyBar, error) {
	raws, err := c.fetch(ctx, datasetDaily, "/equities/bars/daily", url.Values{"date": {day}})
	if err != nil {
		return nil, err
	}
	out := make([]jquants.DailyBar, 0, len(raws))
	for _, r := range raws {
		var v rawDaily
		if err := json.Unmarshal(r, &v); err != nil {
			return nil, c.decodeFailed(ctx, datasetDaily)
		}
		if v.Code == "" {
			continue
		}
		d := v.Date
		if d == "" {
			d = day
		}
		out = append(out, jquants.DailyBar{Day: d, Code: v.Code, Symbol: jquants.SymbolOf(v.Code),
			Open: v.O, High: v.H, Low: v.L, Close: v.C, Volume: v.Vo, Turnover: v.Va,
			AdjFactor: v.AdjFactor, AdjOpen: v.AdjO, AdjHigh: v.AdjH, AdjLow: v.AdjL, AdjClose: v.AdjC, AdjVolume: v.AdjVo,
			UpperLimit: v.UL, LowerLimit: v.LL})
	}
	return out, nil
}

// MinuteBars returns the minute bars of one issue (a 4- or 5-character code) on
// the JST day (/equities/bars/minute?code=&date=). A row without a positive
// price or a parsable time is dropped (and counted in the log).
func (c *Client) MinuteBars(ctx context.Context, day, code string) ([]jquants.MinuteBar, error) {
	code = jquants.CodeOf(code)
	raws, err := c.fetch(ctx, datasetMinute, "/equities/bars/minute", url.Values{"code": {code}, "date": {day}})
	if err != nil {
		return nil, err
	}
	out := make([]jquants.MinuteBar, 0, len(raws))
	dropped := 0
	for _, r := range raws {
		var v rawMinute
		if err := json.Unmarshal(r, &v); err != nil {
			return nil, c.decodeFailed(ctx, datasetMinute)
		}
		d := v.Date
		if d == "" {
			d = day
		}
		at, err := time.ParseInLocation("2006-01-02 15:04", d+" "+v.Time, marketcalendar.JST)
		if err != nil || !positive(v.O, v.H, v.L, v.C) || v.Vo == nil || v.Va == nil || *v.Vo < 0 || *v.Va < 0 {
			dropped++
			continue
		}
		bar := jquants.MinuteBar{Day: d, Code: code, Symbol: jquants.SymbolOf(code), At: at.UTC(),
			Open: *v.O, High: *v.H, Low: *v.L, Close: *v.C, Volume: *v.Vo, Turnover: *v.Va}
		if v.Code != "" {
			bar.Code, bar.Symbol = v.Code, jquants.SymbolOf(v.Code)
		}
		out = append(out, bar)
	}
	if dropped > 0 {
		slog.WarnContext(ctx, "jquants: minute rows dropped as malformed", "day", day, "symbol", jquants.SymbolOf(code), "dropped", dropped)
	}
	return out, nil
}

func positive(vs ...*float64) bool {
	for _, v := range vs {
		if v == nil || *v <= 0 {
			return false
		}
	}
	return true
}

func (c *Client) decodeFailed(ctx context.Context, dataset string) error {
	c.ledger.Failed(ctx, dataset, CodeDecode)
	return &HTTPError{Status: http.StatusOK, Code: CodeDecode}
}
