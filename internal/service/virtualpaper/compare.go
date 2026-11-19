package virtualpaper

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/virtualpaper"
	"github.com/ousiassllc/pitha-trador/internal/service/actuals"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest/variant"
)

// The comparison's bounds.
const (
	// MaxCompareProfiles is the most profiles one comparison takes.
	MaxCompareProfiles = 5
	// MaxTradesPerProfile bounds the rows one profile's comparison reads, so it
	// fits the MCP's 3 s read budget; a longer period is narrowed by the caller.
	MaxTradesPerProfile = 20000
)

// Caution is shown with every comparison.
const Caution = "仮想 Paper は記録専用の研究データです。この比較は設定・取引・Risk のどれにも自動では反映されず、判断に使う場合も人が行います。" +
	"標本が小さい（取引件数・日数が少ない）間は差が偶然で出ることが多く、区間が 0 をまたぐ差は差があるとも無いとも言えません。" +
	"プロファイルを何通りも比べるほど、どれかが良く見えるのは偶然でも起こります（多重比較）。" +
	"データ終了で閉じた取引（unfinished）は指標から外しています。結果は取引日の翌日以降に出ます。"

// ErrTooManyTrades is returned when a profile's trades in the period exceed
// MaxTradesPerProfile.
var ErrTooManyTrades = errors.New("virtualpaper: too many trades in the period")

// ExcludedDay is a day some compared profile has no completed record for, so it
// is left out of every profile's figures.
type ExcludedDay struct {
	Day string `json:"day"`
	// MissingProfileIDs are the compared profiles without a completed day.
	MissingProfileIDs []int64 `json:"missing_profile_ids"`
}

// Paired is a profile's day-by-day difference from the first compared profile.
type Paired struct {
	AgainstProfileID int64 `json:"against_profile_id"`
	// DailyNetReturnPctDiff is the mean over the common days of (this profile's
	// day-sum of net returns - the first's), with the 95% interval over days.
	DailyNetReturnPctDiff actuals.Metric `json:"daily_net_return_pct_diff"`
	TradeCountDiff        int            `json:"trade_count_diff"`
}

// ProfileResult is one profile's figures over the common days.
type ProfileResult struct {
	ProfileID int64          `json:"profile_id"`
	Name      string         `json:"name"`
	Status    string         `json:"status"`
	Params    variant.Params `json:"params"`
	// TradeCount counts the trades closed by an exit condition; Unfinished the
	// ones the data's end closed (left out of every figure).
	TradeCount int     `json:"trade_count"`
	Unfinished int     `json:"unfinished"`
	WinRate    float64 `json:"win_rate"`
	// Expectancy is the mean net (cost-inclusive) return per trade, % of the
	// notional, with the 95% interval clustered by day.
	Expectancy actuals.Metric `json:"expectancy_pct"`
	// TotalPnLJPY is the sum of the recorded pnl_jpy.
	TotalPnLJPY float64 `json:"total_pnl_jpy"`
	// MaxDrawdownPct is the largest peak-to-trough fall of the cumulative net
	// return, trades in exit order (backtest.Aggregate's definition).
	MaxDrawdownPct float64 `json:"max_drawdown_pct"`
	VsFirst        *Paired `json:"vs_first,omitempty"`
}

// Comparison is the response of Compare.
type Comparison struct {
	// From / To are the JST days [From, To) asked for.
	From string `json:"from"`
	To   string `json:"to"`
	// Days are the common days: completed for every compared profile.
	Days         []string        `json:"days"`
	ExcludedDays []ExcludedDay   `json:"excluded_days"`
	Profiles     []ProfileResult `json:"profiles"`
	Caution      string          `json:"caution"`
}

// Compare compares the profiles over the JST days [from, to), using only the
// days completed for all of them (so the same decisions and bars). ids empty
// takes every profile with a completed day in the period (at most
// MaxCompareProfiles); otherwise 2..MaxCompareProfiles distinct ids, the first
// being what the others are paired with. Errors: *InvalidError, *LimitError,
// ErrTooManyTrades.
func Compare(ctx context.Context, q repo.Querier, from, to string, ids []int64) (Comparison, error) {
	out := Comparison{From: from, To: to, Days: []string{}, ExcludedDays: []ExcludedDay{}, Profiles: []ProfileResult{}, Caution: Caution}
	profiles, err := selectProfiles(ctx, q, from, to, ids)
	if err != nil || len(profiles) == 0 {
		return out, err
	}
	marks := make([]map[string]repo.Day, len(profiles))
	for i, p := range profiles {
		days, err := repo.Days(ctx, q, p.ID, from, to)
		if err != nil {
			return out, err
		}
		marks[i] = map[string]repo.Day{}
		for _, d := range days {
			marks[i][d.Day] = d
		}
	}
	common, excluded := splitDays(profiles, marks)
	out.Days, out.ExcludedDays = common, excluded
	inCommon := map[string]bool{}
	for _, d := range common {
		inCommon[d] = true
	}
	var first *perDay
	for i, p := range profiles {
		rows, err := repo.FinishedTrades(ctx, q, p.ID, from, to, MaxTradesPerProfile)
		if errors.Is(err, repo.ErrTooManyRows) {
			return out, ErrTooManyTrades
		}
		if err != nil {
			return out, err
		}
		res, pd, err := summarize(p, rows, marks[i], common, inCommon)
		if err != nil {
			return out, err
		}
		if first == nil {
			first = pd
		} else {
			res.VsFirst = pair(profiles[0].ID, common, first, pd, res.TradeCount-out.Profiles[0].TradeCount)
		}
		out.Profiles = append(out.Profiles, res)
	}
	return out, nil
}

// selectProfiles resolves the profiles to compare.
func selectProfiles(ctx context.Context, q repo.Querier, from, to string, ids []int64) ([]repo.Profile, error) {
	if len(ids) == 0 {
		found, err := repo.ProfileIDsWithDoneDays(ctx, q, from, to)
		if err != nil {
			return nil, err
		}
		if len(found) > MaxCompareProfiles {
			return nil, &LimitError{fmt.Sprintf("期間にデータのあるプロファイルが %d 件あります。profile_ids で %d 件以内に絞ってください", len(found), MaxCompareProfiles)}
		}
		ids = found
	} else if len(ids) < 2 || len(ids) > MaxCompareProfiles {
		return nil, &InvalidError{"profile_ids", fmt.Sprintf("2 件以上 %d 件以下です", MaxCompareProfiles)}
	}
	seen := map[int64]bool{}
	profiles := make([]repo.Profile, 0, len(ids))
	for _, id := range ids {
		if seen[id] {
			return nil, &InvalidError{"profile_ids", "同じ ID は 1 回だけ指定できます"}
		}
		seen[id] = true
		p, err := repo.GetProfile(ctx, q, id)
		if errors.Is(err, repo.ErrNotFound) {
			return nil, &InvalidError{"profile_ids", "その profile_id のプロファイルはありません"}
		}
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, p)
	}
	return profiles, nil
}

// splitDays returns the days completed ('done') for every profile, and the days
// completed for some but not all, with who lacks them.
func splitDays(profiles []repo.Profile, marks []map[string]repo.Day) (common []string, excluded []ExcludedDay) {
	union := map[string]bool{}
	for _, m := range marks {
		for day, d := range m {
			if d.Status == repo.DayDone {
				union[day] = true
			}
		}
	}
	days := make([]string, 0, len(union))
	for day := range union {
		days = append(days, day)
	}
	slices.Sort(days)
	common, excluded = []string{}, []ExcludedDay{}
	for _, day := range days {
		var missing []int64
		for i, p := range profiles {
			if marks[i][day].Status != repo.DayDone {
				missing = append(missing, p.ID)
			}
		}
		if len(missing) == 0 {
			common = append(common, day)
		} else {
			excluded = append(excluded, ExcludedDay{Day: day, MissingProfileIDs: missing})
		}
	}
	return common, excluded
}

// dayKey is the day number actuals clusters by.
func dayKey(day string) (int64, error) {
	t, err := time.ParseInLocation(time.DateOnly, day, jst)
	if err != nil {
		return 0, fmt.Errorf("virtualpaper: day %q: %w", day, err)
	}
	return actuals.JSTDay(t.Add(12 * time.Hour)), nil
}

// summarize computes a profile's figures from its trades of the common days.
func summarize(p repo.Profile, rows []repo.TradeRow, marks map[string]repo.Day, common []string, inCommon map[string]bool) (ProfileResult, *perDay, error) {
	params, err := ParamsOf(p)
	if err != nil {
		return ProfileResult{}, nil, err
	}
	res := ProfileResult{ProfileID: p.ID, Name: p.Name, Status: p.Status, Params: params}
	for _, day := range common {
		res.Unfinished += marks[day].Unfinished
	}
	pd := &perDay{net: make(map[string]float64, len(common))}
	var (
		trades []backtest.Trade
		values []float64
		keys   []int64
	)
	for _, r := range rows {
		if !inCommon[r.Day] {
			continue
		}
		key, err := dayKey(r.Day)
		if err != nil {
			return ProfileResult{}, nil, err
		}
		trades = append(trades, backtest.Trade{
			InstrumentID: r.InstrumentID, Symbol: r.Symbol, EntryTimestamp: r.EntryAt, ExitTimestamp: r.ExitAt, NetReturnPct: r.Net,
		})
		values, keys = append(values, r.Net), append(keys, key)
		pd.net[r.Day] += r.Net
		res.TotalPnLJPY += r.PnLJPY
	}
	m := backtest.Aggregate(trades)
	res.TradeCount, res.WinRate, res.MaxDrawdownPct = m.TradeCount, m.WinRate, m.MaxDrawdownPct
	res.Expectancy = actuals.ClusterMean(values, keys)
	return res, pd, nil
}

// perDay is a profile's net return summed by day.
type perDay struct{ net map[string]float64 }

// pair is the day-by-day difference of b from a over the common days (a day
// without a trade counts as 0 for that profile).
func pair(againstID int64, common []string, a, b *perDay, tradeCountDiff int) *Paired {
	diffs := make([]float64, 0, len(common))
	keys := make([]int64, 0, len(common))
	for _, day := range common {
		key, err := dayKey(day)
		if err != nil {
			continue // common days come from the table's own CHECKed text
		}
		diffs, keys = append(diffs, b.net[day]-a.net[day]), append(keys, key)
	}
	return &Paired{AgainstProfileID: againstID, DailyNetReturnPctDiff: actuals.ClusterMean(diffs, keys), TradeCountDiff: tradeCountDiff}
}
