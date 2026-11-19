package promotiondiag

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/promotionsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experiment"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// checkSettingsSet is Check for a candidate of kind experiment / setting: the same eleven items, read from the
// experiment's before / after comparison (experiment_compare's own function) instead of from the candidate
// ledger. It reads only; nothing is applied (applied stays false) and nothing is written.
func checkSettingsSet(ctx context.Context, q query.Q, now time.Time, req Request, criteria Criteria, eff promotionsettings.Criteria) (*Result, error) {
	out := &Result{
		Candidate: *req.Candidate, Current: Ref{Kind: KindExperimentBefore}, Notice: notice,
		Criteria: criteria, Definitions: settingsSetDefinitions(), EvidenceNote: evidenceNote,
	}
	// Every recorded experiment is the family of the multiple-comparison correction (an experiment or a
	// setting is chosen out of the experiments tried), and the setting kind picks its newest one from them.
	exps, err := experiment.List(ctx, q, "", MaxFamilyExtra)
	if err != nil {
		return nil, err
	}
	exp, found, err := pickExperiment(ctx, q, *req.Candidate, exps)
	if err != nil {
		return nil, err
	}
	if !found {
		out.Items, out.EvidenceNote = noExperimentItems(), noExperimentNote
		finish(out)
		return out, nil
	}
	cmp, err := experiment.Compare(ctx, q, exp, now, 0, 0, false)
	if err != nil {
		return nil, err
	}
	before, after, err := tradeDays(ctx, q, exp, now)
	if err != nil {
		return nil, err
	}
	family := max(len(exps), 1) + req.FamilySizeExtra
	out.Items, out.ExperimentNumbers = settingsSetItems(cmp, before, after, family, eff)
	out.ExperimentNumbers.FamilySizeExtra = req.FamilySizeExtra
	out.Experiment = infoOf(cmp)
	out.Current.ID = fmt.Sprint(exp.ID)
	out.From, out.To = cmp.Before.From, cmp.After.To
	finish(out)
	return out, nil
}

// pickExperiment is the experiment of an experiment candidate (experiment.ErrNotFound when unknown) or the
// newest experiment of a setting candidate's key (found false when none was recorded).
func pickExperiment(ctx context.Context, q query.Q, c Ref, exps []experiment.Experiment) (experiment.Experiment, bool, error) {
	if id, ok := c.experimentID(); ok {
		exp, err := experiment.Get(ctx, q, id)
		return exp, err == nil, err
	}
	for _, e := range exps { // newest first
		if e.Variable.Kind == experiment.KindSetting && e.Variable.Name == c.ID {
			return e, true, nil
		}
	}
	return experiment.Experiment{}, false, nil
}

// tradeDays is the per-JST-day pnl of the closed trades of each period of the experiment (the very trades
// its before / after figures count: Windows are the instants Compare reads).
func tradeDays(ctx context.Context, q query.Q, exp experiment.Experiment, now time.Time) (before, after []query.Day, err error) {
	b, a := exp.Windows(now)
	if before, err = query.DailyPnL(ctx, q, b); err != nil {
		return nil, nil, err
	}
	after, err = query.DailyPnL(ctx, q, a)
	return before, after, err
}

// finish sets first_unmet and the (always false) eligible_as_evidence of a settings-set result.
func finish(out *Result) {
	for _, it := range out.Items {
		if it.Status != StatusPass && !it.NotApplicable && out.FirstUnmet == nil {
			name := it.Name
			out.FirstUnmet = &name
		}
	}
	out.EligibleAsEvidence = false
}

// noExperimentItems are all eleven items unknown: nothing was recorded to read.
func noExperimentItems() []Item {
	items := make([]Item, len(itemOrder))
	for i, name := range itemOrder {
		items[i] = Item{Name: name, Status: StatusUnknown, Detail: DetailNoExperimentResult}
	}
	return items
}
