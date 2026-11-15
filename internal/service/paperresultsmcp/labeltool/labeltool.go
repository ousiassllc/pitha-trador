// Package labeltool holds the additions of issue #1088 to the experiment tools
// (docs/api/endpoints/paper-results-mcp-experiments-labels.md):
//   - experiment_compare returns a label_based member next to the trade-based
//     fields (labelcompare), which never changes verdict / judged;
//   - settings_set takes an optional hypothesis and then records the experiment
//     of the change in the same transaction (mcpset.Writer.SetWithExperiment);
//   - experiments_list returns unrecorded_changes (the unrecorded package).
//
// All three are read-only or ride on the settings_set write that already
// exists behind the write switch; none changes a setting by itself.
package labeltool

import (
	"context"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experiment"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experiment/labelcompare"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// HypothesisProp is the schema of settings_set's optional hypothesis argument.
func HypothesisProp() map[string]any {
	return map[string]any{
		"type": "string", "maxLength": experiment.MaxHypothesisRunes,
		"description": "仮説（任意）。渡すとこの変更を 1 件の実験（kind=setting、変更前→変更後、audit_id=この変更の監査行）として同じトランザクションで記録し、応答に experiment を付ける。" +
			"空・長すぎる・変更前後が同じなら何も保存しない。省略時の動作と応答は従来どおり",
	}
}

// Set is settings_set's write: Writer.Set, or, with a hypothesis,
// Writer.SetWithExperiment (the experiment is nil without one). An error of the
// experiment path goes through experimentErr first; the caller maps the result.
func Set(ctx context.Context, w *mcpset.Writer, client, key, value string, hypothesis *string, experimentErr func(error) error) (mcpset.Entry, *experiment.Experiment, error) {
	if hypothesis == nil {
		entry, err := w.Set(ctx, client, key, value)
		return entry, nil, err
	}
	entry, exp, err := w.SetWithExperiment(ctx, client, key, value, *hypothesis)
	if err != nil {
		return mcpset.Entry{}, nil, experimentErr(err)
	}
	return entry, &exp, nil
}

// Compare is experiment_compare's run: the trade-based comparison (unchanged)
// and the label-based side next to it. fallbackCapitalJPY is risk.yaml's Paper
// capital, used while no Settings value is stored.
func Compare(ctx context.Context, q query.Q, exp experiment.Experiment, now time.Time, minTrades int, fallbackCapitalJPY float64) (any, int, error) {
	capital, err := mcpset.PaperCapitalJPY(ctx, q, fallbackCapitalJPY)
	if err != nil {
		return nil, 0, err
	}
	c, err := experiment.Compare(ctx, q, exp, now, minTrades, capital, true)
	if err != nil {
		return c, 2, err
	}
	out, err := labelcompare.Attach(ctx, q, exp, now, c)
	return out, 2, err
}
