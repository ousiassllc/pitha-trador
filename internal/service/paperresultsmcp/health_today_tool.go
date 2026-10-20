package paperresultsmcp

import (
	"context"
	"encoding/json"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// healthTodayTool is the one-call health check of today (issue #808). It takes
// no arguments: "today" is the JST day of the server clock, and every part is
// read at that moment.
var healthTodayTool = tool{
	name: "health_today",
	description: "今日（JST・取引日判定つき）の健康診断を1回で返す: Paper取引数、メタモデル採用の有無、" +
		"ブートストラップの設定と有効状態、kabuログイン・market-dataの稼働状態（アプリ内警告と同じ固定コード）、" +
		"直近の欠損セッション、日次ガード、Kill Switch、再学習の見送り、そして要対応フラグと固定コード（読み取り専用）",
	schema: map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false},
	decode: func(raw json.RawMessage) (periodArgs, runFunc, *toolError) {
		var none struct{}
		if err := strictDecode(raw, &none); err != nil {
			return periodArgs{}, nil, err
		}
		return periodArgs{}, func(ctx context.Context, e toolEnv) (any, int, error) {
			h, err := query.HealthToday(ctx, e.q, e.now)
			return h, len(h.AttentionCodes), err
		}, nil
	},
}
