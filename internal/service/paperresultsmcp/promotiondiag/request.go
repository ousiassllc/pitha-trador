package promotiondiag

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/ousiassllc/pitha-trador/internal/config/promotionsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/ledgerdiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// MaxFamilyExtra bounds family_size_extra (an absurd family is a typo, not a count).
const MaxFamilyExtra = 10000

// candidateKinds are the kinds a candidate can have on the same rows as the current answer; the first
// four can also be the current one (a live_cohort judged other decisions, so nothing can be paired
// with it). settingsSetKinds (settingsset.go) are the other kinds a candidate can have.
var candidateKinds = []string{ledgerdiag.KindShadow, ledgerdiag.KindTrial, ledgerdiag.KindResearchModel, ledgerdiag.KindBaseline, ledgerdiag.KindLiveCohort}

// ErrInvalidArgument is what a failure of the arguments that needs the database to detect (a
// criterion looser than the stored setting) is wrapped in: the MCP maps it to invalid_argument.
var ErrInvalidArgument = fmt.Errorf("promotiondiag: invalid argument: %w", query.ErrBadCursor)

// Ref names a candidate or the current answer: a kind and the kind's id.
type Ref struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// ledgerID is the id the candidate ledger uses.
func (r Ref) ledgerID() string {
	if r.Kind == ledgerdiag.KindLive {
		return ledgerdiag.LiveID
	}
	return r.Kind + ":" + r.ID
}

// Request is the tool's arguments as decoded from the call: the period (From, To, as every
// period tool takes it) and the tool's own. Decoding rejects an unknown field and an
// argument Validate refuses.
type Request struct {
	From              *string  `json:"from"`
	To                *string  `json:"to"`
	Candidate         *Ref     `json:"candidate"`
	Current           *Ref     `json:"current"`
	Horizon           string   `json:"horizon"`
	USDJPY            *float64 `json:"usd_jpy"`
	NotionalJPY       *float64 `json:"notional_jpy"`
	FamilySizeExtra   int      `json:"family_size_extra"`
	MinCommonRows     *int     `json:"min_common_rows"`
	MinDays           *int     `json:"min_days"`
	MinDiffCILowerBps *float64 `json:"min_diff_ci_lower_bps"`
	MinPositiveDayPct *float64 `json:"min_positive_day_pct"`
	MaxAdjustedP      *float64 `json:"max_adjusted_p"`
}

// UnmarshalJSON decodes strictly and validates, so the dispatcher's decode of a
// bad call is an invalid_argument without a check of its own.
func (r *Request) UnmarshalJSON(b []byte) error {
	type plain Request
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode((*plain)(r)); err != nil {
		return err
	}
	return r.Validate()
}

// overrides are the per-call criteria by their settings key.
func (r Request) overrides() []struct {
	key string
	v   float64
} {
	type kv = struct {
		key string
		v   float64
	}
	var out []kv
	if r.MinCommonRows != nil {
		out = append(out, kv{promotionsettings.KeyMinCommonRows, float64(*r.MinCommonRows)})
	}
	if r.MinDays != nil {
		out = append(out, kv{promotionsettings.KeyMinDays, float64(*r.MinDays)})
	}
	if r.MinDiffCILowerBps != nil {
		out = append(out, kv{promotionsettings.KeyMinDiffCILowerBps, *r.MinDiffCILowerBps})
	}
	if r.MinPositiveDayPct != nil {
		out = append(out, kv{promotionsettings.KeyMinPositiveDayPct, *r.MinPositiveDayPct})
	}
	if r.MaxAdjustedP != nil {
		out = append(out, kv{promotionsettings.KeyMaxAdjustedP, *r.MaxAdjustedP})
	}
	return out
}

// Validate reports an argument the tool does not accept. A per-call criterion looser than the
// documented floor is refused here; one looser than a stricter stored setting is refused by Check.
// A kind or a combination that cannot be checked is an *UnsupportedError (unsupported_candidate_kind).
func (r Request) Validate() error {
	if r.Candidate == nil {
		return errors.New("promotiondiag: candidate {kind, id} is required")
	}
	if isSettingsSet(r.Candidate.Kind) {
		if err := r.validateSettingsSet(); err != nil {
			return err
		}
		return r.validateCommon()
	}
	if err := checkRef("candidate", *r.Candidate, candidateKinds); err != nil {
		return err
	}
	cur := Ref{Kind: ledgerdiag.KindLive, ID: ledgerdiag.LiveID}
	if r.Current != nil {
		cur = *r.Current
		if cur.Kind == ledgerdiag.KindLive && cur.ID != ledgerdiag.LiveID {
			return fmt.Errorf("promotiondiag: current of kind live has the id %q", ledgerdiag.LiveID)
		}
		if cur.Kind == ledgerdiag.KindLiveCohort {
			return unsupported(ReasonCohortAsCurrent)
		}
		if cur.Kind != ledgerdiag.KindLive {
			if err := checkRef("current", cur, candidateKinds); err != nil {
				return err
			}
		}
	}
	if r.Candidate.ledgerID() == cur.ledgerID() {
		return errors.New("promotiondiag: candidate and current are the same")
	}
	if err := r.ledgerRequest(cur).Validate(); err != nil {
		return err
	}
	return r.validateCommon()
}

// validateCommon checks the arguments every kind shares: the family size and the criteria.
func (r Request) validateCommon() error {
	if r.FamilySizeExtra < 0 || r.FamilySizeExtra > MaxFamilyExtra {
		return fmt.Errorf("promotiondiag: family_size_extra must be 0..%d", MaxFamilyExtra)
	}
	floor := promotionsettings.Floor()
	for _, o := range r.overrides() {
		if err := floor.Tighten(o.key, o.v); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidArgument, err)
		}
	}
	return nil
}

func checkRef(what string, ref Ref, kinds []string) error {
	if isSettingsSet(ref.Kind) { // a candidate of this kind never reaches here (Validate reads it from its experiment)
		return unsupported(ReasonSettingsSetAsCurrent)
	}
	if !slices.Contains(kinds, ref.Kind) {
		return fmt.Errorf("promotiondiag: %s kind %q is not one of %v", what, ref.Kind, kinds)
	}
	_, _, err := ledgerdiag.ParseID(ref.ledgerID())
	return err
}

// ledgerRequest is the ledger call the checklist reads its numbers from: every candidate of the
// period (so the Holm family is the real one), each against the current answer on its own common rows.
func (r Request) ledgerRequest(cur Ref) ledgerdiag.Request {
	req := ledgerdiag.Request{
		Horizon: r.Horizon, Alignment: ledgerdiag.AlignPairwise, USDJPY: r.USDJPY, NotionalJPY: r.NotionalJPY,
		FamilyExtra: r.FamilySizeExtra, MeansOnly: true,
	}
	if r.Candidate != nil {
		req.Include = []string{r.Candidate.ledgerID()}
	}
	if id := cur.ledgerID(); id != ledgerdiag.LiveID {
		req.Reference = id
	}
	return req
}

func (r Request) current() Ref {
	if r.Current == nil {
		return Ref{Kind: ledgerdiag.KindLive, ID: ledgerdiag.LiveID}
	}
	return *r.Current
}
