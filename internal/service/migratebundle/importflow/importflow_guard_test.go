package importflow_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/migratebundle"
	"github.com/ousiassllc/pitha-trador/internal/service/migratebundle/bundletest"
	"github.com/ousiassllc/pitha-trador/internal/service/migratebundle/importflow"
)

func TestPreview_RefusesBadBundleWithoutTouchingAnything(t *testing.T) {
	target, path := newPC(t, true)
	before := bundletest.Counts(t, target)
	svc := importflow.New(target, path)
	if _, err := svc.Preview(context.Background(), strings.NewReader("not a bundle")); !errors.Is(err, migratebundle.ErrNotBundle) {
		t.Fatalf("err = %v, want ErrNotBundle", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(filepath.Dir(path), "migration", "staging")); len(entries) != 0 {
		t.Errorf("a refused upload left %d staged entries", len(entries))
	}
	if p, _ := svc.Pending(); p != nil {
		t.Error("pending import after a refused upload")
	}
	after := bundletest.Counts(t, target)
	for k, v := range before {
		if after[k] != v {
			t.Errorf("table %s changed: %d -> %d", k, v, after[k])
		}
	}
}

func TestConfirm_RejectsUnknownAndMalformedTokens(t *testing.T) {
	target, path := bundletest.OpenDB(t)
	svc := importflow.New(target, path)
	for _, token := range []string{"", "../../etc", strings.Repeat("0", 32), strings.Repeat("g", 32)} {
		if _, err := svc.Confirm(context.Background(), token, true); !errors.Is(err, importflow.ErrUnknownToken) {
			t.Errorf("Confirm(%q) err = %v, want ErrUnknownToken", token, err)
		}
	}
}

// A newer preview replaces an unconfirmed one; the old token stops working.
func TestPreview_ReplacesEarlierPreview(t *testing.T) {
	src, _ := bundletest.OpenDB(t)
	bundle := exportBundle(t, src)
	target, path := bundletest.OpenDB(t)
	svc := importflow.New(target, path)
	first, err := svc.Preview(context.Background(), bytes.NewReader(bundle))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Preview(context.Background(), bytes.NewReader(bundle)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Confirm(context.Background(), first.Token, true); !errors.Is(err, importflow.ErrUnknownToken) {
		t.Fatalf("err = %v, want ErrUnknownToken", err)
	}
}

func TestCancel_DiscardsStagedAndPending(t *testing.T) {
	src, _ := bundletest.OpenDB(t)
	bundle := exportBundle(t, src)
	target, path := bundletest.OpenDB(t)
	svc := importflow.New(target, path)
	p, err := svc.Preview(context.Background(), bytes.NewReader(bundle))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Confirm(context.Background(), p.Token, true); err != nil {
		t.Fatal(err)
	}
	if err := svc.Cancel(); err != nil {
		t.Fatal(err)
	}
	if pend, _ := svc.Pending(); pend != nil {
		t.Fatal("pending survived Cancel")
	}
	if res := importflow.ApplyPending(context.Background(), path, fixedNow); res != nil {
		t.Fatalf("ApplyPending after Cancel did something: %+v", res)
	}
}

// A staged database damaged between Confirm and the restart is refused at the
// start: the current database stays, the failure is recorded, and the pending
// import is dropped so the next start does not retry it.
func TestApplyPending_FailureKeepsCurrentDatabase(t *testing.T) {
	src, _ := bundletest.OpenDB(t)
	bundletest.Seed(t, src)
	bundle := exportBundle(t, src)
	target, path := newPC(t, true)
	svc := importflow.New(target, path)
	p, err := svc.Preview(context.Background(), bytes.NewReader(bundle))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Confirm(context.Background(), p.Token, true); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(filepath.Dir(path), "migration", "pending", "pitha.db")
	raw, err := os.ReadFile(staged)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)/2] ^= 0xFF
	if err := os.WriteFile(staged, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	res, reopened := restart(t, target, path)
	if res == nil || res.Status != importflow.StatusFailed || res.Error == "" {
		t.Fatalf("result = %+v", res)
	}
	if got := bundletest.Counts(t, reopened); got["instruments"] != 1 || got["secrets"] != 1 {
		t.Errorf("current database changed by a failed apply: %v", got)
	}
	if pend, _ := importflow.New(reopened, path).Pending(); pend != nil {
		t.Error("failed import still pending")
	}
	if last, _ := importflow.New(reopened, path).LastResult(); last == nil || last.Status != importflow.StatusFailed {
		t.Errorf("LastResult = %+v", last)
	}
}

func TestApplyPending_NothingPending(t *testing.T) {
	_, path := bundletest.OpenDB(t)
	if res := importflow.ApplyPending(context.Background(), path, fixedNow); res != nil {
		t.Fatalf("result = %+v, want nil", res)
	}
}
