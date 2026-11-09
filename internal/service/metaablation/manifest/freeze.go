package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrNotFrozen is returned when a manifest that was never frozen is used as the
// pre-registration of a run.
var ErrNotFrozen = errors.New("manifest: not frozen")

// ErrTampered is returned when a frozen manifest no longer matches its hash.
var ErrTampered = errors.New("manifest: content changed after freeze")

// ErrModelMismatch is returned when the model in use is not the frozen one.
var ErrModelMismatch = errors.New("manifest: model differs from the frozen model")

// ParamsHash is the SHA-256 (hex) of a stored params_json, the only trace of
// the parameters the manifest keeps.
func ParamsHash(paramsJSON string) string {
	sum := sha256.Sum256([]byte(paramsJSON))
	return hex.EncodeToString(sum[:])
}

// Hash is the SHA-256 (hex) of the manifest content without its Frozen field.
func (m Manifest) Hash() (string, error) {
	m.Frozen = nil
	b, err := json.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("manifest: encode for hashing: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// Freeze validates m and records now and the content hash. A frozen manifest
// is never frozen again: change it with NewRun.
func (m *Manifest) Freeze(now time.Time) error {
	if m.Frozen != nil {
		return errors.New("manifest: already frozen; a changed value is a new run (NewRun)")
	}
	if err := m.Validate(); err != nil {
		return err
	}
	if now.Before(m.CreatedAt) {
		return fmt.Errorf("manifest: freeze time %s precedes created_at %s", now.Format(time.RFC3339), m.CreatedAt.Format(time.RFC3339))
	}
	h, err := m.Hash()
	if err != nil {
		return err
	}
	m.Frozen = &Freeze{At: now, SHA256: h}
	return nil
}

// Verify checks that m is valid, frozen and unchanged since.
func (m Manifest) Verify() error {
	if m.Frozen == nil {
		return ErrNotFrozen
	}
	if err := m.Validate(); err != nil {
		return err
	}
	h, err := m.Hash()
	if err != nil {
		return err
	}
	if h != m.Frozen.SHA256 {
		return ErrTampered
	}
	return nil
}

// CheckModel verifies m and that version and the SHA-256 of the stored
// params_json are the frozen model's, so a different model cannot be used
// after the freeze.
func (m Manifest) CheckModel(version, paramsSHA256 string) error {
	if err := m.Verify(); err != nil {
		return err
	}
	if m.Model.Version != version || m.Model.ParamsSHA256 != paramsSHA256 {
		return fmt.Errorf("%w: frozen %q (%.12s), got %q (%.12s)", ErrModelMismatch, m.Model.Version, m.Model.ParamsSHA256, version, paramsSHA256)
	}
	return nil
}

// NewRun derives the manifest of a new run from the frozen prev: change edits
// the copy, the run count is incremented and prev stays listed with reason
// (why the run was repeated). The result is not frozen. Thresholds are never
// changed in place; the earlier run keeps its manifest and results.
func NewRun(prev Manifest, reason string, createdAt time.Time, change func(*Manifest)) (Manifest, error) {
	if err := prev.Verify(); err != nil {
		return Manifest{}, fmt.Errorf("manifest: new run from a manifest that is not a valid frozen one: %w", err)
	}
	if reason == "" {
		return Manifest{}, errors.New("manifest: a new run needs the reason it was repeated")
	}
	b, err := json.Marshal(prev)
	if err != nil {
		return Manifest{}, fmt.Errorf("manifest: copy: %w", err)
	}
	var next Manifest
	if err := json.Unmarshal(b, &next); err != nil {
		return Manifest{}, fmt.Errorf("manifest: copy: %w", err)
	}
	if change != nil {
		change(&next)
	}
	next.CreatedAt = createdAt
	next.Frozen = nil
	next.Run.Count = prev.Run.Count + 1
	next.Run.Previous = append(append([]PreviousRun(nil), prev.Run.Previous...),
		PreviousRun{Count: prev.Run.Count, SHA256: prev.Frozen.SHA256, FrozenAt: prev.Frozen.At, ChangeReason: reason})
	return next, nil
}
