package pages

// The 注文案 screens take plain, already formatted props (docs/components/
// overview.md §3): internal/web/handler/orderproposals maps the read model of
// the Paper results MCP onto them, so a Templ file never imports a service.
// An empty string is "no value" and is shown as "—".

// Fixed codes of order_proposals.status, bucket and action with the names the
// operator sees. The code itself is shown next to the name so it can be told to
// the MCP client or searched for.
var (
	proposalStatusLabels = map[string]string{
		"received":        "受付済み",
		"processing":      "処理中",
		"waiting_session": "立会待ち",
		"submitted":       "発注済み（約定待ち）",
		"filled":          "約定",
		"closed":          "決済済み",
		"rejected":        "拒否",
		"expired":         "期限切れ",
		"cancelled":       "取消",
		"failed":          "失敗",
	}
	proposalBucketLabels = map[string]string{"stock": "株式", "bond_etf": "債券ETF"}
	proposalActionLabels = map[string]string{"open_long": "買い（新規）", "close": "決済"}
)

// ProposalOption is one choice of a filter <select>.
type ProposalOption struct {
	Value string
	Label string
}

// ProposalFilter echoes the list's filter form (dates are JST, YYYY-MM-DD).
type ProposalFilter struct {
	From, To, Status, Bucket string
}

// ProposalRow is one line of the list.
type ProposalRow struct {
	ID           int64
	ReceivedAt   string
	Symbol       string
	Bucket       string
	Action       string
	Status       string
	StatusReason string
	// Quantity is "提案 → アプリ" shares (or the budget for a budget proposal).
	Quantity string
	// MaxLoss is the proposer's figure; EstimatedLoss the application's.
	MaxLoss       string
	EstimatedLoss string
	// RealizedPnl is the linked position's realized pnl once it closed;
	// PnlSign is "gain", "loss" or "" for the colour.
	RealizedPnl string
	PnlSign     string
}

// ProposalListProps is the 一覧 page.
type ProposalListProps struct {
	Filter   ProposalFilter
	Statuses []ProposalOption
	Buckets  []ProposalOption
	Rows     []ProposalRow
	// NextHref is the link to the next page of the same filter, "" on the last.
	NextHref string
	// Errors are shown above the form (a bad date or filter value).
	Errors []string
	// LoadError is shown instead of the table when the read failed.
	LoadError string
}

// ProposalSource is one 情報源. Href is set only for an http or https URL; any
// other URL is shown as text and never becomes a link.
type ProposalSource struct {
	Title, URL, Href, RetrievedAt string
}

// ProposalEvent is one line of the 状態の時系列.
type ProposalEvent struct {
	At, From, To, Reason, Actor, Detail string
}

// ProposalLinked is a linked record (signal, order or position): a title and
// label/value pairs.
type ProposalLinked struct {
	Title  string
	Fields []ProposalField
}

// ProposalField is one label/value pair of the 詳細 screen.
type ProposalField struct {
	Label, Value string
	// Sign colours a pnl value: "gain", "loss" or "".
	Sign string
}

// ProposalDetailProps is the 詳細 page.
type ProposalDetailProps struct {
	ID           int64
	Symbol       string
	Bucket       string
	Action       string
	OrderType    string
	Status       string
	StatusReason string
	// Summary is the 概要 block: times, quantity, prices.
	Summary []ProposalField
	// Rationale and ExitPlan are free text; the template escapes them.
	Rationale string
	ExitPlan  string
	// Plan is 損切り・利確 and the stops that applied.
	Plan []ProposalField
	// Loss is 最大損失: the proposer's, the application's estimate and the
	// actual loss.
	Loss    []ProposalField
	Sources []ProposalSource
	Events  []ProposalEvent
	Linked  []ProposalLinked
}
