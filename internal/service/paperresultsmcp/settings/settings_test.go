package settings

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func write(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

// No file, or an explicit default file, means disabled with the default
// address and no key.
func TestLoadDefaultsAreOff(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), FileName))
	if err != nil {
		t.Fatal(err)
	}
	if s.Enabled || s.APIKey != "" || s.Listen != "127.0.0.1:48110" || s.VerifyAccessHeaders {
		t.Fatalf("missing file must load as off: %+v", s)
	}

	s, err = Load(write(t, `{"enabled": true, "api_key": "k"}`, 0o600))
	if err != nil {
		t.Fatal(err)
	}
	if !s.Enabled || s.APIKey != "k" || s.Listen != DefaultListen {
		t.Fatalf("omitted listen must default: %+v", s)
	}
}

// Unreadable shapes load as the disabled default plus an error, and the
// error never quotes the file (it holds the key).
func TestLoadMalformedIsOffAndSilentAboutContent(t *testing.T) {
	cases := map[string]string{
		"not json":       `enabled: true api_key: SECRET-IN-FILE`,
		"unknown field":  `{"enabled": true, "api_key": "SECRET-IN-FILE", "extra": 1}`,
		"wrong type":     `{"enabled": "yes", "api_key": "SECRET-IN-FILE"}`,
		"trailing value": `{"api_key": "SECRET-IN-FILE"} {}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			s, err := Load(write(t, content, 0o600))
			if err == nil {
				t.Fatal("want an error")
			}
			if s.Enabled || s.APIKey != "" {
				t.Fatalf("malformed file must yield the off default: %+v", s)
			}
			if strings.Contains(err.Error(), "SECRET-IN-FILE") {
				t.Fatalf("error quotes the file: %v", err)
			}
		})
	}
}

// The file holds the API key: group/other-readable is refused on Unix.
func TestLoadRefusesWorldReadableFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix mode bits")
	}
	s, err := Load(write(t, `{"enabled": true, "api_key": "k"}`, 0o644))
	if err != ErrInsecurePermissions || s.Enabled {
		t.Fatalf("got (%+v, %v), want off + ErrInsecurePermissions", s, err)
	}
}

func TestDefaultPathIsBesideTheDatabase(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/cfg")
	t.Setenv("AppData", `C:\cfg`)
	path, err := DefaultPath()
	if err != nil {
		t.Skipf("no user config dir: %v", err)
	}
	if filepath.Base(path) != FileName || filepath.Base(filepath.Dir(path)) != "pitha-trador" {
		t.Fatalf("path = %q", path)
	}
}
