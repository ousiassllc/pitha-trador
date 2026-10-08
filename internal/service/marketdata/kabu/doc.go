// Package kabu is the kabuステーションAPI adapter of the broker-neutral
// boundary (internal/service/broker; docs/architecture/overview.md §4
// "ブローカーアダプタ", overview/integrations.md §5): it wraps
// marketdata.Client and the PUSH feed (kabu/pushfeed) behind broker.Session,
// QuoteSource, StreamFeed, SymbolInfoSource, CandidateSource and Health.
//
// Everything specific to kabuステーション stays behind it: the token
// lifecycle, the 50-slot /register list shared by PUSH and REST (and the REST
// rotation that frees slots on 4002006), the information-API rate limit,
// 4001006/429 backoff, 401/4001009 token reissue with its circuit breaker,
// the swapped Bid/Ask naming (kabu/quote), the /ranking 種別1〜7 candidate
// list and the operator guidance for each session failure (SessionStatusOf).
// Only the composition root (internal/bootstrap) and kabu-only tools
// (rankingmeasure) import this package; the neutral packages are kept from
// doing so by depguard.
package kabu
