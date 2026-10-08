package event_test

import (
	"encoding/base64"
	"testing"

	"golang.org/x/text/encoding/japanese"

	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/event"
)

func TestParseNotifications(t *testing.T) {
	// The notification examples of the EVENT I/F manual (^A ^B ^C delimiters).
	tests := []struct {
		name    string
		msg     string
		cmd     string
		no      int
		field   [2]string // one expected raw field
		wantErr bool
	}{
		{"KP", "p_no\x021\x01p_date\x022018.12.03-11:34:59.138\x01p_cmd\x02KP", "KP", 1, [2]string{}, false},
		{"ST", "p_no\x02208\x01p_date\x022018.12.03-13:11:22.122\x01p_errno\x022\x01p_err\x02session inactive.\x01p_cmd\x02ST", "ST", 208, [2]string{"p_errno", "2"}, false},
		{"SS", "p_no\x021\x01p_date\x022020.06.18-07:30:34.810\x01p_cmd\x02SS\x01p_PV\x02MSGSV\x01p_ENO\x023\x01p_ALT\x020\x01p_CT\x0220200618052959\x01p_LK\x021\x01p_SS\x021", "SS", 1, [2]string{"p_SS", "1"}, false},
		{"multi-valued item", "p_no\x021\x01p_cmd\x02FD\x01p_X\x02a\x03b\x03c", "FD", 1, [2]string{"p_X", "a\x03b\x03c"}, false},
		{"no command", "p_no\x021\x01p_date\x02x", "", 0, [2]string{}, true},
		{"garbage", "hello", "", 0, [2]string{}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n, err := event.Parse(tc.msg)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, want error %v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			if n.Cmd != tc.cmd || n.No != tc.no {
				t.Errorf("cmd/no = %s/%d, want %s/%d", n.Cmd, n.No, tc.cmd, tc.no)
			}
			if tc.field[0] != "" && n.Fields[tc.field[0]] != tc.field[1] {
				t.Errorf("field %s = %q, want %q", tc.field[0], n.Fields[tc.field[0]], tc.field[1])
			}
		})
	}
}

func TestParseFDRowsFirstSnapshotAndDifference(t *testing.T) {
	// Manual's FD example: a first full snapshot, then only changes.
	first := "p_no\x021\x01p_date\x022017.05.02-14:10:05.372\x01p_cmd\x02FD\x01" +
		"p_1_LISS\x0282509594\x01t_1_DPP:T\x0214:10\x01p_1_QBS\x020101\x01p_1_QAS\x020101\x01p_1_DPP\x026128\x01p_1_QAP\x026129\x01p_1_QBP\x026128\x01" +
		"p_2_DPP\x026533\x01t_2_DPP:T\x0214:08"
	n, err := event.Parse(first)
	if err != nil {
		t.Fatal(err)
	}
	rows := n.Rows()
	if len(rows) != 2 || rows[1]["pDPP"] != "6128" || rows[1]["tDPP:T"] != "14:10" || rows[1]["pQAP"] != "6129" || rows[1]["pLISS"] != "82509594" || rows[2]["pDPP"] != "6533" {
		t.Fatalf("rows = %v", rows)
	}
	diff, _ := event.Parse("p_no\x022\x01p_cmd\x02FD\x01p_1_DPP\x026130\x01p_1_DPG\x020057")
	if r := diff.Rows(); len(r) != 1 || r[1]["pDPP"] != "6130" || r[1]["pDPG"] != "0057" {
		t.Fatalf("diff rows = %v", r)
	}
	// Items that are not 時価 rows (p_ENO, p_errno) never become rows.
	ec, _ := event.Parse("p_no\x021\x01p_cmd\x02EC\x01p_ENO\x0210507\x01p_IC\x022468")
	if len(ec.Rows()) != 0 {
		t.Errorf("EC items parsed as rows: %v", ec.Rows())
	}
}

func TestTextDecodesBase64ShiftJIS(t *testing.T) {
	sjis, err := japanese.ShiftJIS.NewEncoder().Bytes([]byte("フュートレック"))
	if err != nil {
		t.Fatal(err)
	}
	n, _ := event.Parse("p_no\x021\x01p_cmd\x02EC\x01p_IN\x02" + base64.StdEncoding.EncodeToString(sjis) + "\x01p_BAD\x02***")
	if got, ok := n.Text("p_IN"); !ok || got != "フュートレック" {
		t.Errorf("Text(p_IN) = %q, %v", got, ok)
	}
	if _, ok := n.Text("p_BAD"); ok {
		t.Error("invalid base64 must not decode")
	}
	if _, ok := n.Text("p_MISSING"); ok {
		t.Error("a missing item must not decode")
	}
}
