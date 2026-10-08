package tachibana

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"golang.org/x/text/encoding/japanese"

	"github.com/ousiassllc/pitha-trador/internal/textutil"
)

const (
	// jsonFormat is sJsonOfmt: item-name JSON (not the numbered, compressed
	// form), so responses never depend on the broker's item numbering.
	jsonFormat = "4"
	// sdDateLayout is p_sd_date: YYYY.MM.DD-HH:MM:SS.TTT (JST, milliseconds).
	sdDateLayout = "2006.01.02-15:04:05.000"
	// maxPNo is the largest p_no the broker accepts.
	maxPNo = 9999999999
	// maxErrorText caps the broker message kept in an APIError.
	maxErrorText = 200
)

// formatSDDate renders t as the p_sd_date of a request.
func formatSDDate(t time.Time) string { return t.In(JST).Format(sdDateLayout) }

// requestBody is the Shift-JIS JSON of one request: the envelope (p_no,
// p_sd_date, sCLMID, sJsonOfmt) plus fields. Fields cannot override the
// envelope.
func requestBody(pNo int64, now time.Time, clmID string, fields map[string]string) ([]byte, error) {
	m := make(map[string]string, len(fields)+4)
	for k, v := range fields {
		m[k] = v
	}
	m["p_no"] = strconv.FormatInt(pNo, 10)
	m["p_sd_date"] = formatSDDate(now)
	m["sCLMID"] = clmID
	m["sJsonOfmt"] = jsonFormat
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("tachibana: encode request: %w", err)
	}
	body, err := japanese.ShiftJIS.NewEncoder().Bytes(raw)
	if err != nil {
		return nil, errors.New("tachibana: request has characters Shift-JIS cannot hold")
	}
	return body, nil
}

// decodeShiftJIS converts a broker body to UTF-8.
func decodeShiftJIS(raw []byte) ([]byte, error) {
	out, err := japanese.ShiftJIS.NewDecoder().Bytes(raw)
	if err != nil {
		return nil, errors.New("tachibana: response is not valid Shift-JIS")
	}
	return out, nil
}

// flexInt decodes a number the broker may send as a JSON string ("-62"), a
// number, or an empty string.
type flexInt int

func (f *flexInt) UnmarshalJSON(b []byte) error {
	b = bytes.Trim(bytes.TrimSpace(b), `"`)
	if len(b) == 0 || string(b) == "null" {
		*f = 0
		return nil
	}
	n, err := strconv.Atoi(string(b))
	if err != nil {
		return errors.New("tachibana: malformed numeric field")
	}
	*f = flexInt(n)
	return nil
}

// envelope is the part of every response the client checks.
type envelope struct {
	CLMID      string  `json:"sCLMID"`
	Errno      flexInt `json:"p_errno"`
	Err        string  `json:"p_err"`
	ResultCode flexInt `json:"sResultCode"`
	ResultText string  `json:"sResultText"`
}

// decodeResponse decodes a Shift-JIS JSON body: first the envelope (p_errno,
// then sResultCode → *APIError), then, when out is not nil, the payload into
// out. The returned errors never include body content other than the
// broker's own error message.
func decodeResponse(raw []byte, out any) error {
	utf8, err := decodeShiftJIS(raw)
	if err != nil {
		return err
	}
	var env envelope
	if err := json.Unmarshal(utf8, &env); err != nil {
		return errors.New("tachibana: response is not the expected JSON")
	}
	if env.Errno != 0 {
		return &APIError{Errno: int(env.Errno), Text: textutil.Excerpt(env.Err, maxErrorText)}
	}
	if env.ResultCode != 0 {
		return &APIError{ResultCode: int(env.ResultCode), Text: textutil.Excerpt(env.ResultText, maxErrorText)}
	}
	if env.CLMID == "" {
		return errors.New("tachibana: response has no sCLMID")
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(utf8, out); err != nil {
		return errors.New("tachibana: response payload does not match the expected shape")
	}
	return nil
}
