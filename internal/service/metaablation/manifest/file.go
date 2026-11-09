package manifest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

// Marshal is the manifest file content (indented JSON, one trailing newline).
func Marshal(m Manifest) ([]byte, error) {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("manifest: encode: %w", err)
	}
	return append(b, '\n'), nil
}

// Unmarshal parses a manifest file, rejecting unknown fields.
func Unmarshal(b []byte) (Manifest, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("manifest: decode: %w", err)
	}
	return m, nil
}

// WriteNew writes m to path and fails when the file exists: a frozen manifest
// is never overwritten.
func WriteNew(path string, m Manifest) error {
	b, err := Marshal(m)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("manifest: create %s: %w", path, err)
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return fmt.Errorf("manifest: write %s: %w", path, err)
	}
	return f.Close()
}

// ReadFrozen reads path and verifies that it is a valid frozen manifest.
func ReadFrozen(path string) (Manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("manifest: read %s: %w", path, err)
	}
	m, err := Unmarshal(b)
	if err != nil {
		return Manifest{}, err
	}
	if err := m.Verify(); err != nil {
		return Manifest{}, err
	}
	return m, nil
}
