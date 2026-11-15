package notetool

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/researchnote"
)

// Listing limits of research_notes_list.
const (
	defaultList = 20
	maxList     = 100
)

// Period limits (API spec §1, §2): the same rule as every other period tool.
const maxPeriodDays = 366

func listSpec() Spec {
	return Spec{
		Name: "research_notes_list",
		Description: "研究ノートの一覧（更新の新しい順）。現在の版の問い・仮説・結果の要約（先頭 200 文字）・結論・タグ・根拠リンクの件数・版の番号・作成と更新の時刻を返す（全文と過去の版は research_note_get）。" +
			"query は問い・仮説・やったこと・結果の要約・タグを大文字小文字を区別せず（英字）部分一致で探す（" + strconv.Itoa(researchnote.MaxQueryRunes) + " 文字以内。% と _ はそのままの文字として探す）。" +
			"tag はタグ 1 つ（完全一致。小文字にして比べる）、conclusion は supported / unsupported / pending で絞る。" +
			"from / to は他の期間つきツールと同じ JST の日付 [from, to) で、ノートの更新時刻を絞る（両方指定するか両方省略。省略は期間で絞らない）。読み取り専用",
		Schema: objectSchema(map[string]any{
			"query": map[string]any{"type": "string", "maxLength": researchnote.MaxQueryRunes, "description": "探す文字列（部分一致）"},
			"tag":   map[string]any{"type": "string", "maxLength": researchnote.MaxTagRunes},
			"conclusion": map[string]any{
				"type": "string", "enum": []string{researchnote.ConclusionSupported, researchnote.ConclusionUnsupported, researchnote.ConclusionPending},
			},
			"from":  map[string]any{"type": "string", "description": "JST の開始日 YYYY-MM-DD（含む）。to と両方指定するか、両方省略（期間で絞らない）"},
			"to":    map[string]any{"type": "string", "description": "JST の終了日 YYYY-MM-DD（含まない）。期間は最大 366 日"},
			"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": maxList, "default": defaultList},
		}),
		Decode: decodeList,
	}
}

func decodeList(raw json.RawMessage) (Run, error) {
	var a struct {
		Query      string  `json:"query"`
		Tag        string  `json:"tag"`
		Conclusion string  `json:"conclusion"`
		From       *string `json:"from"`
		To         *string `json:"to"`
		Limit      *int    `json:"limit"`
	}
	if err := strictDecode(raw, &a); err != nil {
		return nil, err
	}
	limit := defaultList
	if a.Limit != nil {
		switch {
		case *a.Limit < 1:
			return nil, fail(codeInvalidArgument)
		case *a.Limit > maxList:
			return nil, fail(codeLimitExceeded)
		}
		limit = *a.Limit
	}
	switch a.Conclusion {
	case "", researchnote.ConclusionSupported, researchnote.ConclusionUnsupported, researchnote.ConclusionPending:
	default:
		return nil, fail(codeInvalidArgument)
	}
	text := strings.TrimSpace(a.Query)
	if utf8.RuneCountInString(text) > researchnote.MaxQueryRunes || utf8.RuneCountInString(a.Tag) > researchnote.MaxTagRunes {
		return nil, fail(codeInvalidArgument)
	}
	return func(ctx context.Context, e Env) (any, int, error) {
		filter := researchnote.Filter{Query: text, Tag: strings.TrimSpace(a.Tag), Conclusion: a.Conclusion}
		if a.From != nil || a.To != nil {
			from, to, err := resolvePeriod(a.From, a.To, e.Now)
			if err != nil {
				return nil, 0, err
			}
			filter.UpdatedFrom, filter.UpdatedTo = from, to
		}
		rows, err := researchnote.List(ctx, e.Q, filter, limit)
		return map[string]any{"notes": rows}, len(rows), err
	}, nil
}

// resolvePeriod turns from / to into the half-open JST range [from, to). One
// alone, a bad format, to <= from, more than 366 days, or to later than JST's
// tomorrow is invalid_period.
func resolvePeriod(fromArg, toArg *string, now time.Time) (from, to time.Time, err error) {
	if fromArg == nil || toArg == nil {
		return from, to, fail(codeInvalidPeriod)
	}
	y, m, d := now.In(query.JST).Date()
	tomorrow := time.Date(y, m, d, 0, 0, 0, 0, query.JST).AddDate(0, 0, 1)
	if from, err = time.ParseInLocation(time.DateOnly, *fromArg, query.JST); err != nil {
		return from, to, fail(codeInvalidPeriod)
	}
	if to, err = time.ParseInLocation(time.DateOnly, *toArg, query.JST); err != nil {
		return from, to, fail(codeInvalidPeriod)
	}
	if !to.After(from) || to.After(tomorrow) || to.After(from.AddDate(0, 0, maxPeriodDays)) {
		return from, to, fail(codeInvalidPeriod)
	}
	return from, to, nil
}

func getSpec() Spec {
	return Spec{
		Name: "research_note_get",
		Description: "研究ノート 1 件の全文（現在の版）と、すべての版（古い順。各版の全文・書いた人 actor・接続元 client・時刻。1 つのノートの版は " + strconv.Itoa(researchnote.MaxVersionsPerNote) + " まで）。" +
			"更新（research_note_update）の前に、ここで current_version を確かめる。読み取り専用",
		Schema: objectSchema(map[string]any{"note_id": map[string]any{"type": "integer", "minimum": 1}}, "note_id"),
		Decode: func(raw json.RawMessage) (Run, error) {
			var a struct {
				NoteID *int64 `json:"note_id"`
			}
			if err := strictDecode(raw, &a); err != nil {
				return nil, err
			}
			if a.NoteID == nil || *a.NoteID < 1 {
				return nil, fail(codeInvalidArgument)
			}
			id := *a.NoteID
			return func(ctx context.Context, e Env) (any, int, error) {
				d, err := researchnote.GetDetail(ctx, e.Q, id)
				if err != nil {
					return nil, 0, failureOf(err)
				}
				return d, 1 + len(d.Versions), nil
			}, nil
		},
	}
}
