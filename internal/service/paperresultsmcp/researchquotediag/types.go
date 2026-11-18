package researchquotediag

// SourceStats is what one source holds for a JST day (or in total).
type SourceStats struct {
	Rows     int `json:"rows"`
	Symbols  int `json:"symbols"`
	Minutes  int `json:"minutes"`
	ViaEvent int `json:"via_event,omitempty"`
	ViaREST  int `json:"via_rest,omitempty"`
}

// Gap is the price gap between the two sources over the paired minutes, in bps
// of kabu's price ((立花 - kabu) / kabu). The mean, median, 95th percentile
// (nearest rank) and maximum are of the absolute gap; Signed is the plain mean
// (positive: 立花 above kabu). The shares are of the pairs whose absolute gap is
// above 5, 10 and 50 bps. All are null without a pair.
type Gap struct {
	Pairs        int      `json:"pairs"`
	MeanAbsBps   *float64 `json:"mean_abs_bps"`
	MedianBps    *float64 `json:"median_abs_bps"`
	P95AbsBps    *float64 `json:"p95_abs_bps"`
	MaxAbsBps    *float64 `json:"max_abs_bps"`
	MeanBps      *float64 `json:"mean_signed_bps"`
	ShareAbove5  *float64 `json:"share_above_5bps"`
	ShareAbove10 *float64 `json:"share_above_10bps"`
	ShareAbove50 *float64 `json:"share_above_50bps"`
}

// Comparison counts the symbol-minutes of the two sources and the gap over the
// minutes both have.
type Comparison struct {
	MinutesBoth   int `json:"minutes_both"`
	OnlyKabu      int `json:"only_kabu"`
	OnlyTachibana int `json:"only_tachibana"`
	Gap           Gap `json:"gap"`
}

// PhaseComparison is Comparison of one session phase.
type PhaseComparison struct {
	Phase string `json:"phase"`
	Comparison
}

// Day is one JST day on which 立花 recorded samples.
type Day struct {
	Date       string      `json:"date"`
	Tachibana  SourceStats `json:"tachibana"`
	Kabu       SourceStats `json:"kabu"`
	Comparison Comparison  `json:"comparison"`
}

// Status is research_tachibana_status of the quotes component.
type Status struct {
	State     string  `json:"state"`
	Detail    string  `json:"detail"`
	UpdatedAt *string `json:"updated_at"`
}

// Scan says how much of the period was read.
type Scan struct {
	// RowsRead counts the rows of both sources the comparison read.
	RowsRead int `json:"rows_read"`
	RowCap   int `json:"row_cap"`
	// Complete is false when the row cap or the time budget stopped the scan:
	// the days after CoveredThrough are then missing, not zero.
	Complete       bool    `json:"complete"`
	CoveredThrough *string `json:"covered_through"`
	Note           string  `json:"note"`
}

// Data is the tool's result.
type Data struct {
	Status Status  `json:"status"`
	Symbol *string `json:"symbol"`
	// Tachibana and Kabu are the totals of the days listed in Days.
	Tachibana   SourceStats       `json:"tachibana"`
	Kabu        SourceStats       `json:"kabu"`
	Comparison  Comparison        `json:"comparison"`
	ByPhase     []PhaseComparison `json:"by_phase"`
	Days        []Day             `json:"days"`
	Scan        Scan              `json:"scan"`
	Definitions []string          `json:"definitions"`
	Notice      string            `json:"notice"`
}
