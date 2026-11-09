package report

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/manifest"
)

// ErrForbiddenContent is returned when the rendered output mentions something
// an aggregate report must never carry.
var ErrForbiddenContent = errors.New("report: output would contain forbidden content")

// forbidden are markers of content that must not reach the output: raw stored
// payload columns, the model parameter body and credentials.
var forbidden = []string{
	"state_json", "response_json", "params_json", "features_json",
	"api_key", "apikey", "secret", "password", "bearer ", "authorization", "access_token", "refresh_token",
}

// CheckContent returns ErrForbiddenContent when s mentions a forbidden marker.
func CheckContent(s string) error {
	low := strings.ToLower(s)
	for _, f := range forbidden {
		if strings.Contains(low, f) {
			return fmt.Errorf("%w: %q", ErrForbiddenContent, strings.TrimSpace(f))
		}
	}
	return nil
}

// JSON renders the document.
func JSON(d Document) ([]byte, error) {
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("report: encode: %w", err)
	}
	if err := CheckContent(string(b)); err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Files are the paths WriteFiles created.
type Files struct {
	JSON, Markdown, Manifest string
}

// WriteFiles writes the JSON report, the Markdown report and the manifest into
// dir, which is created when missing. The names carry the run count and an
// existing file is never overwritten: an earlier run's output stays. dir must
// not be, or lie inside, any of forbiddenDirs (the directory of the database
// copy), so the output never lands next to the data it was computed from.
func WriteFiles(dir string, d Document, forbiddenDirs ...string) (Files, error) {
	if dir == "" {
		return Files{}, errors.New("report: an output directory is required")
	}
	abs, err := ResolveExisting(dir)
	if err != nil {
		return Files{}, fmt.Errorf("report: %w", err)
	}
	for _, f := range forbiddenDirs {
		fa, err := ResolveExisting(f)
		if err != nil {
			return Files{}, fmt.Errorf("report: %w", err)
		}
		if rel, err := filepath.Rel(fa, abs); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return Files{}, fmt.Errorf("report: output directory %s is inside %s", abs, fa)
		}
	}
	js, err := JSON(d)
	if err != nil {
		return Files{}, err
	}
	md := Markdown(d)
	if err := CheckContent(md); err != nil {
		return Files{}, err
	}
	mf, err := manifest.Marshal(d.Manifest)
	if err != nil {
		return Files{}, err
	}
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return Files{}, fmt.Errorf("report: create %s: %w", abs, err)
	}
	stem := fmt.Sprintf("metaablation-run%d", d.RunCount)
	files := Files{JSON: filepath.Join(abs, stem+".json"), Markdown: filepath.Join(abs, stem+".md"), Manifest: filepath.Join(abs, stem+".manifest.json")}
	outputs := []struct {
		path string
		data []byte
	}{{files.JSON, js}, {files.Markdown, []byte(md)}, {files.Manifest, mf}}
	// All three must be free before any is written: no partial set.
	for _, w := range outputs {
		if _, err := os.Lstat(w.path); err == nil {
			return Files{}, fmt.Errorf("report: %s already exists; an earlier run's output is never overwritten", w.path)
		}
	}
	for _, w := range outputs {
		if err := writeNew(w.path, w.data); err != nil {
			return Files{}, err
		}
	}
	return files, nil
}

// ResolveExisting is the absolute path of p with symlinks resolved in its deepest
// existing ancestor (p itself need not exist yet), so a symlink into a forbidden
// directory is seen for what it is.
func ResolveExisting(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	rest := ""
	for cur := abs; ; {
		if resolved, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(resolved, rest), nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return abs, nil
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

func writeNew(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("report: create %s: %w", path, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("report: write %s: %w", path, err)
	}
	return f.Close()
}
