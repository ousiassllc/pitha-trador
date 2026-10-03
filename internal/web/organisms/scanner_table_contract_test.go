package organisms_test

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// scannerContract is static/src/components/scanner-table/scanner-contract.json:
// the golden column definitions and cell renderings that the SSR fallback
// (this package) and the hydrating Lit component (scanner-contract.test.ts)
// must both produce, so the view before and after hydration cannot drift
// (issue #239 parity).
type scannerContract struct {
	Columns []struct {
		Label   string `json:"label"`
		Hint    string `json:"hint"`
		Numeric bool   `json:"numeric"`
	} `json:"columns"`
	EmptyMessage string `json:"emptyMessage"`
	Rows         []struct {
		Name string `json:"name"`
		Item struct {
			Symbol          string   `json:"symbol"`
			Price           float64  `json:"price"`
			Return1m        *float64 `json:"return_1m"`
			Return5m        *float64 `json:"return_5m"`
			VolumeRatio5m   *float64 `json:"volume_ratio_5m"`
			PriceVsVWAPBps  float64  `json:"price_vs_vwap_bps"`
			SpreadBps       *float64 `json:"spread_bps"`
			JevDirection    *string  `json:"jev_direction"`
			JevConfidence   *float64 `json:"jev_confidence"`
			EntryQuality    *string  `json:"entry_quality"`
			CurrentPosition *float64 `json:"current_position"`
		} `json:"item"`
		Href           string   `json:"href"`
		Cells          []string `json:"cells"`
		ReturnClasses  []string `json:"returnClasses"`
		DirectionClass string   `json:"directionClass"`
		QualityClass   string   `json:"qualityClass"`
	} `json:"rows"`
}

// percentToRatio turns the contract's percent-unit return (the API/Lit
// shape) into the Feature Engine decimal ratio that domain.Candidate holds
// and the SSR fallback converts back to percent for display.
func percentToRatio(v *float64) *float64 {
	if v == nil {
		return nil
	}
	r := *v / 100
	return &r
}

func loadScannerContract(t *testing.T) scannerContract {
	t.Helper()
	data, err := os.ReadFile("../../../static/src/components/scanner-table/scanner-contract.json")
	if err != nil {
		t.Fatalf("read contract: %v", err)
	}
	var c scannerContract
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatalf("parse contract: %v", err)
	}
	return c
}

func parseHTML(t *testing.T, body string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse html: %v", err)
	}
	return doc
}

func findAll(n *html.Node, match func(*html.Node) bool) []*html.Node {
	var out []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && match(n) {
			out = append(out, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return out
}

func tag(name string) func(*html.Node) bool {
	return func(n *html.Node) bool { return n.Data == name }
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func text(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		sb.WriteString(text(c))
	}
	return sb.String()
}

// classSet compares class lists regardless of token order.
func classSet(class string) []string {
	tokens := strings.Fields(class)
	slices.Sort(tokens)
	return tokens
}

func TestScannerTableFallback_MatchesLitContract_Columns(t *testing.T) {
	contract := loadScannerContract(t)
	doc := parseHTML(t, renderScannerTable(t, nil))

	ths := findAll(doc, tag("th"))
	if len(ths) != len(contract.Columns) {
		t.Fatalf("got %d <th>, contract has %d columns", len(ths), len(contract.Columns))
	}
	for i, want := range contract.Columns {
		th := ths[i]
		if got := strings.TrimSpace(text(th)); got != want.Label {
			t.Errorf("column %d label = %q, want %q", i, got, want.Label)
		}
		if got := attr(th, "title"); got != want.Hint {
			t.Errorf("column %d (%s) hint = %q, want %q", i, want.Label, got, want.Hint)
		}
		if got := slices.Contains(strings.Fields(attr(th, "class")), "text-right"); got != want.Numeric {
			t.Errorf("column %d (%s) right-aligned = %v, want %v", i, want.Label, got, want.Numeric)
		}
	}
}

func TestScannerTableFallback_MatchesLitContract_ColumnHelp(t *testing.T) {
	contract := loadScannerContract(t)
	doc := parseHTML(t, renderScannerTable(t, nil))

	help := findAll(doc, func(n *html.Node) bool { return attr(n, "data-testid") == "scanner-column-help" })
	if len(help) != 1 {
		t.Fatalf("got %d column-help blocks, want 1", len(help))
	}
	dts, dds := findAll(help[0], tag("dt")), findAll(help[0], tag("dd"))
	if len(dts) != len(contract.Columns) || len(dds) != len(contract.Columns) {
		t.Fatalf("got %d <dt>/%d <dd>, want %d each", len(dts), len(dds), len(contract.Columns))
	}
	for i, want := range contract.Columns {
		if got := text(dts[i]); got != want.Label {
			t.Errorf("help term %d = %q, want %q", i, got, want.Label)
		}
		if got := text(dds[i]); got != want.Hint {
			t.Errorf("help description %d = %q, want %q", i, got, want.Hint)
		}
	}
}

func TestScannerTableFallback_MatchesLitContract_Rows(t *testing.T) {
	contract := loadScannerContract(t)
	for _, row := range contract.Rows {
		t.Run(row.Name, func(t *testing.T) {
			it := row.Item
			doc := parseHTML(t, renderScannerTable(t, []domain.Candidate{{
				Symbol: it.Symbol, Price: it.Price,
				Return1m: percentToRatio(it.Return1m), Return5m: percentToRatio(it.Return5m),
				VolumeRatio5m: it.VolumeRatio5m, PriceVsVWAPBps: it.PriceVsVWAPBps, SpreadBps: it.SpreadBps,
				JevDirection: it.JevDirection, JevConfidence: it.JevConfidence,
				EntryQuality: it.EntryQuality, CurrentPosition: it.CurrentPosition,
			}}))

			trs := findAll(doc, func(n *html.Node) bool { return n.Data == "tr" && attr(n, "data-symbol") != "" })
			if len(trs) != 1 {
				t.Fatalf("got %d candidate rows, want 1", len(trs))
			}
			tds := findAll(trs[0], tag("td"))
			if len(tds) != len(row.Cells) {
				t.Fatalf("got %d cells, want %d", len(tds), len(row.Cells))
			}
			for i, want := range row.Cells {
				if got := strings.TrimSpace(text(tds[i])); got != want {
					t.Errorf("cell %d = %q, want %q", i, got, want)
				}
			}

			if got := attr(findAll(tds[0], tag("a"))[0], "href"); got != row.Href {
				t.Errorf("href = %q, want %q", got, row.Href)
			}
			for i, cell := range []int{2, 3} {
				if got, want := classSet(attr(tds[cell], "class")), classSet(row.ReturnClasses[i]); !slices.Equal(got, want) {
					t.Errorf("cell %d class = %v, want %v", cell, got, want)
				}
			}
			for cell, want := range map[int]string{7: row.DirectionClass, 9: row.QualityClass} {
				span := findAll(tds[cell], tag("span"))
				if len(span) != 1 {
					t.Fatalf("cell %d: got %d <span>, want 1", cell, len(span))
				}
				if got := classSet(attr(span[0], "class")); !slices.Equal(got, classSet(want)) {
					t.Errorf("cell %d badge class = %v, want %v", cell, got, classSet(want))
				}
			}
		})
	}
}

func TestScannerTableFallback_MatchesLitContract_EmptyState(t *testing.T) {
	contract := loadScannerContract(t)
	doc := parseHTML(t, renderScannerTable(t, []domain.Candidate{}))

	empty := findAll(doc, func(n *html.Node) bool { return attr(n, "data-testid") == "scanner-empty" })
	if len(empty) != 1 {
		t.Fatalf("got %d empty-state notices, want 1", len(empty))
	}
	if got := strings.Join(strings.Fields(text(empty[0])), " "); got != contract.EmptyMessage {
		t.Errorf("empty message = %q, want %q", got, contract.EmptyMessage)
	}
	// The notice exists at page load, where a live region would not be
	// announced; the Lit version (inserted later) does carry role=status.
	if role := attr(empty[0], "role"); role != "" {
		t.Errorf("SSR empty state role = %q, want none", role)
	}
}
