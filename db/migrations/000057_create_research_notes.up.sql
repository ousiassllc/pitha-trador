-- 研究ノート（記録専用。取引・設定には使わない。列の意味と制約は docs/architecture/er/tables-research-notes.md）。versions は追記専用（UPDATE / DELETE / 同じ (note_id, version) か id の INSERT（REPLACE 含む）はトリガで拒否）。
CREATE TABLE research_notes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    question TEXT NOT NULL CHECK (length(trim(question)) > 0 AND length(question) <= 500),
    hypothesis TEXT NOT NULL DEFAULT '' CHECK (length(hypothesis) <= 1000),
    actions TEXT NOT NULL DEFAULT '' CHECK (length(actions) <= 3000),
    result_summary TEXT NOT NULL DEFAULT '' CHECK (length(result_summary) <= 3000),
    conclusion TEXT NOT NULL DEFAULT 'pending' CHECK (conclusion IN ('supported', 'unsupported', 'pending')),
    evidence_links TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(evidence_links) AND json_type(evidence_links) = 'array' AND json_array_length(evidence_links) <= 20 AND length(evidence_links) <= 20000),
    tags TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(tags) AND json_type(tags) = 'array' AND json_array_length(tags) <= 10 AND length(tags) <= 1200),
    current_version INTEGER NOT NULL DEFAULT 1 CHECK (current_version >= 1),
    created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE INDEX research_notes_updated_idx ON research_notes (updated_at, id);
CREATE TABLE research_note_versions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    note_id INTEGER NOT NULL REFERENCES research_notes (id),
    version INTEGER NOT NULL CHECK (version >= 1),
    question TEXT NOT NULL CHECK (length(trim(question)) > 0 AND length(question) <= 500),
    hypothesis TEXT NOT NULL DEFAULT '' CHECK (length(hypothesis) <= 1000),
    actions TEXT NOT NULL DEFAULT '' CHECK (length(actions) <= 3000),
    result_summary TEXT NOT NULL DEFAULT '' CHECK (length(result_summary) <= 3000),
    conclusion TEXT NOT NULL DEFAULT 'pending' CHECK (conclusion IN ('supported', 'unsupported', 'pending')),
    evidence_links TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(evidence_links) AND json_type(evidence_links) = 'array' AND json_array_length(evidence_links) <= 20 AND length(evidence_links) <= 20000),
    tags TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(tags) AND json_type(tags) = 'array' AND json_array_length(tags) <= 10 AND length(tags) <= 1200),
    actor TEXT NOT NULL CHECK (actor IN ('mcp')),
    client TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    UNIQUE (note_id, version)
);
CREATE TRIGGER research_note_versions_no_update BEFORE UPDATE ON research_note_versions BEGIN SELECT RAISE(ABORT, 'research_note_versions is append-only: UPDATE is not allowed'); END;
CREATE TRIGGER research_note_versions_no_delete BEFORE DELETE ON research_note_versions BEGIN SELECT RAISE(ABORT, 'research_note_versions is append-only: DELETE is not allowed'); END;
CREATE TRIGGER research_note_versions_no_replace BEFORE INSERT ON research_note_versions WHEN EXISTS (SELECT 1 FROM research_note_versions WHERE id = NEW.id OR (note_id = NEW.note_id AND version = NEW.version)) BEGIN SELECT RAISE(ABORT, 'research_note_versions is append-only: REPLACE is not allowed'); END;
