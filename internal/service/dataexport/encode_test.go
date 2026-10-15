package dataexport

import (
	"bytes"
	"math"
	"strings"
	"testing"
	"time"
)

func TestPrepareRow_MasksByColumnNameAndContent(t *testing.T) {
	columns := []Column{
		{Name: "api_key"}, {Name: "sSecondPassword"}, {Name: "note"}, {Name: "doc", JSON: true}, {Name: "empty_token"},
	}
	row := Row{"k-1", "p-2", "GET /x?token=abc&y=1", `{"password":"p","ok":1}`, nil}
	got, err := prepareRow(columns, row)
	if err != nil {
		t.Fatalf("prepareRow: %v", err)
	}
	joined := strings.Join([]string{got[0].(string), got[1].(string), got[2].(string), got[3].(string)}, "|")
	for _, leaked := range []string{"k-1", "p-2", "token=abc", `"p"`} {
		if strings.Contains(joined, leaked) {
			t.Errorf("%q survived masking: %s", leaked, joined)
		}
	}
	if !strings.Contains(got[2].(string), "y=1") || !strings.Contains(got[3].(string), `"ok":1`) {
		t.Errorf("non-secret data was lost: %v", got)
	}
	if got[4] != nil {
		t.Errorf("NULL under a sensitive column = %v, want nil (nothing to hide)", got[4])
	}
}

func TestPrepareRow_NormalizesValues(t *testing.T) {
	type named string
	columns := cols("a", "b", "c", "d", "e", "f")
	got, err := prepareRow(columns, Row{int32(7), uint8(3), named("x"), math.NaN(), time.Date(2026, 9, 28, 9, 30, 0, 0, time.FixedZone("JST", 9*3600)), float32(0.5)})
	if err != nil {
		t.Fatalf("prepareRow: %v", err)
	}
	want := []any{int64(7), int64(3), "x", nil, "2026-09-28T00:30:00Z", 0.5}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("column %d = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func TestPrepareRow_Errors(t *testing.T) {
	if _, err := prepareRow(cols("a"), Row{1, 2}); err == nil {
		t.Error("row/column count mismatch: want error")
	}
	if _, err := prepareRow(cols("a"), Row{struct{}{}}); err == nil {
		t.Error("unsupported type: want error")
	}
	if _, err := prepareRow(cols("a"), Row{uint64(math.MaxUint64)}); err == nil {
		t.Error("uint64 overflow: want error")
	}
}

func TestJSONLEncoder_InvalidJSONColumnStaysAString(t *testing.T) {
	var buf bytes.Buffer
	enc, err := newEncoder(FormatJSONL, &buf, []Column{{Name: "resp", JSON: true}, {Name: "n"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := enc.write([]any{"not json", int64(1)}); err != nil {
		t.Fatal(err)
	}
	if err := enc.write([]any{`{"a": [1, 2]}`, nil}); err != nil {
		t.Fatal(err)
	}
	want := `{"resp":"not json","n":1}` + "\n" + `{"resp":{"a":[1,2]},"n":null}` + "\n"
	if buf.String() != want {
		t.Fatalf("jsonl =\n%s\nwant\n%s", buf.String(), want)
	}
}

func TestCSVEncoder_QuotesAndNulls(t *testing.T) {
	var buf bytes.Buffer
	enc, err := newEncoder(FormatCSV, &buf, cols("a", "b", "c"))
	if err != nil {
		t.Fatal(err)
	}
	if err := enc.write([]any{`he said "hi", ok`, nil, true}); err != nil {
		t.Fatal(err)
	}
	if err := enc.flush(); err != nil {
		t.Fatal(err)
	}
	want := "a,b,c\n\"he said \"\"hi\"\", ok\",,true\n"
	if buf.String() != want {
		t.Fatalf("csv = %q, want %q", buf.String(), want)
	}
}
