package opsettings

import (
	"context"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/config/barriersettings"
	"github.com/ousiassllc/pitha-trador/internal/config/costsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// validateCost checks a cost setting being saved (issue #779): its own range,
// the whole set, and that the label cost it implies still leaves the
// triple-barrier stop-loss on the loss side for both directions.
func (s *Service) validateCost(ctx context.Context, key, encoded string) error {
	costs := costsettings.Default()
	if err := s.overlay(ctx, costsettings.Keys(), func(k, raw string) error { return costsettings.Apply(&costs, k, raw) }); err != nil {
		return err
	}
	if err := costsettings.Apply(&costs, key, encoded); err != nil {
		return &InvalidValueError{Reason: err.Error()}
	}
	if err := costs.Validate(); err != nil {
		return invalidIf(err)
	}
	barriers := barriersettings.Default()
	if err := s.overlay(ctx, barriersettings.Keys(), func(k, raw string) error { return barriersettings.Apply(&barriers, k, raw) }); err != nil {
		return err
	}
	return invalidIf(labelCostBelowStopLoss(barriers, costs))
}

// labelCostBelowStopLoss rejects a combination whose label cost (the barrier
// cost moved by the cost settings, per direction) is not below the stop-loss
// width: the label's stop-loss would sit on the profit side of the entry.
func labelCostBelowStopLoss(b barriersettings.Barriers, costs costsettings.Settings) error {
	for _, direction := range []string{domain.JevDirectionLong, domain.JevDirectionShort} {
		if label := costs.LabelCostBps(b.CostBps, direction); label >= b.StopLossBps {
			return fmt.Errorf("コスト設定を反映した %s のラベルコスト %g bps が %s（%g bps）以上になります。コストを下げるか損切り幅を広げてください",
				direction, label, barriersettings.KeyStopLossBps, b.StopLossBps)
		}
	}
	return nil
}

// validateLabelCostAgainst is labelCostBelowStopLoss for new barriers under the
// stored cost settings.
func (s *Service) validateLabelCostAgainst(ctx context.Context, barriers barriersettings.Barriers) error {
	costs := costsettings.Default()
	if err := s.overlay(ctx, costsettings.Keys(), func(k, raw string) error { return costsettings.Apply(&costs, k, raw) }); err != nil {
		return err
	}
	return invalidIf(labelCostBelowStopLoss(barriers, costs))
}
