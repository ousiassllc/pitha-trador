// Package jevreplay is the replay evaluation (issue #1085): it sends the saved
// input of past live Jev trader decisions (jev_decisions.state_json, as stored,
// nothing added) to the model-comparison trial's arms (jev.trial.arms) after
// the fact, and records each arm's answer in the research tables
// jev_replay_results / jev_replay_runs. Past decisions can then be compared on
// the same input without waiting days for new ones.
//
// It is the trial's sibling, not a part of it: the trial runner
// (internal/service/jev/trial) is untouched and replay never writes
// jev_trial_decisions, jev_decisions or jev_shadow_decisions. It reuses the
// trial's arm configuration, per-arm clients and keys (trialwire), its cost
// semantics (reported / list-price estimate / worst case), reservations, JST
// daily caps and key redaction, and it counts its spend against the SAME daily
// caps as the live trial (jev_trial_decisions plus jev_replay_results). A
// replay result is never an input of trading, Policy, Risk, Jev, the RAG
// index, Calibration or the meta-model.
//
// Everything is off until a person turns it on on the Settings screen
// (jevreplaysettings.KeyEnabled): with the switch off, no key or no arm the
// Runner makes no external request. A run is started with a pre-flight
// estimate (BuildPlan) whose token the start must present.
package jevreplay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevreplay"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/jevreplay"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trial"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trialwire"
)

// ArmPlan is one arm's share of a plan.
type ArmPlan struct {
	Arm jevtrialsettings.Arm
	// Calls is the planned calls (decisions the arm is asked about).
	Calls int
	// ExcludedBeforeCutoff counts the decisions dated on or before the arm's
	// published training cutoff: they are not targeted.
	ExcludedBeforeCutoff int
	// CutoffStatus is jevreplay.CutoffVerified when the arm names a cutoff
	// (llm_cutoff), else jevreplay.CutoffUnverified (its results are reference-only).
	CutoffStatus string
	// MaxCostUSD is the reservation-style worst case over the arm's calls;
	// ListPriceUSD the list-price figure (nil: the arm has no list price).
	MaxCostUSD   float64
	ListPriceUSD *float64
}

// Call is one planned call: Decisions[Decision] asked of Arms[Arm].
type Call struct {
	Decision    int
	Arm         int
	RequestSize int
	ReservedUSD float64
}

// PlanInput is what a plan is made of. Spec.Arms names the arms of Arms (the
// caller resolved them); MaxID, when above 0, pins the decisions to those saved
// up to it.
type PlanInput struct {
	Spec     jevreplay.Spec
	From, To time.Time
	Arms     []jevtrialsettings.Arm
	MaxID    int64
}

// Plan is the pre-flight view of a run: the decisions chosen, the calls (decision
// x arm) that follow from the cutoff rule, and the worst-case cost. The estimate
// the Paper results MCP returns and the run the runner executes are the same plan.
type Plan struct {
	Spec      jevreplay.Spec
	From, To  time.Time
	Arms      []ArmPlan
	Decisions []repo.SavedDecision
	Calls     []Call
	// ExcludedInvalidState counts decisions whose saved input is not valid JSON
	// (nothing can be sent for them).
	ExcludedInvalidState int
	MaxDecisionID        int64
	MaxCostUSD           float64
	ListPriceUSD         *float64
	// Token names the plan's content; a run is only started with the token of
	// its current estimate.
	Token string
}

// BuildPlan reads the live trader decisions of in and plans the calls. Only
// decisions dated after an arm's published training cutoff are targeted for
// that arm; an arm without a cutoff is asked about every decision and its
// results are marked reference-only. The saved input is not changed or extended.
func BuildPlan(ctx context.Context, q repo.Querier, in PlanInput) (Plan, error) {
	notBefore, allKnown := time.Time{}, len(in.Arms) > 0
	for _, a := range in.Arms {
		from, known := a.TargetableFrom()
		if !known {
			allKnown = false
			continue
		}
		if notBefore.IsZero() || from.Before(notBefore) {
			notBefore = from
		}
	}
	if !allKnown {
		// An arm without a cutoff is asked about every decision of the period.
		notBefore = time.Time{}
	}
	decisions, err := repo.SavedDecisions(ctx, q, repo.DecisionQuery{From: in.From, To: in.To, NotBefore: notBefore, MaxID: in.MaxID, Limit: in.Spec.MaxRows})
	if err != nil {
		return Plan{}, err
	}
	p := Plan{Spec: in.Spec, From: in.From, To: in.To, Decisions: decisions, Arms: make([]ArmPlan, len(in.Arms))}
	listKnown := true
	var listTotal float64
	for i, a := range in.Arms {
		p.Arms[i] = ArmPlan{Arm: a, CutoffStatus: cutoffStatus(a)}
	}
	for di, d := range decisions {
		p.MaxDecisionID = max(p.MaxDecisionID, d.ID)
		state := json.RawMessage(d.StateJSON)
		if !json.Valid(state) {
			p.ExcludedInvalidState++
			continue
		}
		for ai, a := range in.Arms {
			ap := &p.Arms[ai]
			if from, known := a.TargetableFrom(); known && d.Timestamp.Before(from) {
				ap.ExcludedBeforeCutoff++
				continue
			}
			size, err := trial.RawRequestBytesFor(a, a.PromptVersion, state)
			if err != nil {
				return Plan{}, fmt.Errorf("jevreplay: size the request of arm %q for decision %d: %w", a.ID, d.ID, err)
			}
			reserved := trial.ReservedUSD(a, size)
			ap.Calls++
			ap.MaxCostUSD += reserved
			p.MaxCostUSD += reserved
			if list := listPriceUSD(a, size); list != nil {
				listTotal += *list
				if ap.ListPriceUSD == nil {
					ap.ListPriceUSD = new(0.0)
				}
				*ap.ListPriceUSD += *list
			} else {
				listKnown = false
			}
			p.Calls = append(p.Calls, Call{Decision: di, Arm: ai, RequestSize: size, ReservedUSD: reserved})
		}
	}
	if listKnown && len(p.Calls) > 0 {
		p.ListPriceUSD = &listTotal
	}
	p.Token = p.token()
	return p, nil
}

func cutoffStatus(a jevtrialsettings.Arm) string {
	if _, known := a.TargetableFrom(); known {
		return jevreplay.CutoffVerified
	}
	return jevreplay.CutoffUnverified
}

// token hashes what the estimate promised: the spec, the arms as configured
// (never keys), the decisions chosen, the calls and the worst-case cost. A start
// that presents a different token (anything changed since the estimate) is refused.
func (p Plan) token() string {
	h := sha256.New()
	enc := json.NewEncoder(h)
	arms := make([]jevtrialsettings.Arm, len(p.Arms))
	for i, a := range p.Arms {
		arms[i] = a.Arm
	}
	_ = enc.Encode(p.Spec)
	_ = enc.Encode(arms)
	_, _ = fmt.Fprintf(h, "%d|%d|%d|%d|%.8f", len(p.Decisions), len(p.Calls), p.MaxDecisionID, p.ExcludedInvalidState, p.MaxCostUSD)
	return hex.EncodeToString(h.Sum(nil))[:32]
}

// NewRun is the pending run row that executing p is recorded as.
func (p Plan) NewRun(createdBy string) (jevreplay.Run, error) {
	arms := make([]jevtrialsettings.Arm, len(p.Arms))
	for i, a := range p.Arms {
		arms[i] = a.Arm
	}
	armsJSON, err := jevtrialsettings.EncodeArms(arms)
	if err != nil {
		return jevreplay.Run{}, fmt.Errorf("jevreplay: encode the run's arms: %w", err)
	}
	return jevreplay.Run{
		Spec: p.Spec, PeriodFrom: p.From, PeriodTo: p.To, MaxRows: p.Spec.MaxRows, MaxDecisionID: p.MaxDecisionID, ArmsJSON: armsJSON,
		Estimate:  jevreplay.Estimate{Decisions: len(p.Decisions), Calls: len(p.Calls), MaxCostUSD: p.MaxCostUSD, ListPriceUSD: p.ListPriceUSD, Token: p.Token},
		CreatedBy: createdBy,
	}, nil
}

// armIDs lists the ids of arms.
func armIDs(arms []jevtrialsettings.Arm) []string {
	ids := make([]string, len(arms))
	for i, a := range arms {
		ids[i] = a.ID
	}
	return ids
}

// listPriceUSD is the list-price estimate of one call of a (nil: unknown). A chat
// arm's is unknown before the call: its cost includes the output, whose size is
// not known until the model answers (the plan's worst case is the reservation).
func listPriceUSD(a jevtrialsettings.Arm, requestBytes int) *float64 {
	if a.IsChat() {
		return nil
	}
	return trial.AnsweredCost(a, trialwire.Response{}, requestBytes).USD
}
