package tachibanatest

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// EventConn is one EVENT WebSocket connection the fake accepted.
type EventConn struct {
	// N is the 1-based connection counter.
	N int
	// Path is the request path (it carries the virtual URL's marker).
	Path string
	// Query is the parsed query (p_gyou_no, p_issue_code, p_eno, ...).
	Query url.Values
	// RawQuery is the query as sent (commas unescaped).
	RawQuery string

	ws   *websocket.Conn
	done chan struct{}
}

// Send writes one notification as a text frame.
func (e *EventConn) Send(msg string) error {
	return e.ws.Write(context.Background(), websocket.MessageText, []byte(msg))
}

// Close ends the connection from the server side (normal closure).
func (e *EventConn) Close() { _ = e.ws.Close(websocket.StatusNormalClosure, "") }

// Drop kills the connection without a close handshake.
func (e *EventConn) Drop() { _ = e.ws.CloseNow() }

// Done is closed once the connection ended (either side).
func (e *EventConn) Done() <-chan struct{} { return e.done }

// Events returns the accepted connections, oldest first.
func (f *FakeBroker) Events() []*EventConn {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*EventConn(nil), f.events...)
}

// WaitEvent waits for the n-th (1-based) EVENT connection.
func (f *FakeBroker) WaitEvent(t *testing.T, n int) *EventConn {
	t.Helper()
	Eventually(t, func() bool { return len(f.Events()) >= n })
	return f.Events()[n-1]
}

func isWebSocket(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

func (f *FakeBroker) serveEvent(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	q, _ := url.ParseQuery(r.URL.RawQuery)
	f.mu.Lock()
	ev := &EventConn{N: len(f.events) + 1, Path: r.URL.Path, Query: q, RawQuery: r.URL.RawQuery, ws: ws, done: make(chan struct{})}
	f.events = append(f.events, ev)
	f.mu.Unlock()
	defer close(ev.done)
	for { // returns when the client closes or the connection drops
		if _, _, err := ws.Read(context.Background()); err != nil {
			return
		}
	}
}

const (
	sepItem  = "\x01"
	sepValue = "\x02"
)

func header(no int, cmd string) []string {
	return []string{"p_no" + sepValue + strconv.Itoa(no), "p_date" + sepValue + time.Now().Format("2006.01.02-15:04:05.000"), "p_cmd" + sepValue + cmd}
}

func join(items []string) string { return strings.Join(items, sepItem) }

// FD builds a 時価 notification: rows maps a row number to item names as the
// REST answer spells them ("pDPP", "tDPP:T") and their values.
func FD(no int, rows map[int]map[string]string) string {
	items := header(no, "FD")
	nums := make([]int, 0, len(rows))
	for r := range rows {
		nums = append(nums, r)
	}
	sort.Ints(nums)
	for _, r := range nums {
		names := make([]string, 0, len(rows[r]))
		for k := range rows[r] {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			items = append(items, fmt.Sprintf("%c_%d_%s%s%s", k[0], r, k[1:], sepValue, rows[r][k]))
		}
	}
	return join(items)
}

// KP is the keep-alive notification.
func KP(no int) string { return join(header(no, "KP")) }

// ST is the error-status notification (the broker closes after it).
func ST(no, errno int, text string) string {
	items := header(no, "ST")
	return join(append(items, "p_errno"+sepValue+strconv.Itoa(errno), "p_err"+sepValue+text))
}

// SS is the system-status notification (open=true is 開局).
func SS(no int, eventNo int, open bool) string {
	status := "1"
	if open {
		status = "0"
	}
	items := header(no, "SS")
	return join(append(items, "p_PV"+sepValue+"MSGSV", "p_ENO"+sepValue+strconv.Itoa(eventNo), "p_ALT"+sepValue+"1", "p_LK"+sepValue+"1", "p_SS"+sepValue+status))
}

// EC is an order-execution notification; nameB64 is the base64 of the
// Shift-JIS issue name (p_IN).
func EC(no, eventNo int, orderNo, symbol, nameB64 string) string {
	items := header(no, "EC")
	return join(append(items, "p_PV"+sepValue+"MSGSV", "p_ENO"+sepValue+strconv.Itoa(eventNo), "p_ALT"+sepValue+"1", "p_NT"+sepValue+"12",
		"p_ON"+sepValue+orderNo, "p_IC"+sepValue+symbol, "p_IN"+sepValue+nameB64))
}
