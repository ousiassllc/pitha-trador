package training

import (
	"sort"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
)

// calibrationBinCount is the number of equal-width bins of the OOS reliability
// curve.
const calibrationBinCount = 10

// Evaluation is a model's out-of-sample performance.
type Evaluation struct {
	// Samples is the number of OOS rows the model could score.
	Samples int
	// AUC is the area under the ROC curve of the raw probability against
	// "won"; nil when the OOS rows are all one class (undefined).
	AUC *float64
	// Takes is the number of OOS rows the model (with the Decide policy it was
	// evaluated under) would have taken, and EVBps their mean net return in
	// bps after costs; nil when it would have taken none.
	Takes int
	EVBps *float64
	// Calibration is the reliability curve: per non-empty calibrated
	// probability bin, the mean prediction against the observed win rate.
	Calibration []repo.CalibrationBin
	// scored are the rows the model could score, kept for the bootstrap
	// intervals and the comparison with another model on the same rows.
	scored []scoredRow
}

// Evaluate scores m on the out-of-sample rows: AUC, the mean net return of the
// signals Decide would take under cfg (exactly the rule used live, so the
// number is the EV a deployed model would have earned) and the reliability
// curve. A row m cannot score is left out of Samples.
func Evaluate(m metalabel.Model, rows []Row, cfg metalabel.DecideConfig) Evaluation {
	var (
		e          Evaluation
		raws       []float64
		wins       []bool
		binSum     [calibrationBinCount]float64
		binWins    [calibrationBinCount]int
		binCount   [calibrationBinCount]int
		takeReturn float64
	)
	for idx, r := range rows {
		p, err := m.Predict(r.Vector)
		if err != nil {
			continue
		}
		raws = append(raws, p.Raw)
		wins = append(wins, r.NetReturnBps > 0)
		sr := scoredRow{index: idx, day: dayOf(r), symbol: r.Symbol, win: r.NetReturnBps > 0, raw: p.Raw, ret: r.NetReturnBps}
		i := min(int(p.Calibrated*calibrationBinCount), calibrationBinCount-1)
		binSum[i] += p.Calibrated
		binCount[i]++
		if r.NetReturnBps > 0 {
			binWins[i]++
		}
		if d := metalabel.Decide(&m, r.Direction, r.Features, cfg); d.Action == repo.ActionTake {
			e.Takes++
			takeReturn += r.NetReturnBps
			sr.take = true
		}
		e.scored = append(e.scored, sr)
	}
	e.Samples = len(raws)
	e.AUC = auc(raws, wins)
	if e.Takes > 0 {
		ev := takeReturn / float64(e.Takes)
		e.EVBps = &ev
	}
	for i, n := range binCount {
		if n == 0 {
			continue
		}
		e.Calibration = append(e.Calibration, repo.CalibrationBin{
			MeanPredicted: binSum[i] / float64(n), ObservedRate: float64(binWins[i]) / float64(n), Count: n,
		})
	}
	return e
}

// auc is the probability that a random winner outscores a random loser (ties
// count half), the Mann-Whitney form of the area under the ROC curve. It is nil
// when there are no winners or no losers.
func auc(scores []float64, wins []bool) *float64 {
	type scored struct {
		score float64
		win   bool
	}
	items := make([]scored, len(scores))
	nWin := 0
	for i := range scores {
		items[i] = scored{scores[i], wins[i]}
		if wins[i] {
			nWin++
		}
	}
	nLose := len(items) - nWin
	if nWin == 0 || nLose == 0 {
		return nil
	}
	sort.Slice(items, func(i, j int) bool { return items[i].score < items[j].score })
	// Sum of the (average) ranks of the winners.
	var rankSum float64
	for i := 0; i < len(items); {
		j := i
		for j < len(items) && items[j].score == items[i].score {
			j++
		}
		avg := float64(i+j+1) / 2 // mean of ranks i+1..j
		for k := i; k < j; k++ {
			if items[k].win {
				rankSum += avg
			}
		}
		i = j
	}
	v := (rankSum - float64(nWin)*float64(nWin+1)/2) / (float64(nWin) * float64(nLose))
	return &v
}
