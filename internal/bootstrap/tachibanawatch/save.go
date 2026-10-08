package tachibanawatch

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// held reads the held symbols; the held slots come first in every list.
func (d *Decider) held(ctx context.Context) ([]string, error) {
	return d.cfg.Held.HeldSymbols(ctx)
}

// saveScreened saves the daily_screen list of target.
func (d *Decider) saveScreened(ctx context.Context, now time.Time, target string, s screened) error {
	held, err := d.held(ctx)
	if err != nil {
		return err
	}
	entries := planScreen(d.max, held, d.set.ManualSymbols, s.picks)
	list := domain.WatchList{
		ListDate: target, Source: domain.WatchListDailyScreen, BasisDate: s.basis, DecidedAt: now, Entries: entries,
		Reason: fmt.Sprintf("日足スクリーニング（基準日 %s・対象 %d 銘柄）: 保有・注文中 %d・手動指定 %d・スクリーニング %d",
			s.basis, s.universe, countOrigin(entries, domain.WatchOriginHeld), countOrigin(entries, domain.WatchOriginManual),
			countOrigin(entries, domain.WatchOriginScreen)),
	}
	return d.save(ctx, list)
}

// fallback saves the list of target when the daily bars could not be used:
// the fixed list when the operator has one, else the previous 営業日's list,
// else only the held symbols. unchanged says that the list already is a
// fallback list: nothing is rewritten and nobody is told twice.
func (d *Decider) fallback(ctx context.Context, now time.Time, target, cause string, unchanged bool) error {
	if unchanged {
		return nil
	}
	held, err := d.held(ctx)
	if err != nil {
		return err
	}
	list := domain.WatchList{ListDate: target, DecidedAt: now}
	prefix := fmt.Sprintf("翌営業日（%s）の監視リストを日足から確定できませんでした（%s）。", target, cause)
	fixed := d.set.FixedSymbols
	switch previous, ok, err := d.cfg.Lists.AtOrBefore(ctx, previousDay(target)); {
	case err != nil:
		return err
	case len(fixed) > 0:
		list.Source, list.Entries = domain.WatchListFixedFallback, planFixedFallback(d.max, held, fixed)
		list.Reason = prefix + "固定リストに切り替えました。"
	case ok:
		list.Source, list.Entries = domain.WatchListCarriedOver, planCarryOver(d.max, held, previous)
		list.Reason = prefix + fmt.Sprintf("前営業日のリスト（%s 分）を引き継ぎました。", previous.ListDate)
	default:
		list.Source, list.Entries = domain.WatchListFixedFallback, planFixedFallback(d.max, held, nil)
		list.Reason = prefix + "引き継ぐリストも固定リストもないため、保有・注文中の銘柄だけを監視します。設定画面で固定リストを指定してください。"
	}
	if err := d.save(ctx, list); err != nil {
		return err
	}
	slog.Warn("tachibana: watch list fell back", "list_date", target, "source", list.Source, "symbols", len(list.Entries), "reason", cause)
	if d.cfg.Notify != nil {
		d.cfg.Notify(list.Reason)
	}
	return nil
}

// save stores list and logs how it was decided (counts only; no prices).
func (d *Decider) save(ctx context.Context, list domain.WatchList) error {
	if err := d.cfg.Lists.Save(ctx, list); err != nil {
		return fmt.Errorf("tachibanawatch: save the watch list of %s: %w", list.ListDate, err)
	}
	slog.Info("tachibana: watch list decided", "list_date", list.ListDate, "source", list.Source, "symbols", len(list.Entries),
		"held", countOrigin(list.Entries, domain.WatchOriginHeld), "manual", countOrigin(list.Entries, domain.WatchOriginManual),
		"screen", countOrigin(list.Entries, domain.WatchOriginScreen), "fixed", countOrigin(list.Entries, domain.WatchOriginFixed))
	return nil
}

func countOrigin(entries []domain.WatchListEntry, origin string) int {
	n := 0
	for _, e := range entries {
		if e.Origin == origin {
			n++
		}
	}
	return n
}

func sameEntries(a, b []domain.WatchListEntry) bool {
	return slices.EqualFunc(a, b, func(x, y domain.WatchListEntry) bool {
		return x.Symbol == y.Symbol && x.Origin == y.Origin
	})
}
