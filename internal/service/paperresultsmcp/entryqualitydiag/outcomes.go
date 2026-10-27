package entryqualitydiag

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"sort"

	"github.com/ousiassllc/pitha-trador/internal/config/barriersettings"
	"github.com/ousiassllc/pitha-trador/internal/config/costsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// HorizonCount is how many labeled candidates were held for Minutes (the label
// row's horizon_minutes: the triple-barrier timeout in force when it was
// labeled).
type HorizonCount struct {
	Minutes int `json:"minutes"`
	Labeled int `json:"labeled"`
}

// WorstOutcome is the labeled candidate with the lowest net return.
type WorstOutcome struct {
	NetReturnBps  float64 `json:"net_return_bps"`
	JevDecisionID int64   `json:"jev_decision_id"`
	Symbol        string  `json:"symbol"`
	DecidedAt     string  `json:"decided_at"`
	BarrierHit    *string `json:"barrier_hit"`
}

// NetReturn summarizes the net returns (bps, after the label's trading cost)
// of the labeled candidates.
type NetReturn struct {
	Mean   float64      `json:"mean_bps"`
	Median float64      `json:"median_bps"`
	Sum    float64      `json:"sum_bps"`
	Worst  WorstOutcome `json:"worst"`
}

// Outcomes is what the outcome labels say about the candidates of one scenario
// and side that passed every guard (Counts.PassAllGuards): Candidates =
// Labeled + Unlabeled. Wins are the labeled candidates with a net return above
// zero. WinRate and NetReturn are null while nothing is labeled.
type Outcomes struct {
	Candidates int            `json:"candidates"`
	Labeled    int            `json:"labeled"`
	Unlabeled  int            `json:"unlabeled"`
	Wins       int            `json:"wins"`
	WinRate    *float64       `json:"win_rate"`
	NetReturn  *NetReturn     `json:"net_return"`
	Horizons   []HorizonCount `json:"horizon_minutes"`
}

// ScenarioOutcomes is Outcomes for each side.
type ScenarioOutcomes struct {
	Long  Outcomes `json:"long"`
	Short Outcomes `json:"short"`
}

// LabelCostBps is the cost the label of each side assumes.
type LabelCostBps struct {
	Long  float64 `json:"long"`
	Short float64 `json:"short"`
}

// SlippageBps is the slippage of one side's fills by session.
type SlippageBps struct {
	ContinuousBps float64 `json:"continuous_bps"`
	OpeningBps    float64 `json:"opening_bps"`
	ClosingBps    float64 `json:"closing_bps"`
}

// LabelAssumptions are the holding time, fee and slippage assumptions behind
// the outcomes. The label rows keep no copy of the settings they were made
// with, so the values are the ones in force now (calibration.barrier_* and
// cost.* in runtime_settings, the shipped defaults for an unset key); a row
// labeled before a settings change used the earlier values.
type LabelAssumptions struct {
	LabelVersion int    `json:"label_version"`
	Selection    string `json:"selection"`
	Win          string `json:"win"`
	Exit         string `json:"exit"`
	// Settings says the values below are today's, not those of each label.
	Settings string `json:"settings"`

	TimeoutMinutes int     `json:"timeout_minutes"`
	TakeProfitBps  float64 `json:"take_profit_bps"`
	StopLossBps    float64 `json:"stop_loss_bps"`
	// BarrierCostBps is calibration.barrier_cost_bps, the all-in base cost the
	// label cost starts from; LabelCostBps is it moved by the cost.* settings.
	BarrierCostBps       float64      `json:"barrier_cost_bps"`
	LabelCostBps         LabelCostBps `json:"label_cost_bps"`
	FeeBps               float64      `json:"fee_bps"`
	FeeFixedJPY          float64      `json:"fee_fixed_jpy"`
	ReferenceNotionalJPY float64      `json:"reference_notional_jpy"`
	SlippageLong         SlippageBps  `json:"slippage_long"`
	SlippageShort        SlippageBps  `json:"slippage_short"`
	ShortBorrowBps       float64      `json:"short_borrow_bps"`
}

const (
	selectionText = "判断ごとに label_version=2 で net_return_bps がある行のうち新しい1行（メタモデルの学習行と同じ規則）。ラベルのない候補は unlabeled に数え、集計に入れない"
	winText       = "net_return_bps > 0（メタモデルの学習と同じ定義）。win_rate = wins / labeled"
	exitText      = "利確は +take_profit_bps、損切りは -stop_loss_bps で決済、タイムアウトは最後の足の終値からラベルコストを引いた値。保有時間は horizon_minutes（タイムアウトの分）"
	settingsText  = "ラベル行には作成時の設定が残らないため、下の値は今の runtime_settings（未設定はデフォルト）。ラベル作成後に設定を変えていれば、その行は前の値で作られている"
)

// candidate is one decision that passed every guard in a scenario.
type candidate struct {
	id    int64
	short bool
}

// label is the outcome of one decision, as the labeler recorded it.
type label struct {
	symbol, decidedAt string
	horizon           int
	netBps            float64
	barrierHit        sql.NullString
}

// listLabels returns the newest label_version 2 outcome with a net return of
// each LONG/SHORT trader decision of [from, to) in scope, keyed by decision id
// (the selection ListTrainingRowsFrom makes for the retraining).
func listLabels(ctx context.Context, q query.Q, from, to, scope string, scopeArgs []any) (map[int64]label, error) {
	if scope != "" {
		scope = " AND " + scope
	}
	v := domain.CalibrationLabelVersionTripleBarrier
	args := append([]any{v, from, to}, scopeArgs...)
	rows, err := q.QueryContext(ctx, `
SELECT d.id, d.symbol, d.timestamp, o.horizon_minutes, o.net_return_bps, o.barrier_hit
FROM calibration_outcomes o
JOIN jev_decisions d ON d.id = o.jev_decision_id
WHERE d.decision_type = 'trader' AND d.direction IN ('LONG', 'SHORT')
	AND o.label_version = ? AND o.net_return_bps IS NOT NULL
	AND d.timestamp >= ? AND d.timestamp < ?`+scope+`
	AND o.id = (SELECT MAX(o2.id) FROM calibration_outcomes o2
		WHERE o2.jev_decision_id = d.id AND o2.label_version = ? AND o2.net_return_bps IS NOT NULL)`,
		append(args, v)...)
	if err != nil {
		return nil, fmt.Errorf("query: list outcome labels: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[int64]label{}
	for rows.Next() {
		var (
			id int64
			l  label
			at string
		)
		if err := rows.Scan(&id, &l.symbol, &at, &l.horizon, &l.netBps, &l.barrierHit); err != nil {
			return nil, fmt.Errorf("query: scan outcome label: %w", err)
		}
		l.decidedAt = query.FormatOut(at)
		out[id] = l
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query: list outcome labels: %w", err)
	}
	return out, nil
}

// outcomesOf summarizes the labels of the candidates on one side.
func outcomesOf(cands []candidate, short bool, labels map[int64]label) Outcomes {
	out := Outcomes{Horizons: []HorizonCount{}}
	var (
		nets    []float64
		sum     float64
		worstID int64
		worst   label
		byHor   = map[int]int{}
	)
	for _, c := range cands {
		if c.short != short {
			continue
		}
		out.Candidates++
		l, ok := labels[c.id]
		if !ok {
			out.Unlabeled++
			continue
		}
		out.Labeled++
		if l.netBps > 0 {
			out.Wins++
		}
		sum += l.netBps
		nets = append(nets, l.netBps)
		byHor[l.horizon]++
		if len(nets) == 1 || l.netBps < worst.netBps {
			worstID, worst = c.id, l
		}
	}
	for minutes, n := range byHor {
		out.Horizons = append(out.Horizons, HorizonCount{Minutes: minutes, Labeled: n})
	}
	sort.Slice(out.Horizons, func(i, j int) bool { return out.Horizons[i].Minutes < out.Horizons[j].Minutes })
	if out.Labeled == 0 {
		return out
	}
	rate := float64(out.Wins) / float64(out.Labeled)
	out.WinRate = &rate
	var hit *string
	if worst.barrierHit.Valid {
		hit = &worst.barrierHit.String
	}
	out.NetReturn = &NetReturn{
		Mean: sum / float64(out.Labeled), Median: median(nets), Sum: sum,
		Worst: WorstOutcome{NetReturnBps: worst.netBps, JevDecisionID: worstID, Symbol: worst.symbol, DecidedAt: worst.decidedAt, BarrierHit: hit},
	}
	return out
}

// median is the middle value of xs (the mean of the two middle ones for an even
// count). xs is not empty; it is sorted in place.
func median(xs []float64) float64 {
	slices.Sort(xs)
	n := len(xs)
	if n%2 == 1 {
		return xs[n/2]
	}
	return (xs[n/2-1] + xs[n/2]) / 2
}

// labelSettingsReader reads the triple-barrier and cost runtime_settings keys
// only, with the same single-key statement the other fixed-key reads use.
type labelSettingsReader struct{ q query.Q }

func (s labelSettingsReader) Get(ctx context.Context, key string) (string, bool, error) {
	if !barriersettings.IsKey(key) && !costsettings.IsKey(key) {
		return "", false, fmt.Errorf("entryqualitydiag: runtime setting %q is not readable", key)
	}
	var value string
	err := s.q.QueryRowContext(ctx, `SELECT value FROM runtime_settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("entryqualitydiag: read runtime setting %s: %w", key, err)
	}
	return value, true, nil
}

// labelAssumptions resolves the settings the labels are measured with now.
func labelAssumptions(ctx context.Context, q query.Q) (LabelAssumptions, error) {
	store := labelSettingsReader{q}
	barriers, err := barriersettings.Load(ctx, store)
	if err != nil {
		return LabelAssumptions{}, fmt.Errorf("entryqualitydiag: %w", err)
	}
	costs, err := costsettings.Load(ctx, store)
	if err != nil {
		return LabelAssumptions{}, fmt.Errorf("entryqualitydiag: %w", err)
	}
	slip := func(s costsettings.Slippage) SlippageBps {
		return SlippageBps{ContinuousBps: s.ContinuousBps, OpeningBps: s.OpeningBps, ClosingBps: s.ClosingBps}
	}
	return LabelAssumptions{
		LabelVersion: domain.CalibrationLabelVersionTripleBarrier, Selection: selectionText, Win: winText, Exit: exitText, Settings: settingsText,
		TimeoutMinutes: barriers.TimeoutMinutes(), TakeProfitBps: barriers.TakeProfitBps, StopLossBps: barriers.StopLossBps,
		BarrierCostBps: barriers.CostBps,
		LabelCostBps: LabelCostBps{
			Long:  costs.LabelCostBps(barriers.CostBps, domain.JevDirectionLong),
			Short: costs.LabelCostBps(barriers.CostBps, domain.JevDirectionShort),
		},
		FeeBps: costs.FeeBps, FeeFixedJPY: costs.FeeFixedJPY, ReferenceNotionalJPY: costs.ReferenceNotionalJPY,
		SlippageLong: slip(costs.Long), SlippageShort: slip(costs.Short), ShortBorrowBps: costs.ShortBorrowBps,
	}, nil
}
