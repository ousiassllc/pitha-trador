package settings

import "github.com/ousiassllc/pitha-trador/internal/config/costsettings"

// costGroup holds the trading cost assumption (issue #779): the one set Paper
// fills, the backtest and the triple-barrier label cost (hence the
// meta-labeling EV) all read.
var costGroup = opsGroup{
	id:          "trading_cost",
	name:        "売買コストの想定",
	description: "手数料・スリッページ・SHORT の貸株料の想定です。Paper の約定価格と手数料、バックテストの損益、メタラベリングのラベル（EV）が、すべてこの同じ値を参照します。",
	note: "既定は手数料 0、スリッページ ザラ場 2 bps／寄り・引け 5 bps、貸株料 0 です。保存した値は再起動不要で、次の約定・次のバックテスト・以降に付くラベルから反映されます（付与済みのラベルは変わりません）。" +
		"ラベルのコストは「トリプルバリアのコスト想定」に、ここでの既定値からの往復コストの差を足した値になります。" +
		"スプレッドは板から別に反映されるため、ここには含みません。実際の手数料体系・貸株料はご自身で確認してください。環境変数では設定できません。",
	fields: []opsField{
		{key: costsettings.KeyFeeBps, label: "手数料（約定代金に対する bps、片道）", hint: "0 以上 1000 以下です。国内株式現物の手数料無料を前提に既定は 0 です。"},
		{key: costsettings.KeyFeeFixedJPY, label: "手数料の定額（円、1 約定あたり）", hint: "0 以上 100000 以下です。既定は 0 円です。"},
		{key: costsettings.KeySlippageLongContinuous, label: "LONG スリッページ ザラ場（bps）", hint: "板の最良気配を超えて不利になる分です。既定は 2 bps です。"},
		{key: costsettings.KeySlippageLongOpening, label: "LONG スリッページ 寄り（bps）", hint: "既定は 5 bps です。"},
		{key: costsettings.KeySlippageLongClosing, label: "LONG スリッページ 引け（bps）", hint: "既定は 5 bps です。"},
		{key: costsettings.KeySlippageShortContinuous, label: "SHORT スリッページ ザラ場（bps）", hint: "既定は 2 bps です。"},
		{key: costsettings.KeySlippageShortOpening, label: "SHORT スリッページ 寄り（bps）", hint: "既定は 5 bps です。"},
		{key: costsettings.KeySlippageShortClosing, label: "SHORT スリッページ 引け（bps）", hint: "既定は 5 bps です。"},
		{key: costsettings.KeyShortBorrowBps, label: "SHORT 貸株料（建玉の約定代金に対する bps、1 建玉あたり）", hint: "信用売りの貸株料の想定です。日跨ぎをしないため逆日歩・金利は扱いません。既定は 0 bps です。"},
		{key: costsettings.KeyReferenceNotionalJPY, label: "基準の約定代金（円）", hint: "定額手数料を bps に換算するときの 1 約定の大きさです（バックテスト・ラベル用）。1 以上です。既定は 1000000 円です。"},
	},
}
