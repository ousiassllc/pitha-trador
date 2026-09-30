// Package marketcalendar answers "is the Tokyo Stock Exchange in session?"
// for the trading-session gating described in
// docs/requirements/non-functional.md §3 (立会時間外は市場データ取得・Jev
// 呼び出し・新規発注を停止する), FR-RISK-6 (ハートビート判定は立会時間中のみ)
// and FR-EXIT-1 (引け前強制決済).
//
// The calendar is a pure, dependency-free rule set (no network, no
// tzdata): 前場 9:00-11:30 and 後場 12:30-15:30 JST on every trading day,
// where a trading day is a weekday that is neither a Japanese national
// holiday (祝日法: fixed/Happy-Monday/equinox holidays, 振替休日, 国民の
// 休日) nor one of the exchange's own closures (年末年始休場: 12/31, 1/1,
// 1/2, 1/3). The holiday rules are those in force since 2020 (令和); dates
// before 2020 are not guaranteed. Ad-hoc closures (e.g. system failures)
// are not modelled.
//
// This package MUST NOT import any other internal package: the packages
// that consume it (scheduler, risk, execution) receive it through small
// interfaces wired by internal/bootstrap (internal/service/doc.go).
package marketcalendar
