package slicetests

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// evaluation is the part of jev_evaluation's answer the slices must agree with.
type evaluation struct {
	Decisions int `json:"decisions"`
	Cohorts   []struct {
		ModelID              string `json:"model_id"`
		QuestionVersion      string `json:"question_version"`
		Decisions            int    `json:"decisions"`
		ExcludedBeforeCutoff int    `json:"excluded_before_cutoff"`
		Evaluated            int    `json:"evaluated"`
		Arms                 []struct {
			Arm string `json:"arm"`
			All struct {
				N          int      `json:"n"`
				Days       int      `json:"days"`
				MeanNetBps *float64 `json:"mean_net_bps"`
				WinRate    *float64 `json:"win_rate"`
				CI         *struct {
					Lower float64 `json:"lower"`
					Upper float64 `json:"upper"`
				} `json:"ci"`
			} `json:"all"`
		} `json:"arms"`
	} `json:"cohorts"`
}

func evaluationOf(t *testing.T, srv *testServer, args map[string]any) evaluation {
	t.Helper()
	a := map[string]any{"from": "2026-10-05", "to": "2026-10-12"}
	for k, v := range args {
		a[k] = v
	}
	r := srv.post(t, "tools/call", map[string]any{"name": "jev_evaluation", "arguments": a})
	var env struct {
		Result struct {
			StructuredContent struct {
				Data evaluation `json:"data"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(r.body, &env); err != nil {
		t.Fatalf("decode %s: %v", r.body, err)
	}
	return env.Result.StructuredContent.Data
}

// agree fails unless every cohort of the slices has jev_evaluation's decisions, evaluated rows and llm headline (n, days, mean, win
// rate, interval): the slices cut the same population with the same label, not a re-derived one.
func agree(t *testing.T, srv *testServer, args map[string]any) {
	t.Helper()
	ev := evaluationOf(t, srv, args)
	d := srv.call(t, args).data(t)
	if len(ev.Cohorts) == 0 || len(ev.Cohorts) != len(d.Cohorts) || ev.Decisions != d.Decisions {
		t.Fatalf("jev_evaluation has %d cohorts / %d decisions, the slices %d / %d", len(ev.Cohorts), ev.Decisions, len(d.Cohorts), d.Decisions)
	}
	for i, e := range ev.Cohorts {
		c := d.Cohorts[i]
		if c.ModelID != e.ModelID || c.QuestionVersion != e.QuestionVersion || c.Decisions != e.Decisions || c.Evaluated != e.Evaluated {
			t.Errorf("cohort %d: slices %s/%s %d/%d, evaluation %s/%s %d/%d", i, c.ModelID, c.QuestionVersion, c.Decisions, c.Evaluated, e.ModelID, e.QuestionVersion, e.Decisions, e.Evaluated)
		}
		for _, arm := range e.Arms {
			if arm.Arm != "llm" {
				continue
			}
			a := arm.All
			if c.All.N != a.N || c.All.Days != a.Days || a.MeanNetBps == nil || c.All.MeanNetBps == nil || math.Abs(*c.All.MeanNetBps-*a.MeanNetBps) > eps ||
				math.Abs(*c.All.WinRate-*a.WinRate) > eps ||
				(c.All.CI == nil) != (a.CI == nil) || c.All.CI != nil && (math.Abs(c.All.CI.Lower-a.CI.Lower) > eps || math.Abs(c.All.CI.Upper-a.CI.Upper) > eps) {
				t.Errorf("cohort %d all = %+v, jev_evaluation llm = %+v", i, c.All, a)
			}
		}
	}
}

// The population, the label and the cohort split are jev_evaluation's: the llm headline of every cohort is the same number, also under a
// prompt_version / model_id filter and under a confirmed cutoff (which drops earlier days from the slices exactly as it does there),
// and a placeholder cutoff is said in the caveats and not applied.
func TestSlicesCutTheSamePopulationAsJevEvaluation(t *testing.T) {
	srv, w := newServer(t)
	seedSmall(t, w)
	agree(t, srv, nil)
	agree(t, srv, map[string]any{"model_id": m2})
	agree(t, srv, map[string]any{"prompt_version": v3, "horizon_minutes": 5})

	d := srv.call(t, nil).data(t)
	flagged := false
	for _, c := range d.Caveats {
		flagged = flagged || strings.Contains(c, "仮置き")
	}
	if d.CutoffApplied || !flagged {
		t.Errorf("placeholder cutoff: applied %v, caveat flagged %v", d.CutoffApplied, flagged)
	}

	testkit.Setting(t, w, "metalabel.llm_cutoff", `"2026-10-06"`)
	testkit.Setting(t, w, "metalabel.llm_cutoff_model", `"`+m1+`"`)
	agree(t, srv, nil)
	d = srv.call(t, nil).data(t)
	c := cohortOf(t, d, m1, v3)
	if !d.CutoffApplied || c.Decisions != 8 || c.Evaluated != 5 { // ids 1 and 2 are on 10-05, before the cutoff
		t.Errorf("cutoff: applied %v, decisions %d evaluated %d, want true/8/5", d.CutoffApplied, c.Decisions, c.Evaluated)
	}
	if _, dim := dimOf(t, c, "weekday"); bucketOf(t, dim, "mon").N != 0 || bucketOf(t, dim, "tue").N != 2 {
		t.Errorf("weekday after the cutoff = %+v", dim.Buckets)
	}
	for _, c := range d.Caveats {
		if strings.Contains(c, "仮置き") {
			t.Errorf("a confirmed cutoff still has the placeholder caveat: %s", c)
		}
	}
}

// Two calls on the same data return the same bytes (fixed seed, fixed order), and a period shorter than the data
// leaves out what is outside it.
func TestResponseIsDeterministicAndPeriodBound(t *testing.T) {
	srv, w := newServer(t)
	seedSmall(t, w)
	a, b := srv.call(t, nil), srv.call(t, nil)
	if string(a.body) != string(b.body) {
		t.Error("two calls on the same data differ")
	}
	d := srv.call(t, map[string]any{"from": "2026-10-06", "to": "2026-10-07"}).data(t)
	if len(d.Cohorts) != 1 || d.Cohorts[0].Evaluated != 2 || d.From != "2026-10-06" || d.To != "2026-10-07" {
		t.Errorf("one day = %+v", d.Cohorts)
	}
}
