package importflow_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/migratebundle/bundletest"
	"github.com/ousiassllc/pitha-trador/internal/service/migratebundle/importflow"
)

// The 立花 base URLs are this PC's own: the import leaves them as they were,
// whatever the old PC had.
func TestImport_KeepsThisPCsTachibanaBaseURLs(t *testing.T) {
	src, _ := bundletest.OpenDB(t)
	bundletest.Seed(t, src)
	bundle := exportBundle(t, src)

	target, path := newPC(t, false)
	const own = `"https://this-pc.invalid/"`
	bundletest.Exec(t, target, `INSERT INTO runtime_settings (key, value, updated_at) VALUES ('`+config.KeyTachibanaDemoBaseURL+`', '`+own+`', 't')`)
	svc := importflow.New(target, path)
	preview, err := svc.Preview(context.Background(), bytes.NewReader(bundle))
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if _, err := svc.Confirm(context.Background(), preview.Token, false); err != nil {
		t.Fatal(err)
	}
	res, imported := restart(t, target, path)
	if res == nil || res.Status != importflow.StatusApplied {
		t.Fatalf("result = %+v", res)
	}
	if v := singleValue(t, imported, `SELECT value FROM runtime_settings WHERE key='`+config.KeyTachibanaDemoBaseURL+`'`); v != own {
		t.Errorf("demo base_url after import = %q, want this PC's own", v)
	}
	var n int
	if err := imported.QueryRow(`SELECT count(*) FROM runtime_settings WHERE key=?`, config.KeyTachibanaProdBaseURL).Scan(&n); err != nil || n != 0 {
		t.Errorf("production base_url rows = %d (%v), want none", n, err)
	}
}

// Confirmed on an empty database, the target then gained data: the start-up
// apply must not replace it without the replace confirmation.
func TestApplyPending_RefusesToReplaceDataAddedAfterConfirm(t *testing.T) {
	src, _ := bundletest.OpenDB(t)
	bundletest.Seed(t, src)
	bundle := exportBundle(t, src)
	target, path := newPC(t, false)
	svc := importflow.New(target, path)
	preview, err := svc.Preview(context.Background(), bytes.NewReader(bundle))
	if err != nil || preview.TargetHasData {
		t.Fatalf("Preview: %+v, %v", preview, err)
	}
	pend, err := svc.Confirm(context.Background(), preview.Token, false)
	if err != nil || pend.ReplacedExisting {
		t.Fatalf("Confirm: %+v, %v", pend, err)
	}
	bundletest.Exec(t, target, `INSERT INTO instruments (symbol, name, market, is_active, created_at, updated_at) VALUES ('1111', 'n', 'TSE', 1, 't', 't')`)
	before := bundletest.Counts(t, target)

	res, reopened := restart(t, target, path)
	if res == nil || res.Status != importflow.StatusFailed || !strings.Contains(res.Error, importflow.ErrReplaceNotConfirmed.Error()) {
		t.Fatalf("result = %+v, want a failure naming ErrReplaceNotConfirmed", res)
	}
	if res.SafetyCopy != "" {
		t.Errorf("a refused apply took a safety copy: %q", res.SafetyCopy)
	}
	for k, v := range bundletest.Counts(t, reopened) {
		if before[k] != v {
			t.Errorf("table %s changed by a refused apply: %d -> %d", k, before[k], v)
		}
	}
	if p, _ := importflow.New(reopened, path).Pending(); p != nil {
		t.Error("refused import still pending")
	}
	if last, _ := importflow.New(reopened, path).LastResult(); last == nil || last.Status != importflow.StatusFailed {
		t.Errorf("LastResult = %+v", last)
	}
}

// A staged preview or a marker-less pending folder left on disk (a crash, a
// preview never confirmed) is removed at start; nothing is applied.
func TestApplyPending_RemovesLeftovers(t *testing.T) {
	_, path := bundletest.OpenDB(t)
	root := filepath.Join(filepath.Dir(path), "migration")
	leftovers := []string{filepath.Join(root, "staging", strings.Repeat("a", 32)), filepath.Join(root, "pending")}
	for _, dir := range leftovers {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "pitha.db"), []byte("leftover"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if res := importflow.ApplyPending(context.Background(), path, fixedNow); res != nil {
		t.Fatalf("result = %+v, want nil", res)
	}
	for _, dir := range leftovers {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("%s survived the start (stat err %v)", dir, err)
		}
	}
}
