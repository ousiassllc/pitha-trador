package importflow

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/migratebundle"
)

var (
	// ErrUnknownToken: the token names no staged bundle (never previewed, replaced by a newer preview, or cancelled).
	ErrUnknownToken = errors.New("importflow: no staged bundle for this token; upload the file again")
	// ErrReplaceNotConfirmed: the current database has data and the operator did not confirm replacing it.
	ErrReplaceNotConfirmed = errors.New("importflow: the current database has data; replacing it needs explicit confirmation")
)

// TableComparison is one table's row count in the current database and in the bundle.
type TableComparison struct {
	Name    string `json:"name"`
	Current int64  `json:"current"`
	Bundle  int64  `json:"bundle"`
}

// Preview is what an uploaded bundle would change; nothing has been changed yet.
type Preview struct {
	Token    string                 `json:"token"`
	Manifest migratebundle.Manifest `json:"manifest"`
	Tables   []TableComparison      `json:"tables"`
	// CurrentSchemaVersion is this build's schema; a lower Manifest.SchemaVersion
	// means the bundle is migrated forward when it is applied.
	CurrentSchemaVersion uint `json:"current_schema_version"`
	// TargetHasData: the current database holds user data (any table other than
	// secrets / runtime_settings has rows), so Confirm needs replaceExisting.
	TargetHasData bool `json:"target_has_data"`
}

// Pending is a confirmed import waiting for the next start.
type Pending struct {
	Manifest         migratebundle.Manifest `json:"manifest"`
	ConfirmedAt      string                 `json:"confirmed_at"`
	ReplacedExisting bool                   `json:"replaced_existing"`
}

// Service stages and confirms imports for the running application.
type Service struct {
	db     *sql.DB
	layout layout
	mu     sync.Mutex
	now    func() time.Time
}

// New returns a Service for the live database db, whose file is dbPath.
func New(db *sql.DB, dbPath string) *Service {
	return &Service{db: db, layout: layoutFor(dbPath), now: time.Now}
}

// Preview validates the uploaded bundle (migratebundle.Unpack: compatibility
// first, then integrity), stages its database and compares it with the
// current one. A previous staged-but-unconfirmed upload is discarded: one
// preview is in flight at a time. The live database and any pending import
// are not touched.
func (s *Service) Preview(ctx context.Context, upload io.Reader) (Preview, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.RemoveAll(s.layout.staging()); err != nil {
		return Preview{}, fmt.Errorf("importflow: clear staging: %w", err)
	}
	token, err := newToken()
	if err != nil {
		return Preview{}, err
	}
	dir := s.layout.stagingDir(token)
	if err := ensure(dir); err != nil {
		return Preview{}, err
	}
	p, err := s.stage(ctx, dir, token, upload)
	if err != nil {
		_ = os.RemoveAll(dir)
		return Preview{}, err
	}
	return p, nil
}

func (s *Service) stage(ctx context.Context, dir, token string, upload io.Reader) (Preview, error) {
	uploadPath := filepath.Join(dir, uploadFile)
	size, err := saveUpload(uploadPath, upload)
	if err != nil {
		return Preview{}, err
	}
	f, err := os.Open(uploadPath)
	if err != nil {
		return Preview{}, fmt.Errorf("importflow: reopen upload: %w", err)
	}
	m, err := migratebundle.Unpack(ctx, f, size, filepath.Join(dir, stagedDatabase))
	_ = f.Close()
	if rmErr := os.Remove(uploadPath); err == nil && rmErr != nil {
		err = fmt.Errorf("importflow: remove upload: %w", rmErr)
	}
	if err != nil {
		return Preview{}, err
	}
	current, err := migratebundle.CountTables(ctx, s.db)
	if err != nil {
		return Preview{}, err
	}
	latest, err := migratebundle.LatestSchemaVersion()
	if err != nil {
		return Preview{}, err
	}
	p := Preview{Token: token, Manifest: m, Tables: compare(current, m.Tables), CurrentSchemaVersion: latest, TargetHasData: hasUserData(current)}
	if err := writeJSON(filepath.Join(dir, previewFile), p); err != nil {
		return Preview{}, err
	}
	return p, nil
}

func saveUpload(path string, upload io.Reader) (int64, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, fileMode)
	if err != nil {
		return 0, fmt.Errorf("importflow: create upload file: %w", err)
	}
	n, err := io.Copy(f, upload)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return 0, fmt.Errorf("importflow: receive upload: %w", err)
	}
	return n, nil
}

// Confirm moves the previewed bundle to the pending slot, to be applied at the
// next start. It re-verifies the staged database, refuses with
// ErrReplaceNotConfirmed when the current database has user data and
// replaceExisting is false, and replaces an older pending import.
func (s *Service) Confirm(ctx context.Context, token string, replaceExisting bool) (Pending, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !tokenRe.MatchString(token) {
		return Pending{}, ErrUnknownToken
	}
	dir := s.layout.stagingDir(token)
	var p Preview
	if err := readJSON(filepath.Join(dir, previewFile), &p); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Pending{}, ErrUnknownToken
		}
		return Pending{}, err
	}
	current, err := migratebundle.CountTables(ctx, s.db)
	if err != nil {
		return Pending{}, err
	}
	hasData := hasUserData(current)
	if hasData && !replaceExisting {
		return Pending{}, ErrReplaceNotConfirmed
	}
	if err := migratebundle.VerifyDatabase(ctx, filepath.Join(dir, stagedDatabase), p.Manifest); err != nil {
		return Pending{}, err
	}
	pend := Pending{Manifest: p.Manifest, ConfirmedAt: s.now().UTC().Format(time.RFC3339), ReplacedExisting: hasData}
	if err := writeJSON(filepath.Join(dir, pendingFile), pend); err != nil {
		return Pending{}, err
	}
	if err := os.RemoveAll(s.layout.pending()); err != nil {
		return Pending{}, fmt.Errorf("importflow: clear the earlier pending import: %w", err)
	}
	if err := os.Rename(dir, s.layout.pending()); err != nil {
		return Pending{}, fmt.Errorf("importflow: stage the import for the next start: %w", err)
	}
	return pend, nil
}

// Pending returns the confirmed import waiting for the next start, or nil.
func (s *Service) Pending() (*Pending, error) {
	var p Pending
	switch err := readJSON(filepath.Join(s.layout.pending(), pendingFile), &p); {
	case errors.Is(err, os.ErrNotExist):
		return nil, nil
	case err != nil:
		return nil, err
	}
	return &p, nil
}

// Cancel discards the staged preview and the pending import.
func (s *Service) Cancel() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return errors.Join(os.RemoveAll(s.layout.staging()), os.RemoveAll(s.layout.pending()))
}

// LastResult returns what the last start-up apply did, or nil if none ran.
func (s *Service) LastResult() (*Result, error) {
	var r Result
	switch err := readJSON(s.layout.lastResult(), &r); {
	case errors.Is(err, os.ErrNotExist):
		return nil, nil
	case err != nil:
		return nil, err
	}
	return &r, nil
}

func newToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("importflow: token: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

func compare(current, bundle []migratebundle.TableCount) []TableComparison {
	cur := make(map[string]int64, len(current))
	for _, t := range current {
		cur[t.Name] = t.Rows
	}
	seen := make(map[string]bool, len(bundle))
	out := make([]TableComparison, 0, len(bundle))
	for _, t := range bundle {
		seen[t.Name] = true
		out = append(out, TableComparison{Name: t.Name, Current: cur[t.Name], Bundle: t.Rows})
	}
	for _, t := range current {
		if !seen[t.Name] {
			out = append(out, TableComparison{Name: t.Name, Current: t.Rows})
		}
	}
	return out
}

// hasUserData: any table other than the settings tables holds a row. This is
// deliberately conservative (seeded rows count): a false positive only asks
// the operator for the confirmation, a false negative would skip it.
func hasUserData(tables []migratebundle.TableCount) bool {
	for _, t := range tables {
		if t.Name != "secrets" && t.Name != "runtime_settings" && t.Rows > 0 {
			return true
		}
	}
	return false
}
