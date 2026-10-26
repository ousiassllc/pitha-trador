// Package gmo is a read-only rate adapter for GMOコイン 外国為替FX's public
// REST API (https://api.coin.z.com/fxdocs/, base URL
// https://forex-api.coin.z.com/public, no authentication): the current quote
// (GET /v1/ticker) and candles (GET /v1/klines) of the configured currency
// pairs.
//
// It is a standalone component (issue #907): nothing in stock/bond trading,
// scanning, Risk, Paper fills or capital allocation uses it, it never places
// an order and it has no API key or login code (the private API is out of
// scope). It deliberately does not use internal/service/broker's Quote:
// GMO's FX public API returns no volume and no order book, so Volume and
// BidDepth have no meaning here. Ticker and Candle carry only what the API
// returns (bid/ask, OHLC).
//
// Behaviour fixed by the issue:
//   - Pairs are configured (Config.Symbols); USD_JPY is the first one.
//   - Candle intervals are 1min and 1day only; BID and ASK candles are
//     requested separately (PriceType).
//   - At most one HTTP call per second (Config.MinInterval, never below
//     1s), enforced across every goroutine sharing a Client.
//   - A failed call is retried with exponential backoff while the failure is
//     transient (network error, HTTP 429/5xx, API-level rate-limit or
//     maintenance code). After Config.MaxAttempts the Client stops, logs an
//     error and returns ErrRetriesExhausted. Permanent failures (other
//     4xx, invalid parameter) are returned at once.
//
// GMOコイン terms to keep in mind (docs/architecture/overview/integrations.md
// §14): commercial use and redistribution of the rates are prohibited
// (約款 21条3項), and 1min candles exist only from 2023-10-28 onwards.
package gmo
