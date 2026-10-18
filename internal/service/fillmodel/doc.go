// Package fillmodel is the one fill assumption Paper Trading
// (internal/service/execution) and the Backtest Engine
// (internal/service/backtest) share (issue #509, FR-BT-4 / FR-ENTRY-*):
// a market order is not filled at the signal price in full but at what the
// 東証 would give a retail-sized order -
//
//   - 呼値単位: prices are on the TSE tick grid (TickSize), rounded against
//     the order (buy up, sell down);
//   - スプレッド: ザラ場 market orders cross the spread (buy at the ask, sell
//     at the bid; a half spread around the last price when only SpreadBps
//     is known);
//   - 滑り: Model.SlippageBps beyond the touch, adverse to the order;
//   - 手数料: Model.FeeBps of the notional plus Model.FeeFixedJPY per fill;
//     SHORT: Model.ShortBorrowBps (貸株料) once per position;
//   - 昼休み・立会時間外: nothing fills (marketcalendar.PhaseClosed);
//   - 寄り・引けの気配: the opening/closing auction crosses at one price, so
//     no spread is paid, but the indicative price can move before the
//     板寄せ: Model.OpeningSlippageBps / ClosingSlippageBps instead of
//     SlippageBps.
//
// Costs holds one Model per direction and is built from the Settings-screen
// cost settings (internal/config/costsettings), the single source Paper
// fills, the backtest and the triple-barrier label cost all read (#779).
//
// Everything here is a pure function of its inputs: no I/O, no clock.
package fillmodel
