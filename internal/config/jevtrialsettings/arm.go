package jevtrialsettings

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
)

// Providers an arm can name. Stage 1 is OpenRouter only (Cloudflare directly,
// Scout and Luna come later).
const ProviderOpenRouter = "openrouter"

// Values of Arm.DataCollection (OpenRouter's provider.data_collection).
const (
	DataCollectionDeny  = "deny"
	DataCollectionAllow = "allow"
)

// MaxArms bounds the number of arms.
const MaxArms = 8

// MaxModelLen bounds a model id.
const MaxModelLen = 100

// MaxListPriceUSDPerMtok bounds a list price, so a typo cannot hide a cost.
const MaxListPriceUSDPerMtok = 1000.0

// WorstCaseUSDPerMtok is the price per million input tokens a call whose cost is
// unknown (no reported cost, no list price) is counted at against the daily caps:
// the highest list price of the stage-1 models (cloudflare/clef, 0.24 USD per
// million input tokens, as published on 2026-11-13). It is a counting rule for the
// caps, not a measured price.
const WorstCaseUSDPerMtok = 0.24

// armID is an arm id: it ends up in runtime_settings keys, so it is lower-case
// letters, digits, dot, underscore and hyphen, 1 to 48 characters.
var armID = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,47}$`)

// Arm is one model the trial asks. It is the minimal shape (a string arm id and
// a JSON configuration) that issue #1010's arms can be mapped onto later.
type Arm struct {
	// ID names the arm in jev_trial_decisions.arm_id and in the per-arm keys,
	// e.g. "or-clef-flash-v3".
	ID string `json:"id"`
	// Provider is ProviderOpenRouter.
	Provider string `json:"provider"`
	// Model is the model id sent, e.g. "cloudflare/clef-flash".
	Model string `json:"model"`
	// PromptVersion is a registered Trader prompt version (e.g. "trader-v3").
	PromptVersion string `json:"prompt_version"`
	// DailyCostCapUSD is the arm's own daily cap (USD per JST day); omitted or 0
	// means DefaultArmDailyCostCapUSD.
	DailyCostCapUSD float64 `json:"daily_cost_cap_usd,omitempty"`
	// ListPriceUSDPerMtok is the list price per million input tokens used to
	// estimate a call whose response reports no cost. 0 or omitted: unknown (such
	// a call is counted at WorstCaseUSDPerMtok and marked unknown).
	ListPriceUSDPerMtok float64 `json:"list_price_usd_per_mtok,omitempty"`
	// ProviderOnly restricts OpenRouter routing to these providers
	// (provider.only); with it set, fallbacks are disabled.
	ProviderOnly []string `json:"provider_only,omitempty"`
	// DataCollection is "deny" (default) or "allow".
	DataCollection string `json:"data_collection,omitempty"`
	// ZDR restricts routing to Zero Data Retention endpoints.
	ZDR bool `json:"zdr,omitempty"`
}

// CapUSD is the arm's effective daily cost cap.
func (a Arm) CapUSD() float64 {
	if a.DailyCostCapUSD > 0 {
		return a.DailyCostCapUSD
	}
	return DefaultArmDailyCostCapUSD
}

// DataCollectionOrDefault is DataCollection, "deny" when empty.
func (a Arm) DataCollectionOrDefault() string {
	if a.DataCollection == "" {
		return DataCollectionDeny
	}
	return a.DataCollection
}

// Validate checks one arm's shape. That PromptVersion is registered is checked
// where the registry is (internal/service/jev).
func (a Arm) Validate() error {
	if !armID.MatchString(a.ID) {
		return fmt.Errorf("jevtrialsettings: arm id %q must be 1〜48 characters of a-z, 0-9, '.', '_' or '-'", a.ID)
	}
	if a.Provider != ProviderOpenRouter {
		return fmt.Errorf("jevtrialsettings: arm %q: provider must be %q", a.ID, ProviderOpenRouter)
	}
	if a.Model == "" || len(a.Model) > MaxModelLen {
		return fmt.Errorf("jevtrialsettings: arm %q: model must be 1〜%d characters", a.ID, MaxModelLen)
	}
	if err := jevshadowsettings.ValidateVersion(a.PromptVersion); err != nil {
		return fmt.Errorf("jevtrialsettings: arm %q: %w", a.ID, err)
	}
	if a.DailyCostCapUSD != 0 {
		if err := ValidateCap(a.DailyCostCapUSD); err != nil {
			return fmt.Errorf("jevtrialsettings: arm %q: daily_cost_cap_usd: %w", a.ID, err)
		}
	}
	if p := a.ListPriceUSDPerMtok; math.IsNaN(p) || math.IsInf(p, 0) || p < 0 || p > MaxListPriceUSDPerMtok {
		return fmt.Errorf("jevtrialsettings: arm %q: list_price_usd_per_mtok must be 0〜%g", a.ID, MaxListPriceUSDPerMtok)
	}
	if slices.Contains(a.ProviderOnly, "") {
		return fmt.Errorf("jevtrialsettings: arm %q: provider_only must not contain an empty name", a.ID)
	}
	if dc := a.DataCollection; dc != "" && dc != DataCollectionDeny && dc != DataCollectionAllow {
		return fmt.Errorf("jevtrialsettings: arm %q: data_collection must be %q or %q", a.ID, DataCollectionDeny, DataCollectionAllow)
	}
	return nil
}

// ParseArms decodes and validates the runtime_settings value of KeyArms: a JSON
// array of at most MaxArms arms with distinct ids. An empty array is valid (no
// arm). Unknown fields are refused so a misspelt key is not silently dropped.
func ParseArms(jsonValue string) ([]Arm, error) {
	var arms []Arm
	dec := json.NewDecoder(strings.NewReader(jsonValue))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&arms); err != nil {
		return nil, fmt.Errorf("jevtrialsettings: decode %s as a JSON array of arms: %w", KeyArms, err)
	}
	if len(arms) > MaxArms {
		return nil, fmt.Errorf("jevtrialsettings: %s holds %d arms, at most %d", KeyArms, len(arms), MaxArms)
	}
	seen := map[string]bool{}
	for _, a := range arms {
		if err := a.Validate(); err != nil {
			return nil, err
		}
		if seen[a.ID] {
			return nil, fmt.Errorf("jevtrialsettings: arm id %q is used twice", a.ID)
		}
		seen[a.ID] = true
	}
	return arms, nil
}

// EncodeArms validates arms and renders them as the stored JSON array ("[]" for
// none).
func EncodeArms(arms []Arm) (string, error) {
	if arms == nil {
		arms = []Arm{}
	}
	b, err := json.Marshal(arms)
	if err != nil {
		return "", fmt.Errorf("jevtrialsettings: encode %s: %w", KeyArms, err)
	}
	if _, err := ParseArms(string(b)); err != nil {
		return "", err
	}
	return string(b), nil
}
