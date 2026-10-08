// Package market reads the 立花 e支店 master and 時価 (issue #735): the
// morning 銘柄市場マスタ behind broker.SymbolInfoSource, the 全銘柄マスタ, and
// 時価 snapshots (CLMMfdsGetMarketPrice, up to 120 symbols per request)
// translated into the broker-neutral broker.Quote. Every request goes through
// the tachibana.Client's serial queue, so it obeys the one-in-flight rule, the
// per-second budget and the priorities.
package market

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// value is one field of a response row. The broker sends strings, but a bare
// number or null must not break the decode.
type value string

func (v *value) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	switch {
	case s == "null":
		*v = ""
	case strings.HasPrefix(s, `"`):
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		*v = value(str)
	default:
		*v = value(s)
	}
	return nil
}

// row is one element of a response list: item name to value.
type row map[string]value

// text is the trimmed value of key ("" when absent).
func (r row) text(key string) string { return strings.TrimSpace(string(r[key])) }

// num is the numeric value of key, nil when absent, empty or not a finite number.
func (r row) num(key string) *float64 {
	s := r.text(key)
	if s == "" {
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return nil
	}
	return &f
}
