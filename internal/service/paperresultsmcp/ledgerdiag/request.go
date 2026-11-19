package ledgerdiag

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/evaldiag"
)

// Candidate kinds. KindLive is the reference itself ("live"); it is a candidate only
// when the promotion checklist takes another candidate as its reference.
const (
	KindLive          = "live"
	KindLiveCohort    = "live_cohort"
	KindShadow        = "shadow"
	KindTrial         = "trial"
	KindBaseline      = "baseline"
	KindResearchModel = "research_model"
)

// LiveID is the id of the live reference.
const LiveID = "live"

// Baselines are the baseline arms in output order (the arms of jev_evaluation without llm).
var Baselines = []string{evaldiag.ArmRandom, evaldiag.ArmMomentum, evaldiag.ArmContrarian, evaldiag.ArmNone, evaldiag.ArmLongLeg, evaldiag.ArmShortLeg}

// researchHorizons are the label horizons of the research model (and of research_horizon_labels).
var researchHorizons = []string{Horizon30m, Horizon60m, HorizonClose}

// maxArmIDLen is the width of jev_trial_decisions.arm_id; maxAlgorithmLen of research_model_runs.algorithm.
const (
	maxArmIDLen     = 48
	maxAlgorithmLen = 20
)

// Request is the tool's arguments as decoded from the call: the period (From, To,
// as every period tool takes it) and the tool's own. Decoding rejects an unknown
// field and an argument Validate refuses. Reference and FamilyExtra are not
// arguments of the tool (json "-"): the promotion checklist sets them.
type Request struct {
	From        *string  `json:"from"`
	To          *string  `json:"to"`
	Horizon     string   `json:"horizon"`
	Alignment   string   `json:"alignment"`
	Candidates  []string `json:"candidates"`
	USDJPY      *float64 `json:"usd_jpy"`
	NotionalJPY *float64 `json:"notional_jpy"`
	ModelID     string   `json:"model_id"`

	// Reference is the candidate id the differences are taken against: "" or LiveID is the
	// live reference.
	Reference string `json:"-"`
	// Include are candidates listed even when the period has no row of them (candidates is empty):
	// the promotion checklist's own candidate.
	Include []string `json:"-"`
	// MeansOnly skips the candidates' own bootstrap interval (their means are still reported): the
	// promotion checklist reads only the differences' intervals.
	MeansOnly bool `json:"-"`
	// FamilyExtra widens the multiple-comparison family by that many comparisons made elsewhere.
	FamilyExtra int `json:"-"`
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

// Validate reports an argument the tool does not accept.
func (r Request) Validate() error {
	if !slices.Contains([]string{"", Horizon10m, Horizon30m, Horizon60m, HorizonClose}, r.Horizon) {
		return fmt.Errorf("ledgerdiag: horizon %q is not 10m, 30m, 60m or close", r.Horizon)
	}
	if !slices.Contains([]string{"", AlignCommonAll, AlignPairwise}, r.Alignment) {
		return fmt.Errorf("ledgerdiag: alignment %q is not common_all or pairwise", r.Alignment)
	}
	if len(r.Candidates) > MaxCandidates {
		return fmt.Errorf("ledgerdiag: candidates lists at most %d ids", MaxCandidates)
	}
	seen := map[string]bool{}
	for _, id := range r.Candidates {
		if _, _, err := ParseID(id); err != nil {
			return err
		}
		if id == LiveID {
			return fmt.Errorf("ledgerdiag: %q is the reference, not a candidate", id)
		}
		if seen[id] {
			return fmt.Errorf("ledgerdiag: candidate %q is listed twice", id)
		}
		seen[id] = true
	}
	for name, v := range map[string]*float64{"usd_jpy": r.USDJPY, "notional_jpy": r.NotionalJPY} {
		if v != nil && (!(*v > 0) || math.IsInf(*v, 0)) {
			return fmt.Errorf("ledgerdiag: %s must be a positive number", name)
		}
	}
	if len(r.ModelID) > evaldiag.MaxModelIDLen {
		return fmt.Errorf("ledgerdiag: model_id is at most %d characters", evaldiag.MaxModelIDLen)
	}
	if r.FamilyExtra < 0 {
		return fmt.Errorf("ledgerdiag: family extra must not be negative")
	}
	if r.Reference != "" {
		if _, _, err := ParseID(r.Reference); err != nil {
			return err
		}
	}
	return nil
}

func (r Request) horizon() string {
	if r.Horizon == "" {
		return Horizon10m
	}
	return r.Horizon
}

func (r Request) alignment() string {
	if r.Alignment == "" {
		return AlignCommonAll
	}
	return r.Alignment
}

func (r Request) reference() string {
	if r.Reference == "" {
		return LiveID
	}
	return r.Reference
}

// costConversion reports whether both conversion arguments are present.
func (r Request) costConversion() (usdJPY, notionalJPY float64, ok bool) {
	if r.USDJPY == nil || r.NotionalJPY == nil {
		return 0, 0, false
	}
	return *r.USDJPY, *r.NotionalJPY, true
}

// ParseID splits a candidate id into its kind and key: live; live_cohort:<model_id>/<version>;
// shadow:<prompt_version>; trial:<arm_id>; baseline:<arm>; research_model:<algorithm>:<horizon>.
// It checks only the id's shape, not that the candidate exists.
func ParseID(id string) (kind, key string, err error) {
	if id == LiveID {
		return KindLive, "", nil
	}
	if len(id) > MaxIDLen {
		return "", "", fmt.Errorf("ledgerdiag: candidate id is at most %d characters", MaxIDLen)
	}
	kind, key, _ = strings.Cut(id, ":")
	switch kind {
	case KindShadow:
		if err := jevshadowsettings.ValidateVersion(key); err != nil {
			return "", "", fmt.Errorf("ledgerdiag: candidate %q: %w", id, err)
		}
	case KindTrial:
		if key == "" || len(key) > maxArmIDLen || hasSpaceOrControl(key) {
			return "", "", fmt.Errorf("ledgerdiag: candidate %q: an arm id is 1〜%d characters without spaces", id, maxArmIDLen)
		}
	case KindBaseline:
		if !slices.Contains(Baselines, key) {
			return "", "", fmt.Errorf("ledgerdiag: candidate %q: a baseline is one of %s", id, strings.Join(Baselines, ", "))
		}
	case KindResearchModel:
		algorithm, horizon, ok := strings.Cut(key, ":")
		if !ok || algorithm == "" || len(algorithm) > maxAlgorithmLen || hasSpaceOrControl(algorithm) || !slices.Contains(researchHorizons, horizon) {
			return "", "", fmt.Errorf("ledgerdiag: candidate %q: expected research_model:<algorithm>:<30m|60m|close>", id)
		}
	case KindLiveCohort:
		i := strings.LastIndex(key, "/")
		if i <= 0 || i > evaldiag.MaxModelIDLen || jevshadowsettings.ValidateVersion(key[i+1:]) != nil {
			return "", "", fmt.Errorf("ledgerdiag: candidate %q: expected live_cohort:<model_id>/<version>", id)
		}
	default:
		return "", "", fmt.Errorf("ledgerdiag: candidate %q has an unknown kind", id)
	}
	return kind, key, nil
}

func hasSpaceOrControl(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) >= 0
}
