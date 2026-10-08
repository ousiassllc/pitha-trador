package event

import (
	"log/slog"
	"sync"
	"time"
)

// maxOrderEvents bounds the recorded EC notifications.
const maxOrderEvents = 100

// OrderEvent is one EC (注文約定通知) notification as recorded. The adapter
// only records them (orders arrive with issue #55): no order content beyond
// what identifies the event is kept or logged.
type OrderEvent struct {
	At         time.Time
	EventNo    int64
	NotifyType string // p_NT
	OrderNo    string // p_ON
	Symbol     string // p_IC
	Name       string // p_IN, decoded
}

// Statuses is what SS (システムステータス) and US (運用ステータス) last said.
type Statuses struct {
	// SystemKnown is false until an SS arrived; then SystemOpen is p_SS=0 (開局).
	SystemKnown bool
	SystemOpen  bool
	// LoginAllowed is p_LK (1 = ログイン許可).
	LoginAllowed string
	SystemAt     time.Time
	// Operations is the last US status per "市場コード/運用ユニット".
	Operations map[string]string
}

type recorder struct {
	mu       sync.Mutex
	orders   []OrderEvent
	statuses Statuses
}

func (r *recorder) order(n Notification, now time.Time) {
	ev := OrderEvent{At: now, EventNo: n.eventNo(), NotifyType: n.Fields["p_NT"], OrderNo: n.Fields["p_ON"], Symbol: n.Fields["p_IC"]}
	ev.Name, _ = n.Text("p_IN")
	r.mu.Lock()
	r.orders = append(r.orders, ev)
	if len(r.orders) > maxOrderEvents {
		r.orders = append([]OrderEvent(nil), r.orders[len(r.orders)-maxOrderEvents:]...)
	}
	r.mu.Unlock()
	slog.Info("tachibana: order notification recorded", "event_no", ev.EventNo, "type", ev.NotifyType, "symbol", ev.Symbol)
}

func (r *recorder) system(n Notification, now time.Time) {
	r.mu.Lock()
	changed := !r.statuses.SystemKnown || r.statuses.SystemOpen != (n.Fields["p_SS"] == "0")
	r.statuses.SystemKnown = true
	r.statuses.SystemOpen = n.Fields["p_SS"] == "0"
	r.statuses.LoginAllowed = n.Fields["p_LK"]
	r.statuses.SystemAt = now
	open := r.statuses.SystemOpen
	r.mu.Unlock()
	if changed {
		slog.Info("tachibana: system status", "open", open, "login_allowed", n.Fields["p_LK"])
	}
}

func (r *recorder) operation(n Notification) {
	key := n.Fields["p_MC"] + "/" + n.Fields["p_UU"]
	r.mu.Lock()
	if r.statuses.Operations == nil {
		r.statuses.Operations = map[string]string{}
	}
	r.statuses.Operations[key] = n.Fields["p_US"]
	r.mu.Unlock()
}

// Orders returns the recorded EC notifications, oldest first.
func (f *Feed) Orders() []OrderEvent {
	f.rec.mu.Lock()
	defer f.rec.mu.Unlock()
	return append([]OrderEvent(nil), f.rec.orders...)
}

// Statuses returns the last SS and US statuses.
func (f *Feed) Statuses() Statuses {
	f.rec.mu.Lock()
	defer f.rec.mu.Unlock()
	s := f.rec.statuses
	s.Operations = make(map[string]string, len(f.rec.statuses.Operations))
	for k, v := range f.rec.statuses.Operations {
		s.Operations[k] = v
	}
	return s
}
