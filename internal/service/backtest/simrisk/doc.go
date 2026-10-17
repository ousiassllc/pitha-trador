// Package simrisk is the Risk Engine of the backtest replay (issue #775): the
// FR-RISK-1/2 entry limits evaluated against a simulated portfolio instead of
// the positions table, so a replay is held to the same max_open_positions,
// same-direction cap, market-adverse, exposure, daily-loss, consecutive-loss,
// cooldown and sizing rules as Paper Trading.
//
// It reuses the live pure rules (risk/correlation, risk/sizing) and the live
// rejection reasons (risk.Reason*); only the state is simulated. State a
// historical replay cannot know is treated as follows (docs/requirements/
// functional/components-platform.md FR-BT-4):
//
//   - Operator pause, manual Kill Switch, market-data/Jev/broker-API Kill
//     Switches and the UI heartbeat are not recorded in a replayable form: they
//     are treated as inactive.
//   - The daily_loss_limit and consecutive_losses Kill Switches are derivable
//     from the replayed trades: a breach rejects entries (Check's reason, as
//     live), and - since live latches them until the operator resumes - the
//     replay assumes the operator resumes at the start of the next trading day
//     (JST): the loss streak and the cooldown baseline restart there.
//   - The FR-RISK-8 daily entry halt (yen loss limit, profit lock, trade
//     count) is not replayed.
package simrisk
