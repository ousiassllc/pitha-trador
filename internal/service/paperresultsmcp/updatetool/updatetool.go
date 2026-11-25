// Package updatetool declares the update tools of the Paper results MCP
// (issue #1222; docs/api/endpoints/update-tools/paper-results-mcp-update.md) as
// tooldef.Defs: update_status reads the desktop application's self-updater,
// update_check_now asks it to check GitHub Releases now, update_apply asks it
// to install a release that check already downloaded and verified.
//
// The MCP is a separate process and holds no updater. The tools only read the
// status the application publishes and record requests (internal/repository/
// updatecontrol); the application (internal/service/updater/mcpbridge) runs
// them through the existing Checker.CheckForUpdate and QuitForUpdate, so the
// signature and checksum checks, the URL and size limits, SafeGate (evaluated
// again right before the install) and the dev-build skip stay where they are.
// No argument can pick a version, a URL, an asset, or skip SafeGate or the
// signature check: the schemas take no arguments at all.
package updatetool

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/updatecontrol"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/tooldef"
	"github.com/ousiassllc/pitha-trador/internal/service/updater"
	"github.com/ousiassllc/pitha-trador/internal/version"
)

const (
	// pollInterval is how often a waiting tool re-reads its request.
	pollInterval = 100 * time.Millisecond
	// settleBudget is how long a withdrawn or claimed apply request may still take to settle.
	settleBudget = time.Second
)

// waitBudget is how long a tool waits for the application to answer its
// request; the tool call itself is bounded by the server's 3 s budget.
var waitBudget = 2 * time.Second

// Defs are the update tools.
func Defs() []tooldef.Def {
	return []tooldef.Def{statusDef(), checkNowDef(), applyDef()}
}

func statusDef() tooldef.Def {
	return tooldef.Def{
		Name: "update_status",
		Description: "デスクトップアプリの自動更新の状態（読み取り専用）。今のアプリの版 current_version、最後に見た最新リリース latest_version、" +
			"available / blocked / ready / dev_build / no_release、導入を止めている条件の種別 blocked_kind（SafeGate の open_positions / kill_switch / recent_order / check_failed と、取引日の 9:00〜15:30 の market_hours）と理由 blocked_reason（Settings と同じ文言）、" +
			"最終確認時刻 checked_at、失敗の種別 error_kind と理由 error_reason、直近の update_apply の結果 last_apply を返す。" +
			"アップデーターが無いビルド、またはアプリが動いていないときは unavailable=true（エラーにはならない）。生のエラー文・パス・URL・鍵は出さない",
		Schema:   tooldef.ObjectSchema(map[string]any{}),
		NoPeriod: true,
		Decode: noArgs(func(ctx context.Context, e tooldef.Env) (any, int, error) {
			v, err := readView(ctx, e.Q, e.Now)
			return v, 1, err
		}),
	}
}

func checkNowDef() tooldef.Def {
	return tooldef.Def{
		Name: "update_check_now",
		Description: "GitHub Releases に対する即時確認（スケジューラーの周期確認と同じ Checker.CheckForUpdate。周期確認・Settings の手動確認と同時には走らない）。" +
			"確認が終わるまで最大 2 秒待ち、update_status と同じ形の状態に check（completed / failed / running / expired）を足して返す。running はまだ確認中（update_status で後から見る）。" +
			"このツールはアプリの終了も導入もしない（導入は update_apply だけ）。dev ビルドは今どおり確認をスキップし dev_build=true を返す",
		Schema:   tooldef.ObjectSchema(map[string]any{}),
		Writes:   true,
		NoPeriod: true,
		Decode:   noArgs(runCheckNow),
	}
}

func applyDef() tooldef.Def {
	return tooldef.Def{
		Name: "update_apply",
		Description: "check 済み（update_check_now で署名とチェックサムを検証したインストーラーを取得済み）の新しい版を、アプリの既存の導入手順（QuitForUpdate）で導入する依頼。" +
			"書き込み許可（settings_write_enable）がオンのときだけ動き、オフなら write_disabled で何もしない。" +
			"導入の直前に SafeGate（ポジションなし・Kill Switch なし・直近 5 分の発注なし）を再評価し、さらに取引日の東証の立会時間帯（JST 9:00〜15:30。昼休みを含む）は導入せず、保留なら result=deferred と reason（open_positions / kill_switch / recent_order / check_failed / market_hours）、" +
			"検証済みのインストーラーが無ければ result=deferred reason=not_ready（先に update_check_now）を返し、どちらもアプリを終了しない。通ったときだけ result=accepted を返し、" +
			"応答のあとアプリは通常の終了手順に入り（MCP は先に止まる）、インストーラーが起動する。依頼は actor=mcp と client を付けて監査に残る。" +
			"版・URL・アセットの指定、SafeGate や署名検証を飛ばす引数は無い（ダウングレードもできない）",
		Schema:   tooldef.ObjectSchema(map[string]any{}),
		Writes:   true,
		NoPeriod: true,
		Decode:   noArgs(runApply),
	}
}

// noArgs is the Decode of a tool that takes no argument: anything else is invalid_argument.
func noArgs(run tooldef.Run) func(json.RawMessage) (tooldef.PeriodArgs, tooldef.Run, *tooldef.Failure) {
	return func(raw json.RawMessage) (tooldef.PeriodArgs, tooldef.Run, *tooldef.Failure) {
		if f := tooldef.NoArgs(raw); f != nil {
			return tooldef.PeriodArgs{}, nil, f
		}
		return tooldef.PeriodArgs{}, run, nil
	}
}

// wait polls request id on its own read handle until the application finished
// it or budget ends (one last read at the end), and returns the last state read.
func wait(ctx context.Context, q query.Q, id int64, budget time.Duration) (updatecontrol.Request, error) {
	deadline := time.NewTimer(budget)
	defer deadline.Stop()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		// A fresh statement per poll: the handle is autocommit, so it sees the application's commits.
		req, err := updatecontrol.GetRequest(ctx, q, id)
		if err != nil || req.Done() {
			return req, err
		}
		select {
		case <-ctx.Done():
			return req, ctx.Err()
		case <-deadline.C:
			return updatecontrol.GetRequest(ctx, q, id)
		case <-ticker.C:
		}
	}
}

// freshRead opens the call's own read handle (the call's transaction holds the
// server's single read connection, and its snapshot cannot see the answer).
func freshRead(e tooldef.Env) (query.Q, func(), error) {
	if e.FreshRead == nil {
		return nil, nil, errors.New("updatetool: no read handle")
	}
	return e.FreshRead()
}

// enqueue records a request of kind in one write transaction. gated puts it behind the write switch.
func enqueue(ctx context.Context, e tooldef.Env, kind string, gated bool) (id int64, err error) {
	w, err := e.Writer()
	if err != nil {
		return 0, err
	}
	err = w.InTx(ctx, "update request transaction", gated, func(tx *sql.Tx, now time.Time) error {
		if kind == updatecontrol.KindCheck {
			if open, ok, err := updatecontrol.OpenCheck(ctx, tx); err != nil {
				return err
			} else if ok {
				id = open // join the check that is already queued or running
				return nil
			}
		}
		var err error
		id, err = updatecontrol.InsertRequest(ctx, tx, now, kind, e.Client)
		return err
	})
	if errors.Is(err, mcpset.ErrWriteDisabled) {
		return 0, tooldef.Fail(tooldef.CodeWriteDisabled)
	}
	return id, err
}

// withdraw ends a request the application never took and reports whether it did.
func withdraw(ctx context.Context, e tooldef.Env, id int64) (bool, error) {
	w, err := e.Writer()
	if err != nil {
		return false, err
	}
	var done bool
	err = w.InTx(ctx, "update request withdraw transaction", false, func(tx *sql.Tx, now time.Time) error {
		var err error
		done, err = updatecontrol.Withdraw(ctx, tx, now, id)
		return err
	})
	return done, err
}

func unavailableView() view {
	return view{Unavailable: true, UnavailableReason: unavailableText, CurrentVersion: version.Version}
}

const unavailableText = "このビルドはアップデートの確認に対応していません。"

// reasonText words a deferral reason: SafeGate's kinds and market_hours like the Settings panel, plus this tool's own.
func reasonText(reason string) string {
	switch reason {
	case updatecontrol.ReasonNotReady:
		return "検証済みのインストーラーがありません。先に update_check_now で確認してください"
	case reasonUnavailable:
		return unavailableText
	case reasonTimeout:
		return "アプリが時間内に応答しませんでした。依頼は取り下げました（導入されていません）"
	}
	return updater.BlockedReason(updater.BlockKind(reason))
}
