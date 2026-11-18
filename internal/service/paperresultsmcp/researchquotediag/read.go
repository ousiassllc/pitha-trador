package researchquotediag

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/researchquote"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// minuteLen is the length of "2026-10-08T00:30", the UTC minute of a stored
// timestamp (substr(timestamp, 1, 16)), the join key with the symbol.
const minuteLen = 16

// errCapped means a day's rows did not fit in the rows left: the day is left out.
var errCapped = errors.New("researchquotediag: row cap reached")

// dayData is one source's rows of one JST day, reduced to what the comparison
// needs.
type dayData struct {
	stats   SourceStats
	symbols map[string]struct{}
	minutes map[string]struct{}
}

func newDayData() *dayData {
	return &dayData{symbols: map[string]struct{}{}, minutes: map[string]struct{}{}}
}

func (d *dayData) add(symbol, minute string) {
	d.stats.Rows++
	d.symbols[symbol] = struct{}{}
	d.minutes[minute] = struct{}{}
}

func (d *dayData) finish() {
	d.stats.Symbols, d.stats.Minutes = len(d.symbols), len(d.minutes)
}

// tachibanaDay is the research samples of one day by "symbol|minute".
type tachibanaDay struct {
	dayData
	price map[string]float64
	phase map[string]string
}

// readTachibana reads the day's 立花 samples ([from, to), stored timestamps).
// limit is how many rows may be read; one more is errCapped.
func readTachibana(ctx context.Context, q query.Q, from, to, symbol string, limit int) (*tachibanaDay, error) {
	sqlText := `SELECT symbol, substr(timestamp, 1, 16), price, session_phase, via FROM research_quote_samples
		WHERE source = 'tachibana' AND timestamp >= ? AND timestamp < ?`
	args := []any{from, to}
	if symbol != "" {
		sqlText += ` AND symbol = ?`
		args = append(args, symbol)
	}
	rows, err := q.QueryContext(ctx, sqlText+` LIMIT ?`, append(args, limit+1)...)
	if err != nil {
		return nil, fmt.Errorf("researchquotediag: read samples: %w", err)
	}
	defer func() { _ = rows.Close() }()
	d := &tachibanaDay{dayData: *newDayData(), price: map[string]float64{}, phase: map[string]string{}}
	for rows.Next() {
		var sym, minute, phase, via string
		var price float64
		if err := rows.Scan(&sym, &minute, &price, &phase, &via); err != nil {
			return nil, fmt.Errorf("researchquotediag: scan sample: %w", err)
		}
		if d.stats.Rows == limit {
			return nil, errCapped
		}
		d.add(sym, minute)
		if via == researchquote.ViaREST {
			d.stats.ViaREST++
		} else {
			d.stats.ViaEvent++
		}
		key := sym + "|" + minute
		d.price[key], d.phase[key] = price, phase
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("researchquotediag: read samples: %w", err)
	}
	d.finish()
	return d, nil
}

// kabuDay is kabu's market_snapshots of one day: the last price per "symbol|minute".
type kabuDay struct {
	dayData
	price map[string]float64
}

// readKabu reads the day's market_snapshots (price only) in time order, so the
// last snapshot of a symbol-minute wins.
func readKabu(ctx context.Context, q query.Q, from, to, symbol string, limit int) (*kabuDay, error) {
	sqlText := `SELECT symbol, substr(timestamp, 1, 16), price FROM market_snapshots WHERE timestamp >= ? AND timestamp < ?`
	args := []any{from, to}
	if symbol != "" {
		sqlText += ` AND symbol = ?`
		args = append(args, symbol)
	}
	rows, err := q.QueryContext(ctx, sqlText+` ORDER BY timestamp, id LIMIT ?`, append(args, limit+1)...)
	if err != nil {
		return nil, fmt.Errorf("researchquotediag: read snapshots: %w", err)
	}
	defer func() { _ = rows.Close() }()
	d := &kabuDay{dayData: *newDayData(), price: map[string]float64{}}
	for rows.Next() {
		var sym, minute string
		var price float64
		if err := rows.Scan(&sym, &minute, &price); err != nil {
			return nil, fmt.Errorf("researchquotediag: scan snapshot: %w", err)
		}
		if d.stats.Rows == limit {
			return nil, errCapped
		}
		d.add(sym, minute)
		if price > 0 {
			d.price[sym+"|"+minute] = price
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("researchquotediag: read snapshots: %w", err)
	}
	d.finish()
	return d, nil
}

// compare pairs the two sources' symbol-minutes of one day into the day's
// comparison, and adds each group to byPhase. The phase of a minute with a 立花
// sample is the sample's; of one with kabu's snapshot only, that of its time.
func compare(t *tachibanaDay, k *kabuDay, byPhase map[string]*cmpAcc) *cmpAcc {
	day := &cmpAcc{}
	phase := func(key string) string {
		if p, ok := t.phase[key]; ok {
			return p
		}
		at, err := time.Parse("2006-01-02T15:04", key[len(key)-minuteLen:])
		if err != nil {
			return researchquote.PhaseClose
		}
		return researchquote.PhaseAt(at)
	}
	acc := func(key string) *cmpAcc {
		p := phase(key)
		if byPhase[p] == nil {
			byPhase[p] = &cmpAcc{}
		}
		return byPhase[p]
	}
	for key, tp := range t.price {
		kp, ok := k.price[key]
		p := acc(key)
		if !ok {
			day.onlyTachibana++
			p.onlyTachibana++
			continue
		}
		bps := (tp - kp) / kp * 1e4
		day.both++
		p.both++
		day.gaps.add(bps)
		p.gaps.add(bps)
	}
	for key := range k.price {
		if _, ok := t.price[key]; !ok {
			day.onlyKabu++
			acc(key).onlyKabu++
		}
	}
	return day
}
