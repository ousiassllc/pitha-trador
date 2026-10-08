// Package event is the 立花 e支店 EVENT I/F (WebSocket) receiver (issue
// #737): it keeps one connection to the EVENT virtual URL, parses the
// notifications (FD 時価, KP, ST, EC, SS, US), merges 時価 per symbol into the
// latest neutral Quote and serves broker.StreamFeed's Latest: the EVENT value
// when it is at most 30 seconds old, else a rate-limited REST 時価 refill.
package event

import (
	"encoding/base64"
	"errors"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/text/encoding/japanese"
)

// Delimiters of a notification: ^A between items, ^B between an item name and
// its value, ^C between values of one item.
const (
	sepItem  = "\x01"
	sepValue = "\x02"
	sepMulti = "\x03"
)

// Notification is one parsed EVENT message: the common items plus the items
// of its kind (Fields keeps the raw values, ^C-joined for multi-valued items).
type Notification struct {
	// No is p_no, the per-connection sequence (not the event number).
	No int
	// Date is p_date, the sending time as the broker wrote it.
	Date string
	// Cmd is p_cmd: ST, KP, FD, EC, NS, SS, US, ...
	Cmd    string
	Fields map[string]string
}

// ErrMalformed is returned for a message that is not an EVENT notification.
var ErrMalformed = errors.New("tachibana: malformed EVENT notification")

// Parse splits a notification message into its items.
func Parse(msg string) (Notification, error) {
	n := Notification{Fields: map[string]string{}}
	for _, item := range strings.Split(msg, sepItem) {
		name, val, found := strings.Cut(item, sepValue)
		if !found || name == "" {
			continue
		}
		switch name {
		case "p_no":
			n.No, _ = strconv.Atoi(val)
		case "p_date":
			n.Date = val
		case "p_cmd":
			n.Cmd = val
		default:
			n.Fields[name] = val
		}
	}
	if n.Cmd == "" {
		return Notification{}, ErrMalformed
	}
	return n, nil
}

// Values splits the multi-valued item name.
func (n Notification) Values(name string) []string {
	v, ok := n.Fields[name]
	if !ok {
		return nil
	}
	return strings.Split(v, sepMulti)
}

// Text decodes a Japanese item (p_IN, p_HDL, p_TX): the WebSocket form sends
// them as base64 of Shift-JIS bytes.
func (n Notification) Text(name string) (string, bool) {
	raw, ok := n.Fields[name]
	if !ok || raw == "" {
		return "", false
	}
	sjis, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return "", false
	}
	utf8, err := japanese.ShiftJIS.NewDecoder().Bytes(sjis)
	if err != nil {
		return "", false
	}
	return string(utf8), true
}

// rowItem matches a 時価 item "p_<行番号>_<情報コード>": the type letter (p
// plain, t time, x hex), the row number and the information code.
var rowItem = regexp.MustCompile(`^([ptx])_(\d+)_(.+)$`)

// Rows groups the 時価 items by row number. Each row maps the REST item name
// (type letter + information code: "pDPP", "tDPP:T", "pGBV1") to its value,
// the names the market package reads.
func (n Notification) Rows() map[int]map[string]string {
	rows := map[int]map[string]string{}
	for name, val := range n.Fields {
		m := rowItem.FindStringSubmatch(name)
		if m == nil {
			continue
		}
		row, err := strconv.Atoi(m[2])
		if err != nil {
			continue
		}
		if rows[row] == nil {
			rows[row] = map[string]string{}
		}
		rows[row][m[1]+m[3]] = val
	}
	return rows
}

// eventNo is the notification's p_ENO (0 when it has none).
func (n Notification) eventNo() int64 {
	v, _ := strconv.ParseInt(n.Fields["p_ENO"], 10, 64)
	return v
}
