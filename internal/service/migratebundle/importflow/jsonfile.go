package importflow

import (
	"encoding/json"
	"fmt"
	"os"
)

// writeJSON writes v to path atomically (temporary file + rename), 0600.
func writeJSON(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("importflow: encode %q: %w", path, err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, fileMode); err != nil {
		return fmt.Errorf("importflow: write %q: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("importflow: write %q: %w", path, err)
	}
	return nil
}

// readJSON reads path into v; a missing file is reported as os.ErrNotExist
// (errors.Is) so callers can treat it as "nothing there".
func readJSON(path string, v any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("importflow: parse %q: %w", path, err)
	}
	return nil
}
