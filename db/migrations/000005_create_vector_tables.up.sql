-- market_snapshot_vectors / jev_decision_vectors: sqlite-vec vec0 virtual
-- tables holding the standardized 14-dimension feature embedding for each
-- market_snapshots / jev_decisions row (docs/architecture/er.md
-- §ベクトルインデックス（sqlite-vec）, functional.md FR-RAG-1). The vec0
-- module is registered process-wide by internal/repository/db.go's blank
-- import of modernc.org/sqlite/vec before this migration runs.
CREATE VIRTUAL TABLE market_snapshot_vectors USING vec0(
    snapshot_id INTEGER PRIMARY KEY,
    embedding FLOAT[14]
);

CREATE VIRTUAL TABLE jev_decision_vectors USING vec0(
    decision_id INTEGER PRIMARY KEY,
    embedding FLOAT[14]
);
