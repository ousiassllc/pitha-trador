package researchdiag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Description is the tool's description (docs/api/endpoints/paper-results-mcp-research-labels.md §3.34).
const Description = "研究用のホライズンラベル（30 分・60 分・引け）の読み取り専用の集計（取引の入力ではない）: 期間（JST）内のスナップショットについて、" +
	"その足の気配で入って 30 分後・60 分後・同日の引け（15:25 JST 以降の最後の足）で決済したときの bps を、コスト前（mean_gross_bps）・" +
	"既存ラベルが置くコストの控除後（mean_net_bps。コスト＝設定 calibration.barrier_cost_bps と cost.* から記録時に決めた値）で、買い・売り別に返す。" +
	"件数・日数・勝率（net>0）・中央値、日単位のブートストラップの区間（mean_net_ci）と検出できる最小差（detectable_minimum_bps。α=0.05 両側・検出力 0.8 の記述で、採用のしきい値ではない）。" +
	"cost_vs_spread に、ラベルが置くコストと実際のスプレッド（その足の spread_bps）の平均・中央値、gross から spread/2 を引いた net_after_spread_bps、" +
	"スプレッドがコストを超えた行の割合。coverage に期間内のスナップショット（または判断）と記録済みの行の数・日別の記録状況、recorded_through に記録済みの最新の日。" +
	"scope: snapshots（既定。全スナップショット）/ trader / scout（その判断のあった足。判断自身の方向で集計し、NONE は除く。direction で LONG/SHORT に絞れる）。" +
	"ラベル・学習データ・取引は変えない。しきい値の判定・採用の推奨は返さない"

// SchemaProperties are the tool's own arguments besides the period.
func SchemaProperties() map[string]any {
	return map[string]any{
		"scope": map[string]any{
			"type": "string", "enum": []string{ScopeSnapshots, ScopeTrader, ScopeScout}, "default": ScopeSnapshots,
			"description": "snapshots: 全スナップショット（買い・売りの両方を返す）/ trader・scout: その判断があった足を、判断自身の方向（NONE は除く）で集計",
		},
		"direction": map[string]any{
			"type": "string", "enum": []string{domain.JevDirectionLong, domain.JevDirectionShort},
			"description": "trader / scout のとき判断の方向で絞る。scope が snapshots のときは指定できない（両側を返す）",
		},
	}
}

// Request is the tool's arguments as decoded from the call: the period (From,
// To, as every period tool takes it) and the tool's own. Decoding rejects an
// unknown field and an argument Validate refuses.
type Request struct {
	From      *string `json:"from"`
	To        *string `json:"to"`
	Scope     string  `json:"scope"`
	Direction string  `json:"direction"`
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
	switch r.Scope {
	case "", ScopeSnapshots, ScopeTrader, ScopeScout:
	default:
		return fmt.Errorf("researchdiag: scope %q is not snapshots, trader or scout", r.Scope)
	}
	switch r.Direction {
	case "":
	case domain.JevDirectionLong, domain.JevDirectionShort:
		if r.scope() == ScopeSnapshots {
			return fmt.Errorf("researchdiag: direction applies to the trader and scout scopes only; the snapshots scope reports both sides")
		}
	default:
		return fmt.Errorf("researchdiag: direction %q is not LONG or SHORT", r.Direction)
	}
	return nil
}

func (r Request) scope() string {
	if r.Scope == "" {
		return ScopeSnapshots
	}
	return r.Scope
}

// Run summarizes the research labels of [r.From, r.To) in the scope of req. now
// only decides which JST days are complete (before today). It returns the data
// and the number of label rows the statistics are over.
func Run(ctx context.Context, q query.Q, r query.Range, now time.Time, req Request) (any, int, error) {
	d, rows, err := Analyze(ctx, q, r, req, now)
	return d, rows, err
}
