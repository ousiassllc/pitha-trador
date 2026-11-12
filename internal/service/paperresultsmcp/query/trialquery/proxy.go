package trialquery

import (
	"slices"

	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
)

// ProxyNote is the fixed caveat of the counterfactual_proxy block.
const ProxyNote = "counterfactual_labels は、見送られたライブの LONG/SHORT 候補を「ライブの方向で入っていたらどうなったか」で記録した既存のラベル（新しく計算していない）。" +
	"NONE と答えた（または答えなかった）アームの判断に付いていれば、その見送りが結果として良かったかの目安（proxy）にすぎず、実現した結果ではない。" +
	"hit・miss・順位・realised の指標には混ぜず、ここだけに出す。ラベルが付いていない判断は coverage に現れる"

// ProxyHorizon is the counterfactual labels (proxy) of one horizon over the
// arm's no-direction decisions: how many have a label and what the live
// direction's hypothetical entry did (the barrier it hit first and its mean
// net return in bps, net of cost as the label records it).
type ProxyHorizon struct {
	HorizonMinutes  int            `json:"horizon_minutes"`
	Labelled        int            `json:"labelled"`
	Coverage        *float64       `json:"coverage"`
	BarrierHit      map[string]int `json:"barrier_hit"`
	AvgNetReturnBps *float64       `json:"avg_net_return_bps_of_live_direction"`
}

// ProxyArm is one arm's no-direction decisions in the cohort (NONE answers and
// decisions the arm gave no answer to) and the counterfactual labels they have.
type ProxyArm struct {
	ArmID               string         `json:"arm_id"`
	NoDirectionDecision int            `json:"no_direction_decisions"`
	NoneAnswers         int            `json:"none_answers"`
	NoAnswer            int            `json:"no_answer"`
	Horizons            []ProxyHorizon `json:"horizons"`
}

// Proxy is the counterfactual_proxy block. Proxy is always true: nothing in it
// is a realised outcome.
type Proxy struct {
	Proxy bool       `json:"proxy"`
	Note  string     `json:"note"`
	Arms  []ProxyArm `json:"arms"`
}

func buildProxy(arms []string, cohort []*decision, labels map[int]map[int64]cfLabel) Proxy {
	out := Proxy{Proxy: true, Note: ProxyNote, Arms: make([]ProxyArm, len(arms))}
	horizons := make([]int, 0, len(labels))
	for h := range labels {
		horizons = append(horizons, h)
	}
	slices.Sort(horizons)
	for i, arm := range arms {
		pa := ProxyArm{ArmID: arm, Horizons: []ProxyHorizon{}}
		var noDirection []*decision
		for _, d := range cohort {
			switch r := d.rows[i]; {
			case r.status != jevtrial.StatusOK:
				pa.NoAnswer++
				noDirection = append(noDirection, d)
			case r.direction == dirNone:
				pa.NoneAnswers++
				noDirection = append(noDirection, d)
			}
		}
		pa.NoDirectionDecision = len(noDirection)
		for _, h := range horizons {
			ph := ProxyHorizon{HorizonMinutes: h, BarrierHit: map[string]int{"tp": 0, "sl": 0, "timeout": 0}}
			var netSum float64
			for _, d := range noDirection {
				if l, ok := labels[h][d.id]; ok {
					ph.Labelled++
					ph.BarrierHit[l.barrier]++
					netSum += l.netBps
				}
			}
			if ph.Labelled == 0 {
				continue
			}
			avg := netSum / float64(ph.Labelled)
			ph.AvgNetReturnBps, ph.Coverage = &avg, ratio(ph.Labelled, pa.NoDirectionDecision)
			pa.Horizons = append(pa.Horizons, ph)
		}
		out.Arms[i] = pa
	}
	return out
}
