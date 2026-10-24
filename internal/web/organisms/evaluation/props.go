package evaluation

// Cell is one metric of a table: Text is its display ("12.3% [8.1–16.5]" or
// "判定不能"), Undetermined marks the latter (shown muted), Help the text of
// its help mark (the reference value and why of a 判定不能 metric); empty
// shows no mark.
type Cell struct {
	Text         string
	Undetermined bool
	Help         string
}

// Row is one slice of a metrics table.
type Row struct {
	Key                                               string
	N                                                 int
	NetPnL                                            string
	WinRate, EVJPY, EVBps, ProfitFactor, DDJPY, DDPct Cell
}

// Table is a metrics table over one dimension.
type Table struct {
	ID        string
	Title     string
	KeyHeader string
	Rows      []Row
}

// Check is one line of the 合格ライン.
type Check struct {
	ID       string
	Label    string
	Required string
	Actual   string
	// Status is "pass", "fail" or "undetermined"; StatusText its label.
	Status     string
	StatusText string
}

// Pair is a label/value line. ID is unique in the list (the suffix of the
// help mark's id); Help, when non-empty, is the help text of the label.
type Pair struct{ ID, Label, Value, Help string }

// JevRow is Jev's hit rate and Brier score of one calibration label version.
type JevRow struct {
	Label          string
	N              int
	HitRate, Brier Cell
}

// MetaRow sets one meta model's OOS metrics beside its realized Paper EV.
type MetaRow struct {
	Version, Status, OOSWindow string
	OOSSamples                 string
	OOSAUC, OOSEV              Cell
	LinkedN                    int
	PredictedEV, RealizedEV    Cell
	EVGap                      Cell
	WindowN                    int
	WindowEV                   Cell
}

// Panel is Panel's props.
type Panel struct {
	// Verdict is "pass", "fail" or "undetermined"; VerdictText its label.
	Verdict     string
	VerdictText string
	Checks      []Check
	// Capital is "初期資金 ¥30,000,000" (or the unset note); CapitalNote
	// reminds that Paper's is a placeholder (empty when unset).
	Capital     string
	CapitalNote string
	Criteria    string
	Overall     Row
	Costs       []Pair
	Tables      []Table
	Jev         []JevRow
	Meta        []MetaRow
}
