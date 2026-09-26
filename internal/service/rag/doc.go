// Package rag implements Jev RAG (経験ベース文脈拡張,
// docs/requirements/functional.md §4.13, docs/architecture/overview.md
// §7): it standardizes each market_snapshots/jev_decisions row's feature
// values into a fixed 14-dimension embedding (Build), persists it to the
// corresponding sqlite-vec vec0 virtual table (market_snapshot_vectors /
// jev_decision_vectors, docs/architecture/er.md §ベクトルインデックス), and
// searches those tables for past states similar to the one Jev is about
// to evaluate so a summary of what happened then can be injected into
// the Jev request as few-shot context (Service.Context).
//
// The embedding is a deterministic function of already-computed Feature
// Engine values - no LLM call, no network round trip, no added latency
// or API cost (functional.md FR-RAG-3). A cold start (few or no rows
// indexed yet) is not an error: Service.Context simply returns fewer (or
// zero) SimilarCase values, and callers send that empty Context to Jev
// unconditionally (FR-RAG-4).
package rag
