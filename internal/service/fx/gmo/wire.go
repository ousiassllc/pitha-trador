package gmo

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"time"
)

// envelope is GMO's common response frame. Errors carry "messages" instead
// of "data" (non-zero status).
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

// decodeTickers parses the all-pairs ticker response, keeping only the symbols
// in want. Rows of other pairs are ignored without validation, so bad data for
// a pair nobody asked for cannot break the configured ones.
func decodeTickers(data json.RawMessage, want []string) (map[string]Ticker, error) {
	var rows []json.RawMessage
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, invalid("ticker data: %v", err)
	}
	out := make(map[string]Ticker, len(want))
	for _, raw := range rows {
		var head struct {
			Symbol string `json:"symbol"`
		}
		if json.Unmarshal(raw, &head) != nil || !slices.Contains(want, head.Symbol) {
			continue
		}
		t, err := decodeTicker(raw)
		if err != nil {
			return nil, err
		}
		out[t.Symbol] = t
	}
	return out, nil
}

func decodeTicker(raw json.RawMessage) (Ticker, error) {
	var r tickerWire
	if err := json.Unmarshal(raw, &r); err != nil {
		return Ticker{}, invalid("ticker row: %v", err)
	}
	bid, err := price(r.Symbol+" bid", r.Bid)
	if err != nil {
		return Ticker{}, err
	}
	ask, err := price(r.Symbol+" ask", r.Ask)
	if err != nil {
		return Ticker{}, err
	}
	ts, err := time.Parse(time.RFC3339Nano, r.Timestamp)
	if err != nil {
		return Ticker{}, invalid("%s timestamp %q", r.Symbol, r.Timestamp)
	}
	status := MarketStatus(r.Status)
	if status != MarketOpen && status != MarketClose {
		return Ticker{}, invalid("%s status %q", r.Symbol, r.Status)
	}
	return Ticker{Symbol: r.Symbol, Bid: bid, Ask: ask, Timestamp: ts, Status: status}, nil
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
