// Package rejectstats counts why the Policy Engine did not enter: today's NONE
// trade_signals grouped by the head of reject_reason (issue #896, the Home
// screen's 「エントリーしなかった理由」). It is strictly read-only and depends
// only on internal/repository/sqlutil, like internal/repository/actuals.
package rejectstats
