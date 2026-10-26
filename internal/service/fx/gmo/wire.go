package gmo

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"
)

// envelope is GMO's common response frame. Errors carry "messages" instead
// of "data" (status 1, or 5 during maintenance).
type envelope struct {
	Status   int             `json:"status"`
	Data     json.RawMessage `json:"data"`
	Messages []Message       `json:"messages"`
}

type tickerWire struct {
	Symbol    string `json:"symbol"`
	Ask       string `json:"ask"`
	Bid       string `json:"bid"`
	Timestamp string `json:"timestamp"`
	Status    string `json:"status"`
}

type candleWire struct {
	OpenTime string `json:"openTime"`
	Open     string `json:"open"`
	High     string `json:"high"`
	Low      string `json:"low"`
	Close    string `json:"close"`
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidResponse, fmt.Sprintf(format, args...))
}

// price parses a GMO decimal string; zero, negative and non-finite values are
// not rates.
func price(field, s string) (float64, error) {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v <= 0 || math.IsInf(v, 0) || math.IsNaN(v) {
		return 0, invalid("%s %q is not a positive price", field, s)
	}
	return v, nil
}

func decodeTickers(data json.RawMessage) (map[string]Ticker, error) {
	var rows []tickerWire
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, invalid("ticker data: %v", err)
	}
	out := make(map[string]Ticker, len(rows))
	for _, r := range rows {
		bid, err := price(r.Symbol+" bid", r.Bid)
		if err != nil {
			return nil, err
		}
		ask, err := price(r.Symbol+" ask", r.Ask)
		if err != nil {
			return nil, err
		}
		ts, err := time.Parse(time.RFC3339Nano, r.Timestamp)
		if err != nil {
			return nil, invalid("%s timestamp %q", r.Symbol, r.Timestamp)
		}
		status := MarketStatus(r.Status)
		if status != MarketOpen && status != MarketClose {
			return nil, invalid("%s status %q", r.Symbol, r.Status)
		}
		out[r.Symbol] = Ticker{Symbol: r.Symbol, Bid: bid, Ask: ask, Timestamp: ts, Status: status}
	}
	return out, nil
}

func decodeKlines(data json.RawMessage) ([]Candle, error) {
	var rows []candleWire
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, invalid("klines data: %v", err)
	}
	out := make([]Candle, 0, len(rows))
	for _, r := range rows {
		ms, err := strconv.ParseInt(r.OpenTime, 10, 64)
		if err != nil {
			return nil, invalid("openTime %q", r.OpenTime)
		}
		c := Candle{OpenTime: time.UnixMilli(ms).UTC()}
		for _, f := range []struct {
			name, raw string
			dst       *float64
		}{{"open", r.Open, &c.Open}, {"high", r.High, &c.High}, {"low", r.Low, &c.Low}, {"close", r.Close, &c.Close}} {
			if *f.dst, err = price(f.name, f.raw); err != nil {
				return nil, err
			}
		}
		out = append(out, c)
	}
	return out, nil
}
