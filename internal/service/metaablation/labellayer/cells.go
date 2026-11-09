package labellayer

import (
	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/eligibility"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/stats"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
)

// cellResult is the table-1 row of cell c. rows is every independent row (for
// the funnel), eligible the common eligible candidates, days the sorted days.
func cellResult(c metaablation.Cell, rows, eligible []scored, days []string) CellResult {
	results := make([]eligibility.Result, len(rows))
	for i, s := range rows {
		results[i] = s.res
	}
	cr := CellResult{Cell: c, Name: c.Name(), Funnel: eligibility.BuildFunnel(results, c.QOn), Days: len(days), EligibleCandidates: len(eligible)}
	for _, s := range rows {
		if s.res.Unknown == "" && !s.res.Reversion && s.res.Outcome(c.QOn).Stage == 0 && c.MOn && !s.model {
			cr.MetaSkips++
		}
	}
	for _, s := range eligible {
		if takes(c, s) {
			cr.Takes++
			cr.SumNetBps += s.row.NetReturnBps
		}
	}
	if len(eligible) > 0 {
		cr.PerEligibleBps = cr.SumNetBps / float64(len(eligible))
	}
	if cr.Takes > 0 {
		v := cr.SumNetBps / float64(cr.Takes)
		cr.PerTakeBps = &v
	}
	return cr
}

// compare is the paired comparison To - From over the eligible candidates, by
// decision id: a skipped candidate counts 0, the unit of the bootstrap is the
// trading day (a cluster is the day's summed differences over its candidates).
func compare(plan stats.Plan, cmp metaablation.Comparison, eligible []scored, days []string) ComparisonResult {
	idx := map[string]int{}
	for i, d := range days {
		idx[d] = i
	}
	clusters := make([]stats.Cluster, len(days))
	var counts stats.Counts
	counts.Days, counts.EligibleCandidates, counts.BasisName = len(days), len(eligible), "discordant_pairs"
	for _, s := range eligible {
		cl := &clusters[idx[s.day]]
		cl.N++
		cl.Sum += value(cmp.To, s) - value(cmp.From, s)
		from, to := takes(cmp.From, s), takes(cmp.To, s)
		if from {
			counts.TakesFrom++
		}
		if to {
			counts.TakesTo++
		}
		if from != to {
			counts.Discordant++
		}
	}
	counts.Basis = counts.Discordant
	est := stats.Describe(clusters)
	if cmp.Primary {
		est = stats.BootstrapFor(plan, cmp.Name, clusters)
	}
	return ComparisonResult{From: cmp.From.Name(), To: cmp.To.Name(), Result: stats.Decide(plan, cmp.Name, !cmp.Primary, est, counts)}
}

// rescoreReference is population (i): the guard-free rescoring of the original
// OOS rows with the stored model. A row without a label or complete features
// is counted as incomplete.
func rescoreReference(m *metalabel.Model, cfg metalabel.DecideConfig, rows []Row) (Reference, error) {
	ref := Reference{Rows: len(rows)}
	var sum float64
	for _, r := range rows {
		if !r.Eligibility.Labeled || len(r.Eligibility.Features.MissingNames()) > 0 || r.Direction == "" {
			ref.Incomplete++
			continue
		}
		v := metalabel.Decide(m, r.Direction, r.Eligibility.Features, cfg)
		if rescoreFailed(v.Reason) {
			return Reference{}, errNotRescorable(v.Reason)
		}
		ref.Rescored++
		if v.Action == repo.ActionTake {
			ref.Takes++
			sum += r.NetReturnBps
		}
	}
	if ref.Rescored > 0 {
		v := sum / float64(ref.Rescored)
		ref.MeanNetBpsPerRow = &v
	}
	if ref.Takes > 0 {
		v := sum / float64(ref.Takes)
		ref.MeanNetBpsPerTake = &v
	}
	return ref, nil
}
