package tachibanawatch_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/tachibanawatch"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	tt "github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/tachibanatest"
)

func TestSessionDate(t *testing.T) {
	for _, tc := range []struct {
		name string
		now  time.Time
		want string
	}{
		{"before the open", tt.AtJST(2026, 10, 8, 6, 0), "2026-10-08"},
		{"mid-session", tt.AtJST(2026, 10, 8, 10, 0), "2026-10-08"},
		{"just before the close", tt.AtJST(2026, 10, 8, 15, 29), "2026-10-08"},
		{"after the close", tt.AtJST(2026, 10, 8, 15, 30), "2026-10-09"},
		{"Friday evening skips the weekend and the holiday", tt.AtJST(2026, 10, 9, 19, 0), "2026-10-13"},
		{"Saturday", tt.AtJST(2026, 10, 10, 10, 0), "2026-10-13"},
		{"the holiday itself", tt.AtJST(2026, 10, 12, 7, 0), "2026-10-13"},
	} {
		if got := tachibanawatch.SessionDate(tc.now); got != tc.want {
			t.Errorf("%s: SessionDate = %s, want %s", tc.name, got, tc.want)
		}
	}
}

func saveList(t *testing.T, f *fixture, date string, symbols ...string) {
	t.Helper()
	l := domain.WatchList{ListDate: date, Source: domain.WatchListDailyScreen, DecidedAt: f.clock.Now()}
	for _, s := range symbols {
		l.Entries = append(l.Entries, domain.WatchListEntry{Symbol: s, Origin: domain.WatchOriginScreen})
	}
	if err := f.lists.Save(context.Background(), l); err != nil {
		t.Fatalf("Save list: %v", err)
	}
}

func TestSource_ServesTheListOfTheCurrentSessionAndTheNextOneAfterTheClose(t *testing.T) {
	f := newFixture(t, tt.AtJST(2026, 10, 8, 6, 0))
	src := tachibanawatch.Source{Lists: f.lists, Clock: f.clock}
	if _, err := src.Candidates(context.Background()); !errors.Is(err, tachibanawatch.ErrNoList) {
		t.Fatalf("before any list: err = %v, want ErrNoList", err)
	}

	saveList(t, f, "2026-10-08", "T1", "T2")
	saveList(t, f, "2026-10-09", "F1")
	got, err := src.Candidates(context.Background())
	if err != nil || fmt.Sprint(got) != "[T1 T2]" {
		t.Fatalf("morning = %v, %v; want today's list in subscription order", got, err)
	}
	f.clock.Set(tt.AtJST(2026, 10, 8, 15, 30))
	if got, _ = src.Candidates(context.Background()); fmt.Sprint(got) != "[F1]" {
		t.Errorf("after the close = %v, want the next 立会日's list", got)
	}
}

func TestSource_KeepsTheLatestEarlierListWhileTodaysIsMissing(t *testing.T) {
	f := newFixture(t, tt.AtJST(2026, 10, 9, 6, 0))
	saveList(t, f, "2026-10-08", "T1")
	got, err := tachibanawatch.Source{Lists: f.lists, Clock: f.clock}.Candidates(context.Background())
	if err != nil || fmt.Sprint(got) != "[T1]" {
		t.Fatalf("Candidates = %v, %v; want the earlier list while today's is not decided", got, err)
	}
}

func TestViewer_ListsAndFallbackNotice(t *testing.T) {
	f := newFixture(t, tt.AtJST(2026, 10, 9, 6, 0))
	viewer := tachibanawatch.Viewer{Lists: f.lists, Clock: f.clock}
	if n := viewer.WatchNotice(context.Background()); n != "" {
		t.Fatalf("notice with no list = %q", n)
	}
	saveList(t, f, "2026-10-08", "T1")
	fallback := domain.WatchList{ListDate: "2026-10-09", Source: domain.WatchListCarriedOver, Reason: "日足を使えませんでした", DecidedAt: f.clock.Now()}
	if err := f.lists.Save(context.Background(), fallback); err != nil {
		t.Fatal(err)
	}

	view, err := viewer.WatchLists(context.Background())
	if err != nil || !view.Enabled || view.ActiveDate != "2026-10-09" || len(view.Lists) != 2 || view.Lists[0].ListDate != "2026-10-09" {
		t.Fatalf("view = %+v, %v", view, err)
	}
	if n := viewer.WatchNotice(context.Background()); n != "日足を使えませんでした" {
		t.Errorf("notice = %q, want the fallback's reason while it is in use", n)
	}
	f.clock.Set(tt.AtJST(2026, 10, 9, 19, 0)) // the next list takes over after the close
	if n := viewer.WatchNotice(context.Background()); n != "" {
		t.Errorf("notice after the fallback day = %q, want none", n)
	}
}
