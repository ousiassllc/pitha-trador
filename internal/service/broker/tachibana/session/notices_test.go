package session_test

import (
	"errors"
	"os"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/session"
	tt "github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/tachibanatest"
)

func TestVersionAndDocumentNotices(t *testing.T) {
	r := newRig(t, tt.AtJST(2026, 10, 8, 6, 0))
	set := func(spec, doc string) {
		r.fb.SetLoginExtra(map[string]any{"sUpdateInformAPISpecFunction": spec, "sUpdateInformWebDocument": doc})
	}
	login := func() {
		t.Helper()
		if err := r.a.Session().LoginNow(r.ctx); err != nil {
			t.Fatal(err)
		}
	}

	set("", "")
	login()
	if r.totalNotices() != 0 || r.a.Status().VersionRetiring {
		t.Fatalf("nothing announced yet: notices=%d status=%+v", r.totalNotices(), r.a.Status())
	}

	set("20261101", "20261015")
	login()
	if r.countNotices(session.NoticeAPISpecUpdate) != 1 || r.countNotices(session.NoticeDocumentUpdate) != 1 {
		t.Fatalf("want one notice of each kind, got %d total", r.totalNotices())
	}
	if !r.a.Status().VersionRetiring {
		t.Error("VersionRetiring must be set while a release date is pending")
	}

	login() // same values: the dates stay in the response until the next one is set
	if r.totalNotices() != 2 {
		t.Errorf("an unchanged date raised a notice again (%d notices)", r.totalNotices())
	}

	set("20261201", "20261015")
	login()
	if r.countNotices(session.NoticeAPISpecUpdate) != 2 || r.countNotices(session.NoticeDocumentUpdate) != 1 {
		t.Errorf("want a new API notice only, got %d total", r.totalNotices())
	}

	set("20260901", "20260901") // a date in the past is no news (予定日 < 当日)
	login()
	if r.totalNotices() != 3 || r.a.Status().VersionRetiring {
		t.Errorf("past dates: notices=%d status=%+v", r.totalNotices(), r.a.Status())
	}

	set("20261008", "") // today counts
	login()
	if r.countNotices(session.NoticeAPISpecUpdate) != 3 {
		t.Errorf("a release date of today must be announced (%d notices)", r.totalNotices())
	}
}

func TestUnreadDocumentsAreReportedWithoutSession(t *testing.T) {
	r := newRig(t, tt.AtJST(2026, 10, 8, 6, 0))
	r.fb.SetLoginExtra(map[string]any{
		"sKinsyouhouMidokuFlg": "1", "sUrlRequest": "", "sUrlMaster": "", "sUrlPrice": "", "sUrlEvent": "", "sUrlEventWebSocket": "",
	})
	for range 2 {
		if err := r.a.Session().LoginNow(r.ctx); !errors.Is(err, tachibana.ErrDocumentsUnread) {
			t.Fatalf("err = %v, want ErrDocumentsUnread", err)
		}
	}
	st := r.a.Status()
	if st.Issue != broker.SessionIssueDocumentsUnread || !st.DocumentsUnread || st.Guidance != session.GuidanceDocuments || st.Failures != 2 {
		t.Fatalf("status = %+v", st)
	}
	if n := r.countNotices(session.NoticeDocumentsUnread); n != 1 {
		t.Errorf("notices = %d, want 1", n)
	}
	if err := r.call(); !errors.Is(err, broker.ErrNoSession) {
		t.Errorf("call err = %v: no virtual URL was issued", err)
	}

	r.fb.SetLoginExtra(nil)
	if err := r.a.Session().LoginNow(r.ctx); err != nil {
		t.Fatal(err)
	}
	if st := r.a.Status(); st.Failed() || st.DocumentsUnread {
		t.Errorf("status after reading the documents = %+v", st)
	}
}

func TestUnreadablePrivateKeyIsReportedWithoutARequest(t *testing.T) {
	r := newRig(t, tt.AtJST(2026, 10, 8, 6, 0))
	settings := r.fb.Settings()
	if err := os.Remove(settings.DemoPrivateKeyPath); err != nil {
		t.Fatal(err)
	}
	a := newAdapterWith(r, settings)
	if err := a.Session().LoginNow(r.ctx); !errors.Is(err, tachibana.ErrKeyUnreadable) {
		t.Fatalf("err = %v", err)
	}
	if st := a.Status(); st.Issue != broker.SessionIssueKeyMismatch || st.Guidance == "" {
		t.Errorf("status = %+v", st)
	}
	if len(r.fb.Requests()) != 0 {
		t.Error("a login was sent without a usable key")
	}
}
