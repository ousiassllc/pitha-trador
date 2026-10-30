// Package disclosureskipdiag is the read-only analysis behind the Paper
// results MCP's disclosure_skip_summary tool (issue #935): for the Jev
// entry-candidate decisions of a period, how many the 適時開示 skip rule would
// have skipped, not skipped or could not judge (the records the application's
// disclosure skip recorder stored, internal/service/disclosureskip), and how the
// outcome labels of each group came out. It writes nothing and changes no
// decision: the rule is recorded, never applied.
package disclosureskipdiag

// Rule says what the verdicts mean; it is returned so a reader of the numbers
// does not need the specification at hand.
type Rule struct {
	Versions []int  `json:"rule_versions"`
	Skip     string `json:"skip"`
	NoSkip   string `json:"no_skip"`
	Unknown  string `json:"unknown"`
}

// PromptScope is the Trader prompt versions the counts cover (as
// entry_quality_whatif's): Only when one version was asked for, otherwise every
// version but Excluded (the versions of another cohort than the live one).
type PromptScope struct {
	Only     string   `json:"only,omitempty"`
	Excluded []string `json:"excluded"`
}

// Population is the period's entry-candidate decisions: the live Jev trader
// decisions with direction LONG or SHORT. EntryCandidates = Recorded +
// NotRecorded; a decision is not recorded while it waits for a covering fetch
// (the first 60 minutes), when the application was not running, or when the
// recorder is off.
type Population struct {
	EntryCandidates int `json:"entry_candidates"`
	Recorded        int `json:"recorded"`
	NotRecorded     int `json:"not_recorded"`
}

// NetReturn summarizes the net returns (bps, after the label's trading cost) of
// the labeled decisions of a group. Null while nothing in the group is labeled.
type NetReturn struct {
	Mean float64 `json:"mean_bps"`
	Sum  float64 `json:"sum_bps"`
}

// Group is the decisions of one verdict. Count = Labeled + Unlabeled. Wins are
// the labeled decisions with a net return above zero; WinRate = Wins / Labeled
// (the definition of entry_quality_whatif's outcomes and of the meta-model's
// training), null while nothing is labeled.
type Group struct {
	Count     int        `json:"count"`
	Labeled   int        `json:"labeled"`
	Unlabeled int        `json:"unlabeled"`
	Wins      int        `json:"wins"`
	WinRate   *float64   `json:"win_rate"`
	NetReturn *NetReturn `json:"net_return"`
}

// Verdicts are the groups of the three verdicts.
type Verdicts struct {
	Skip    Group `json:"skip"`
	NoSkip  Group `json:"no_skip"`
	Unknown Group `json:"unknown"`
}

// Bucket is a count under a fixed code.
type Bucket struct {
	Code  string `json:"code"`
	Count int    `json:"count"`
}

// Labels says how the outcome labels were chosen.
type Labels struct {
	LabelVersion int    `json:"label_version"`
	Selection    string `json:"selection"`
	Win          string `json:"win"`
}

// Data is disclosure_skip_summary's data.
type Data struct {
	RowsFrom       string      `json:"rows_from"`
	RowsTo         string      `json:"rows_to"`
	PromptVersions PromptScope `json:"prompt_versions"`
	Rule           Rule        `json:"rule"`
	Population     Population  `json:"population"`
	Verdicts       Verdicts    `json:"verdicts"`
	// UnknownReasons splits Verdicts.Unknown by why (no_fetch, incomplete_fetch).
	UnknownReasons []Bucket `json:"unknown_reasons"`
	// SkipKinds counts the skip decisions per kind of matching disclosure (a
	// decision with matches of both kinds is in both).
	SkipKinds []Bucket `json:"skip_kinds"`
	Labels    Labels   `json:"labels"`
}

// Args are the tool's own arguments.
type Args struct {
	// PromptVersion restricts the counts to one Trader prompt version; empty is
	// the live version's cohort.
	PromptVersion string
	// Foreign are the prompt versions left out when PromptVersion is empty
	// (jev.ForeignVersionsOfLive).
	Foreign []string
}
