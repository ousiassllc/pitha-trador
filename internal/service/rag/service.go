package rag

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository"
)

// DefaultK is FR-RAG-2's initial top-k similarity search size.
const DefaultK = 5

// Service builds, persists and searches the standardized feature
// embeddings backing Jev RAG (functional.md §4.13). It is the single
// entry point internal/service/jev and internal/service/featureengine
// depend on; vec0 tables are not modeled through internal/repository
// because each one is a plain (rowid, embedding) pair, not a domain
// entity with its own CRUD surface.
type Service struct {
	db        *sql.DB
	decisions *repository.DecisionRepository
	snapshots *repository.SnapshotRepository
}

// NewService returns a Service backed by db (which must have
// modernc.org/sqlite/vec registered - internal/repository/db.go does
// this via a blank import before Open applies db/migrations' vec0
// CREATE VIRTUAL TABLE statements) and used to hydrate similarity-search
// hits into SimilarCase summaries via decisions/snapshots.
func NewService(db *sql.DB, decisions *repository.DecisionRepository, snapshots *repository.SnapshotRepository) *Service {
	return &Service{db: db, decisions: decisions, snapshots: snapshots}
}

// IndexSnapshot standardizes in and writes it to market_snapshot_vectors
// for snapshotID (FR-RAG-1).
func (s *Service) IndexSnapshot(ctx context.Context, snapshotID int64, in FeatureInput) error {
	return s.insertVector(ctx, "market_snapshot_vectors", "snapshot_id", snapshotID, Build(in))
}

// IndexDecision standardizes in and writes it to jev_decision_vectors for
// decisionID (FR-RAG-1).
func (s *Service) IndexDecision(ctx context.Context, decisionID int64, in FeatureInput) error {
	return s.insertVector(ctx, "jev_decision_vectors", "decision_id", decisionID, Build(in))
}

func (s *Service) insertVector(ctx context.Context, table, idColumn string, id int64, v Vector) error {
	embedding, err := v.json()
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO `+table+` (`+idColumn+`, embedding) VALUES (?, ?)`, id, embedding); err != nil {
		return fmt.Errorf("rag: insert %s row %d: %w", table, id, err)
	}
	return nil
}

// match is one sqlite-vec similarity search hit: the matched row's id
// (snapshot_id or decision_id) and its L2 distance from the query vector
// (smaller is more similar).
type match struct {
	id       int64
	distance float64
}

func (s *Service) search(ctx context.Context, table, idColumn string, v Vector, k int) ([]match, error) {
	if k <= 0 {
		return nil, nil
	}
	embedding, err := v.json()
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT `+idColumn+`, distance FROM `+table+` WHERE embedding MATCH ? ORDER BY distance LIMIT ?`,
		embedding, k)
	if err != nil {
		return nil, fmt.Errorf("rag: search %s: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	var out []match
	for rows.Next() {
		var m match
		if err := rows.Scan(&m.id, &m.distance); err != nil {
			return nil, fmt.Errorf("rag: scan %s match: %w", table, err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rag: iterate %s matches: %w", table, err)
	}
	return out, nil
}

// SimilarCase is one past market state RAG found similar to the state
// Jev is currently evaluating, summarized for Jev's few-shot prompt
// (FR-RAG-3). Source records which vec0 table matched: "jev_decision" or
// "market_snapshot".
type SimilarCase struct {
	Source    string    `json:"source"`
	Symbol    string    `json:"symbol"`
	Timestamp time.Time `json:"timestamp"`
	Distance  float64   `json:"distance"`

	// Direction/Confidence are only ever set when Source is
	// "jev_decision" and the matched decision recorded a Jev Trader
	// judgement (domain.JevDecision.Direction is nil for
	// decision_type=scout, functional.md §4.5). future_return /
	// was_direction_correct (FR-RAG-3) require calibration_outcomes,
	// which does not exist yet in this codebase (functional.md §4.12 is
	// a later sub-scope); add them here once that table lands.
	Direction  *string  `json:"direction,omitempty"`
	Confidence *float64 `json:"confidence,omitempty"`
}

// Context is the few-shot context the RAG Context Builder injects into a
// Jev Scout/Trader request (FR-RAG-3, architecture/overview.md §7). An
// empty Context (nil/zero-length Cases) is the expected cold-start value
// (FR-RAG-4): callers still send it to Jev, which falls back to its
// normal judgement.
type Context struct {
	Cases []SimilarCase `json:"cases,omitempty"`
}

// Context returns the FR-RAG-2 top-k (DefaultK if k <= 0) similar past
// states for in: jev_decisions matches first, backfilled with
// market_snapshots matches up to k total. jev_decisions matches are
// prioritized because - once functional.md §4.12's calibration_outcomes
// lands - they are the ones that can carry a resolved outcome; that is
// this codebase's current closest match to FR-RAG-2's
// "calibration_outcomes紐付き済みのもの優先" intent, since the table
// itself does not exist yet.
//
// A search or hydration error is only ever returned for a true
// technical failure (e.g. a closed/broken database connection); a
// cold-start empty result is not an error (FR-RAG-4).
func (s *Service) Context(ctx context.Context, in FeatureInput, k int) (Context, error) {
	if k <= 0 {
		k = DefaultK
	}
	v := Build(in)

	decisionMatches, err := s.search(ctx, "jev_decision_vectors", "decision_id", v, k)
	if err != nil {
		return Context{}, fmt.Errorf("rag: search similar decisions: %w", err)
	}

	var cases []SimilarCase
	for _, m := range decisionMatches {
		d, err := s.decisions.Get(ctx, m.id)
		if err != nil {
			continue
		}
		cases = append(cases, SimilarCase{
			Source:     "jev_decision",
			Symbol:     d.Symbol,
			Timestamp:  d.Timestamp,
			Distance:   m.distance,
			Direction:  d.Direction,
			Confidence: d.Confidence,
		})
	}

	if len(cases) < k {
		snapshotMatches, err := s.search(ctx, "market_snapshot_vectors", "snapshot_id", v, k-len(cases))
		if err != nil {
			return Context{}, fmt.Errorf("rag: search similar snapshots: %w", err)
		}
		for _, m := range snapshotMatches {
			snap, err := s.snapshots.Get(ctx, m.id)
			if err != nil {
				continue
			}
			cases = append(cases, SimilarCase{
				Source:    "market_snapshot",
				Symbol:    snap.Symbol,
				Timestamp: snap.Timestamp,
				Distance:  m.distance,
			})
		}
	}

	return Context{Cases: cases}, nil
}
