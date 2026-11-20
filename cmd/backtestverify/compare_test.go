package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/backtestsource"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
)

// fakeComparer returns canned RunConfigs and records the period it was asked.
type fakeComparer struct {
	configs []backtest.RunConfig
	err     error

	gotPeriod backtest.Period
}

func (f *fakeComparer) Mode() backtestsource.ReplayMode { return backtestsource.ReplayPaper }

func (f *fakeComparer) RunConfigs(_ context.Context, period backtest.Period) ([]backtest.RunConfig, error) {
	f.gotPeriod = period
	return f.configs, f.err
}

var comparePeriod = backtest.Period{Start: time.Date(2026, 10, 14, 0, 0, 0, 0, jst), End: time.Date(2026, 10, 15, 0, 0, 0, 0, jst)}

func TestCompareLegacy_PrintsMetaAndLegacyDifference(t *testing.T) {
	cfgs := []backtest.RunConfig{{InstrumentID: 1, Symbol: "7203"}, {InstrumentID: 2, Symbol: "6758"}}
	want := backtest.EntryComparison{
		Meta:   backtest.Metrics{TradeCount: 1, WinRate: 1, Expectancy: 0.4, ProfitFactor: 2.5, MaxDrawdownPct: 0.1, NetPnLPct: 0.4},
		Legacy: backtest.Metrics{TradeCount: 3, WinRate: 1.0 / 3, Expectancy: -0.2, ProfitFactor: 0.5, MaxDrawdownPct: 0.9, NetPnLPct: -0.6},
	}
	var gotCfgs []backtest.RunConfig
	var gotPeriod backtest.Period
	stub := func(_ context.Context, c []backtest.RunConfig, p backtest.Period) (backtest.EntryComparison, error) {
		gotCfgs, gotPeriod = c, p
		return want, nil
	}
	src := &fakeComparer{configs: cfgs}

	var stdout, stderr bytes.Buffer
	if code := compareLegacy(context.Background(), src, stub, comparePeriod, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit code = %d, want 0\nstderr:\n%s", code, stderr.String())
	}
	if len(gotCfgs) != len(cfgs) || gotPeriod != comparePeriod || src.gotPeriod != comparePeriod {
		t.Errorf("comparison got %d configs over %v (source asked %v), want the source's %d configs over %v", len(gotCfgs), gotPeriod, src.gotPeriod, len(cfgs), comparePeriod)
	}
	for _, line := range []string{
		"再生モード: paper（min_entry_quality: Paper用 policy.paper.*）",
		"期間: 2026-10-14 00:00 〜 2026-10-15 00:00 (JST)",
		"新旧エントリールール比較",
		"新ルール(Meta・現行):          件数 1 勝率 100.0% Expectancy 0.400% PF 2.500 MaxDD 0.100% 純損益 0.400%",
		"旧ルール(Legacy・確信度しきい値): 件数 3 勝率 33.3% Expectancy -0.200% PF 0.500 MaxDD 0.900% 純損益 -0.600%",
		"差（新−旧）: 件数 -2 勝率 +66.7pt Expectancy +0.600% MaxDD -0.800% 純損益 +1.000%",
	} {
		if !strings.Contains(stdout.String(), line) {
			t.Errorf("stdout missing %q:\n%s", line, stdout.String())
		}
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

func TestCompareLegacy_ErrorsAreExitTwoWithNothingOnStdout(t *testing.T) {
	failing := func(context.Context, []backtest.RunConfig, backtest.Period) (backtest.EntryComparison, error) {
		return backtest.EntryComparison{}, errors.New("replay broke")
	}
	ok := func(context.Context, []backtest.RunConfig, backtest.Period) (backtest.EntryComparison, error) {
		return backtest.EntryComparison{}, nil
	}
	for name, tc := range map[string]struct {
		src     *fakeComparer
		compare compareFunc
		want    string
	}{
		"reading the history fails": {&fakeComparer{err: errors.New("read snapshots")}, ok, "read snapshots"},
		"the replay fails":          {&fakeComparer{}, failing, "replay broke"},
	} {
		var stdout, stderr bytes.Buffer
		if code := compareLegacy(context.Background(), tc.src, tc.compare, comparePeriod, &stdout, &stderr); code != exitError {
			t.Errorf("%s: exit code = %d, want 2", name, code)
		}
		if stdout.Len() != 0 || !strings.Contains(stderr.String(), tc.want) {
			t.Errorf("%s: stdout %q, stderr %q; want only stderr naming %q", name, stdout.String(), stderr.String(), tc.want)
		}
	}
}

func TestRun_CompareLegacyOnAnEmptyHistoryPrintsZeroComparison(t *testing.T) {
	db := seedDB(t, time.Time{})
	for name, mode := range map[string]string{"live": "live", "paper": "paper"} {
		code, stdout, stderr := runCLI(t, "-db", db, "-from", "2026-10-01", "-to", "2026-10-16", "-compare-legacy", "-replay-mode", mode)
		if code != exitOK {
			t.Fatalf("%s: exit code = %d, want 0\nstdout:\n%s\nstderr:\n%s", name, code, stdout, stderr)
		}
		for _, want := range []string{"再生モード: " + mode, "新旧エントリールール比較", "新ルール(Meta・現行):", "旧ルール(Legacy・確信度しきい値):", "差（新−旧）: 件数 +0"} {
			if !strings.Contains(stdout, want) {
				t.Errorf("%s: stdout missing %q:\n%s", name, want, stdout)
			}
		}
		if strings.Contains(stdout, "再現性検証") {
			t.Errorf("%s: -compare-legacy must replace the reproducibility check:\n%s", name, stdout)
		}
	}
}

func TestCompareLegacy_DoesNotWriteToDB(t *testing.T) {
	db := seedDB(t, time.Date(2026, 10, 14, 9, 5, 0, 0, jst))
	read := func() []byte {
		b, err := os.ReadFile(db)
		if err != nil {
			t.Fatalf("read database file: %v", err)
		}
		return b
	}
	before := read()
	for _, mode := range []string{"live", "paper"} {
		if code, _, stderr := runCLI(t, "-db", db, "-from", "2026-10-14", "-to", "2026-10-14", "-compare-legacy", "-replay-mode", mode); code != exitOK {
			t.Fatalf("%s: exit code = %d, want 0\nstderr:\n%s", mode, code, stderr)
		}
		if !bytes.Equal(read(), before) {
			t.Fatalf("%s: the database file changed: -compare-legacy must be read-only (runtime_settings, orders and positions included)", mode)
		}
	}
}

func TestWithoutCompareLegacy_OutputAndExitCodeUnchanged(t *testing.T) {
	// The reproducibility check, byte for byte what it printed before the flag.
	empty := seedDB(t, time.Time{})
	code, stdout, stderr := runCLI(t, "-db", empty, "-from", "2026-10-01", "-to", "2026-10-16")
	if code != exitOK || stderr != "" {
		t.Fatalf("empty database: exit %d, stderr %q; want exit 0 and no stderr", code, stderr)
	}
	const wantEmpty = "再生モード: live（min_entry_quality: Live用 policy.*）\n" +
		"期間: 2026-10-01 00:00 〜 2026-10-17 00:00 (JST)\n" +
		"バックテスト再現性検証: データ無し\n" +
		"  期間内にPaperのエントリーが無いため、突き合わせる対象がありません。\n"
	if stdout != wantEmpty {
		t.Errorf("empty database: stdout =\n%s\nwant\n%s", stdout, wantEmpty)
	}

	withEntry := seedDB(t, time.Date(2026, 10, 14, 9, 5, 0, 0, jst))
	code, stdout, stderr = runCLI(t, "-db", withEntry, "-from", "2026-10-14", "-to", "2026-10-14")
	if code != exitFailed || !strings.Contains(stderr, "does not reproduce Paper trading") {
		t.Errorf("unreproducible entry: exit %d, stderr %q; want exit 1 with the report's error", code, stderr)
	}
	if !strings.Contains(stdout, "バックテスト再現性検証: 不合格") || strings.Contains(stdout, "新旧エントリールール比較") {
		t.Errorf("unreproducible entry: stdout = %s, want the failing report and no comparison", stdout)
	}
}

func TestRun_CompareLegacyUsageErrorsAreExitTwo(t *testing.T) {
	db := seedDB(t, time.Time{})
	missing := filepath.Join(t.TempDir(), "absent.db")
	for name, args := range map[string][]string{
		"only -from":       {"-db", db, "-compare-legacy", "-from", "2026-10-01"},
		"unknown mode":     {"-db", db, "-compare-legacy", "-replay-mode", "demo"},
		"stray argument":   {"-db", db, "-compare-legacy", "extra"},
		"missing database": {"-db", missing, "-compare-legacy"},
		"help":             {"-compare-legacy", "-h"},
	} {
		code, stdout, stderr := runCLI(t, args...)
		if code != exitError || stdout != "" || stderr == "" {
			t.Errorf("%s: exit %d, stdout %q, stderr %q; want exit 2 with only stderr", name, code, stdout, stderr)
		}
	}
	if _, _, stderr := runCLI(t, "-h"); !strings.Contains(stderr, "-compare-legacy") {
		t.Errorf("-h does not document -compare-legacy:\n%s", stderr)
	}
}
