package notify_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

func TestSlackNotifier_DailyEntryHalted(t *testing.T) {
	var got []string
	n, closeSrv := captureWebhook(t, &got)
	defer closeSrv()

	halt := domain.DailyEntryHalt{
		Day:     "2026-09-28",
		Reason:  domain.DailyHaltReasonProfitLock,
		Message: "daily profit reached the lock threshold",
	}
	if err := n.DailyEntryHalted(context.Background(), halt); err != nil {
		t.Fatalf("DailyEntryHalted: %v", err)
	}
	const want = ":octagonal_sign: 新規エントリー停止: 利益確保（profit lock）（reason=profit_lock）\n" +
		"daily profit reached the lock threshold\n" +
		"本日の新規エントリーは停止しました（決済は継続）。翌営業日に自動で解除されます。"
	if len(got) != 1 || got[0] != want {
		t.Errorf("got = %q, want [%q]", got, want)
	}
}

func TestSlackNotifier_AIStageSkipped(t *testing.T) {
	var got []string
	n, closeSrv := captureWebhook(t, &got)
	defer closeSrv()

	if err := n.AIStageSkipped(context.Background(), "Sol提案生成", errors.New("anthropic api timeout")); err != nil {
		t.Fatalf("AIStageSkipped: %v", err)
	}
	const want = ":warning: 自己改善ループ: Sol提案生成のAPI呼び出しに失敗したため本日分をスキップしました（翌営業日に再試行）: anthropic api timeout"
	if len(got) != 1 || got[0] != want {
		t.Errorf("got = %q, want [%q]", got, want)
	}
}
