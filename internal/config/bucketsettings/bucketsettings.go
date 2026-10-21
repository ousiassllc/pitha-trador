// Package bucketsettings holds the Paper per-asset-class budget (予算枠)
// settings (issue #824, parent #823): how much of risk.paper.initial_capital_jpy
// each of the two buckets, stock and bond_etf, may hold.
// They live in runtime_settings (docs/architecture/er.md §runtime_settings)
// under Keys, outside config.RiskLimits like papersettings: a bucket budget is
// not a limit of config/risk.yaml and the Live Risk Engine never reads it
// (internal/bootstrap/risk.go wires it for Paper only), so Live keeps one
// whole-account bucket.
//
// A budget is a whole number of yen, 0 meaning "this bucket is not used". The
// budgets together may not exceed the operating capital (CheckTotal); every
// writer of a key or of the capital applies that one check.
package bucketsettings

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strconv"

	"github.com/ousiassllc/pitha-trador/internal/config/papersettings"
)

// runtime_settings keys; every value is a JSON number (a whole number of yen).
const (
	KeyStockBudgetJPY   = "risk.bucket.stock.budget_jpy"
	KeyBondETFBudgetJPY = "risk.bucket.bond_etf.budget_jpy"
)

// MaxBudgetJPY is the largest budget Apply accepts; it equals the largest
// operating capital (papersettings.MaxCapitalJPY).
const MaxBudgetJPY = papersettings.MaxCapitalJPY

// Keys returns every runtime_settings key Apply accepts, in display order.
func Keys() []string { return []string{KeyStockBudgetJPY, KeyBondETFBudgetJPY} }

// IsKey reports whether key is one of Keys.
func IsKey(key string) bool { return slices.Contains(Keys(), key) }

// Overrides are the stored rows of Keys. A nil field means that key has no
// (valid) stored row.
type Overrides struct {
	StockBudgetJPY   *float64
	BondETFBudgetJPY *float64
}

// Budgets are the budgets in force, in yen.
type Budgets struct {
	StockJPY   float64
	BondETFJPY float64
}

// Total is the budgets' sum.
func (b Budgets) Total() float64 { return b.StockJPY + b.BondETFJPY }

// Validate checks n against key's range: a whole number of yen from 0 to
// MaxBudgetJPY. It is the single range check every writer of a bucket key uses.
func Validate(key string, n float64) error {
	if !IsKey(key) {
		return fmt.Errorf("bucketsettings: %q is not a bucket budget key", key)
	}
	if math.IsNaN(n) || math.IsInf(n, 0) || n != math.Trunc(n) || n < 0 || n > MaxBudgetJPY {
		return fmt.Errorf("bucketsettings: %s=%v: 予算枠は 0 円以上 1 兆円（1000000000000 円）以下の整数で指定してください（0 はその枠を使わない）", key, n)
	}
	return nil
}

// Apply writes jsonValue - a runtime_settings.value, a JSON number - into the
// field named by key after Validate. o is left untouched on error.
func Apply(o *Overrides, key, jsonValue string) error {
	var n float64
	if err := json.Unmarshal([]byte(jsonValue), &n); err != nil {
		return fmt.Errorf("bucketsettings: decode runtime setting %s=%q as a number: %w", key, jsonValue, err)
	}
	if err := Validate(key, n); err != nil {
		return err
	}
	if key == KeyStockBudgetJPY {
		o.StockBudgetJPY = &n
	} else {
		o.BondETFBudgetJPY = &n
	}
	return nil
}

// Resolve returns the budgets in force for an operating capital of capitalJPY.
// A bucket with no stored row has its default: the stock bucket the whole
// capital, the bond_etf bucket 0 - the behaviour before the buckets existed.
func (o Overrides) Resolve(capitalJPY float64) Budgets {
	b := Budgets{StockJPY: capitalJPY}
	if o.StockBudgetJPY != nil {
		b.StockJPY = *o.StockBudgetJPY
	}
	if o.BondETFBudgetJPY != nil {
		b.BondETFJPY = *o.BondETFBudgetJPY
	}
	return b
}

// CheckTotal rejects budgets whose sum exceeds capitalJPY: the buckets share
// the operating capital, they cannot be worth more than it together.
func CheckTotal(b Budgets, capitalJPY float64) error {
	if total := b.Total(); total > capitalJPY {
		return fmt.Errorf("bucketsettings: 枠の合計 %v 円（株 %v 円 + 債券ETF %v 円）が運用資金 %v 円を超えています", total, b.StockJPY, b.BondETFJPY, capitalJPY)
	}
	return nil
}

// Store is the runtime_settings read access Load needs;
// *system.RuntimeSettingsRepository implements it.
type Store interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
}

// Load reads every stored row. A row that no longer parses or is out of range
// (the Settings screen stores only valid ones) is logged and treated as not
// stored, so the bucket then has its default (Resolve); only a store read
// failure is an error.
func Load(ctx context.Context, store Store) (Overrides, error) {
	var o Overrides
	for _, key := range Keys() {
		raw, ok, err := store.Get(ctx, key)
		if err != nil {
			return Overrides{}, fmt.Errorf("bucketsettings: read %s: %w", key, err)
		}
		if !ok {
			continue
		}
		if err := Apply(&o, key, raw); err != nil {
			slog.Warn("bucketsettings: ignoring invalid stored bucket budget; its default applies", "error", err)
		}
	}
	return o, nil
}

// Effective returns the budgets in force for capitalJPY (Load then Resolve).
// It is the per-evaluation read the Risk Engine and the Settings screen share.
func Effective(ctx context.Context, store Store, capitalJPY float64) (Budgets, error) {
	o, err := Load(ctx, store)
	if err != nil {
		return Budgets{}, err
	}
	return o.Resolve(capitalJPY), nil
}

// CheckSave checks that storing the encoded value under key keeps the budgets'
// sum within the operating capital. key is a bucket key (encoded is the budget
// being stored) or papersettings.KeyInitialCapitalJPY (encoded is the capital
// being stored); capitalJPY is the capital in force before the save. The
// other budgets are read from store, a missing row meaning its default
// (Resolve). It is the one check behind every writer of a bucket key or of the
// capital: the Settings screen's Save, the MCP's Set and Reset's default check
// all reach it through opsettings.Service.
func CheckSave(ctx context.Context, store Store, capitalJPY float64, key, encoded string) error {
	overrides, err := Load(ctx, store)
	if err != nil {
		return err
	}
	if key == papersettings.KeyInitialCapitalJPY {
		var stored papersettings.Overrides
		if err := papersettings.Apply(&stored, key, encoded); err != nil {
			return err
		}
		capitalJPY = *stored.InitialCapitalJPY
	} else if err := Apply(&overrides, key, encoded); err != nil {
		return err
	}
	return CheckTotal(overrides.Resolve(capitalJPY), capitalJPY)
}

// DefaultText is what applies to key while nothing is stored, for an operating
// capital of capitalJPY: the stock budget the whole capital rounded down to a
// whole yen (budgets are integers, and the capital need not be), the bond_etf
// budget 0.
func DefaultText(key string, capitalJPY float64) string {
	if key == KeyStockBudgetJPY {
		return strconv.FormatFloat(math.Floor(capitalJPY), 'f', -1, 64)
	}
	return "0"
}
