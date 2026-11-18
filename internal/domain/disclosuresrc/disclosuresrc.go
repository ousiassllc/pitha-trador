// Package disclosuresrc holds the value types of the extra disclosure / news
// sources (issue #1096): the EDINET document list (metadata only) and the 立花
// e支店 news, as the research tables research_edinet_documents and
// research_tachibana_news store them (migration 000063).
//
// The data is research-only. It is never an input of the gates, Policy, Risk,
// Jev or the existing disclosure skip rules.
package disclosuresrc

import (
	"strconv"
	"time"
)

// Sources, as the `source` column of research_disclosure_source_status names them.
const (
	SourceEdinet        = "edinet"
	SourceTachibanaNews = "tachibana_news"
)

// Sources lists every source in display order.
func Sources() []string { return []string{SourceEdinet, SourceTachibanaNews} }

// symbolLen is the length of the symbol format disclosure_items uses (the TSE
// 4-character securities code, e.g. "7203" or "130A").
const symbolLen = 4

// NormalizeSymbol turns a securities code of a source into the symbol format
// disclosure_items.symbol uses: 4 characters, upper case. EDINET's secCode has
// 5 characters (the 4-character code plus a check digit), so the first 4 are
// taken; a 4-character code is kept. Anything else (empty, other lengths,
// characters that are not letters or digits) is not a code and returns ok=false.
func NormalizeSymbol(code string) (symbol string, ok bool) {
	if len(code) != symbolLen && len(code) != symbolLen+1 {
		return "", false
	}
	b := []byte(code[:symbolLen])
	for i, c := range b {
		switch {
		case c >= 'a' && c <= 'z':
			b[i] = c - 'a' + 'A'
		case (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9'):
		default:
			return "", false
		}
	}
	return string(b), true
}

// EdinetDocument is one document of the EDINET document list: the metadata the
// list returns. The document itself is never fetched.
type EdinetDocument struct {
	// DocID is EDINET's document id (docID); unique.
	DocID string
	// Symbol is the normalized 4-character code; SecCode is EDINET's secCode as returned.
	Symbol  string
	SecCode string
	// EdinetCode and FilerName identify the filer; DocTypeCode is the document type
	// (docTypeCode), Title the docDescription.
	EdinetCode  string
	FilerName   string
	DocTypeCode string
	Title       string
	// SubmittedAt is the submission time (EDINET gives JST; stored as UTC).
	SubmittedAt time.Time
}

// EdinetDay is the record of one fetched document-list day (JST, YYYY-MM-DD).
type EdinetDay struct {
	Day       string
	FetchedAt time.Time
	// Total is the list's document count, Recorded the ones stored, Skipped the
	// ones without a securities code or a readable submission time.
	Total    int
	Recorded int
	Skipped  int
}

// TachibanaNews is one news item with one related symbol. A news item with
// several related symbols is several rows; one without any has Symbol "".
type TachibanaNews struct {
	NewsID      string
	Symbol      string
	Headline    string
	PublishedAt time.Time
}

// Failure codes of a failed request, short fixed strings (never an error text:
// EDINET's key travels in the URL). An HTTP error is HTTPFailureCode.
const (
	FailTimeout         = "timeout"
	FailTransport       = "transport"
	FailInvalidResponse = "invalid_response"
	FailAPIError        = "api_error"
)

// HTTPFailureCode classifies a non-200 HTTP status: http_401, http_403 and
// http_429 as they are, other 4xx as http_4xx, 5xx as http_5xx.
func HTTPFailureCode(status int) string {
	switch {
	case status == 401 || status == 403 || status == 429:
		return "http_" + strconv.Itoa(status)
	case status >= 400 && status < 500:
		return "http_4xx"
	case status >= 500:
		return "http_5xx"
	}
	return "http_other"
}

// SourceStatus is one source's fetch state (research_disclosure_source_status).
type SourceStatus struct {
	Source string
	// RequestsDay (JST) and RequestsToday are what the daily cap counts.
	RequestsDay   string
	RequestsToday int
	RequestCount  int
	FailureCount  int
	// LastFetchedAt, LastSuccessAt and LastFailureAt are nil until they happen.
	LastFetchedAt   *time.Time
	LastSuccessAt   *time.Time
	LastFailureAt   *time.Time
	LastFailureCode string
}
