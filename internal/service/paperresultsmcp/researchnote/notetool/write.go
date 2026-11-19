package notetool

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/researchnote"
)

// fieldProps are the content fields shared by create and update.
func fieldProps() map[string]any {
	return map[string]any{
		"question":       map[string]any{"type": "string", "maxLength": researchnote.MaxQuestionRunes, "description": "問い（何を知りたいか）"},
		"hypothesis":     map[string]any{"type": "string", "maxLength": researchnote.MaxHypothesisRunes, "description": "仮説"},
		"actions":        map[string]any{"type": "string", "maxLength": researchnote.MaxActionsRunes, "description": "やったこと（集計・バックテスト・調べもの）"},
		"result_summary": map[string]any{"type": "string", "maxLength": researchnote.MaxResultRunes, "description": "結果の要約"},
		"conclusion": map[string]any{
			"type": "string", "enum": []string{researchnote.ConclusionSupported, researchnote.ConclusionUnsupported, researchnote.ConclusionPending},
			"description": "結論。supported＝支持、unsupported＝不支持、pending＝保留",
		},
		"evidence_links": map[string]any{
			"type": "array", "maxItems": researchnote.MaxEvidenceLinks,
			"items": objectSchema(map[string]any{
				"kind": map[string]any{"type": "string", "enum": []string{
					researchnote.KindExperimentID, researchnote.KindBacktestResultID, researchnote.KindComparisonLedger, researchnote.KindURL, researchnote.KindOther,
				}},
				"ref": map[string]any{"type": "string", "maxLength": researchnote.MaxEvidenceRefRunes, "description": "実験 ID・バックテストの結果 ID・比較台帳の条件・URL など"},
			}, "kind", "ref"),
			"description": "根拠へのリンク",
		},
		"tags": map[string]any{
			"type": "array", "maxItems": researchnote.MaxTags,
			"items":       map[string]any{"type": "string", "maxLength": researchnote.MaxTagRunes},
			"description": "タグ（小文字にして重複を除く）",
		},
	}
}

// contentArgs are the arguments both write tools decode. A pointer is "not
// given" (update keeps the current value; create uses the default).
type contentArgs struct {
	Question      *string                  `json:"question"`
	Hypothesis    *string                  `json:"hypothesis"`
	Actions       *string                  `json:"actions"`
	ResultSummary *string                  `json:"result_summary"`
	Conclusion    *string                  `json:"conclusion"`
	EvidenceLinks *[]researchnote.Evidence `json:"evidence_links"`
	Tags          *[]string                `json:"tags"`
}

func (a contentArgs) patch() researchnote.Patch {
	return researchnote.Patch{
		Question: a.Question, Hypothesis: a.Hypothesis, Actions: a.Actions, ResultSummary: a.ResultSummary,
		Conclusion: a.Conclusion, EvidenceLinks: a.EvidenceLinks, Tags: a.Tags,
	}
}

// fields is the content of a new note: every field not given is empty (the
// conclusion pending).
func (a contentArgs) fields() researchnote.Fields {
	var f researchnote.Fields
	text := func(dst *string, v *string) {
		if v != nil {
			*dst = *v
		}
	}
	text(&f.Question, a.Question)
	text(&f.Hypothesis, a.Hypothesis)
	text(&f.Actions, a.Actions)
	text(&f.ResultSummary, a.ResultSummary)
	text(&f.Conclusion, a.Conclusion)
	if a.EvidenceLinks != nil {
		f.EvidenceLinks = *a.EvidenceLinks
	}
	if a.Tags != nil {
		f.Tags = *a.Tags
	}
	return f
}

func createSpec() Spec {
	return Spec{
		Name: "research_note_create",
		Description: "研究ノートを 1 件記録する（問い・仮説・やったこと・結果の要約・結論（支持 supported／不支持 unsupported／保留 pending。既定は保留）・根拠へのリンク・タグ）。" +
			"記録だけで、設定・取引・Risk・発注・Jev には何も影響しない。調整の実験（experiment_create）が設定 1 つの変更の記録なのに対し、データの集計・バックテスト・比較台帳・外部の情報で分かったことを残す。" +
			"question だけが必須。evidence_links は {kind, ref} の配列（kind は experiment_id / backtest_result_id / comparison_ledger / url / other。最大 " + strconv.Itoa(researchnote.MaxEvidenceLinks) + " 件）、tags は最大 " + strconv.Itoa(researchnote.MaxTags) + " 件。" +
			"API キー・トークン・パスワード・Bearer・JWT など、キーの形の文字列は保存の前に [masked] に置き換える（応答・版・一覧にも [masked] で出る）。作成は版 1 になる。書き込み許可がオフのときは write_disabled",
		Schema: objectSchema(fieldProps(), "question"),
		Writes: true,
		Decode: func(raw json.RawMessage) (Run, error) {
			var a contentArgs
			if err := strictDecode(raw, &a); err != nil {
				return nil, err
			}
			if a.Question == nil {
				return nil, failDetail(codeInvalidArgument, "question は必須です")
			}
			f := a.fields()
			return func(ctx context.Context, e Env) (any, int, error) {
				w, err := e.Writer()
				if err != nil {
					return nil, 0, err
				}
				note, err := CreateNote(ctx, w, e.Client, f)
				if err != nil {
					return nil, 0, failureOf(err)
				}
				return note, 1, nil
			}, nil
		},
	}
}

func updateSpec() Spec {
	props := fieldProps()
	props["note_id"] = map[string]any{"type": "integer", "minimum": 1}
	props["expected_version"] = map[string]any{"type": "integer", "minimum": 1, "description": "いま見ている版（current_version）。違うと research_note_version_conflict"}
	return Spec{
		Name: "research_note_update",
		Description: "研究ノートを更新する。渡した項目だけを差し替えた新しい版を追記する（前の版は消さず、research_note_get で読める）。" +
			"expected_version には、いま見ているノートの current_version を渡す（別の書き込みで版が進んでいると research_note_version_conflict で何も書かない。research_note_get で読み直して出し直す）。" +
			"evidence_links / tags に [] を渡すと空にする。変更が 1 つも無い・現在の版と同じ内容は invalid_argument。1 つのノートの版は " + strconv.Itoa(researchnote.MaxVersionsPerNote) + " まで。" +
			"キーの形の文字列は保存の前に [masked] に置き換える。記録だけで、設定・取引には何も影響しない。書き込み許可がオフのときは write_disabled",
		Schema: objectSchema(props, "note_id", "expected_version"),
		Writes: true,
		Decode: func(raw json.RawMessage) (Run, error) {
			var a struct {
				NoteID          *int64 `json:"note_id"`
				ExpectedVersion *int   `json:"expected_version"`
				contentArgs
			}
			if err := strictDecode(raw, &a); err != nil {
				return nil, err
			}
			if a.NoteID == nil || *a.NoteID < 1 || a.ExpectedVersion == nil || *a.ExpectedVersion < 1 {
				return nil, fail(codeInvalidArgument)
			}
			id, expected, patch := *a.NoteID, *a.ExpectedVersion, a.patch()
			return func(ctx context.Context, e Env) (any, int, error) {
				w, err := e.Writer()
				if err != nil {
					return nil, 0, err
				}
				note, err := UpdateNote(ctx, w, e.Client, id, expected, patch)
				if err != nil {
					return nil, 0, failureOf(err)
				}
				return note, 1, nil
			}, nil
		},
	}
}

// The note side of the MCP's Writer (issue #1095). Its whole write surface is
// two transactions: CreateNote (one research_notes INSERT and the version 1
// INSERT) and UpdateNote (the next version's INSERT and one UPDATE of the
// note's head row). Both run behind the write switch (mcpset.ErrWriteDisabled
// while it is off, like settings_set); they touch no setting, audit row,
// position or signal. The rows carry their own actor and client, so there is no
// mcp_settings_audit row.

// CreateNote stores f as a new note (version 1). client is the caller's host. f
// is validated and masked by researchnote.Insert.
func CreateNote(ctx context.Context, w *mcpset.Writer, client string, f researchnote.Fields) (researchnote.Note, error) {
	var out researchnote.Note
	err := w.InTx(ctx, "research note transaction", true, func(tx *sql.Tx, now time.Time) (err error) {
		out, err = researchnote.Insert(ctx, tx, now, client, f)
		return err
	})
	return out, err
}

// UpdateNote writes the next version of note id when its current version is
// expected (researchnote.ErrVersionConflict otherwise). The earlier versions stay.
func UpdateNote(ctx context.Context, w *mcpset.Writer, client string, id int64, expected int, p researchnote.Patch) (researchnote.Note, error) {
	var out researchnote.Note
	err := w.InTx(ctx, "research note update transaction", true, func(tx *sql.Tx, now time.Time) (err error) {
		out, err = researchnote.Update(ctx, tx, now, client, id, expected, p)
		return err
	})
	return out, err
}
