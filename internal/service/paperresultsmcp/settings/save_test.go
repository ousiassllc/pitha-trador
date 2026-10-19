package settings

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSaveRoundTripsThroughLoadAndKeepsAccessFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", FileName) // parent does not exist yet
	want := Settings{
		Enabled: true, Listen: "[::1]:48111", APIKey: "pitha-mcp-k",
		VerifyAccessHeaders: true, AccessClientID: "id", AccessClientSecret: "secret",
	}
	if err := Save(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load rejects what Save wrote: %v", err)
	}
	if got != want {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
	if err := ValidateListen(got.Listen, nil); err != nil {
		t.Fatalf("saved listen no longer valid: %v", err)
	}
}

func TestSaveWritesOwnerOnlyFileAndDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix mode bits")
	}
	dir := filepath.Join(t.TempDir(), "cfg")
	path := filepath.Join(dir, FileName)
	if err := Save(path, Default()); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %v (err %v), want 0600", info.Mode().Perm(), err)
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %v (err %v), want 0700", info.Mode().Perm(), err)
	}
	// Replacing an existing wider file leaves a 0600 one (Load refuses wider).
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, Default()); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("Load after re-Save over a 0644 file: %v", err)
	}
}

// A failed Save leaves the old file intact and no temp file behind; a
// successful one leaves no temp file either (the rename is the only
// publication step).
func TestSaveIsAtomicAndLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := Save(path, Settings{Listen: DefaultListen, APIKey: "old"}); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, Settings{Listen: DefaultListen, APIKey: "new"}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != FileName {
		t.Fatalf("directory = %v, want only %s", entries, FileName)
	}

	// A directory squatting on the target makes the final rename fail.
	blocked := filepath.Join(t.TempDir(), FileName)
	if err := os.MkdirAll(filepath.Join(blocked, "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	err = Save(blocked, Settings{Listen: DefaultListen, APIKey: "SECRET-KEY"})
	if err == nil {
		t.Fatal("Save over a directory must fail")
	}
	if strings.Contains(err.Error(), "SECRET-KEY") {
		t.Fatalf("error leaks the key: %v", err)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(blocked), "*.tmp")); len(left) != 0 {
		t.Fatalf("temp files left behind: %v", left)
	}
}

func TestGenerateAPIKey(t *testing.T) {
	seen := map[string]bool{}
	for range 50 {
		key, err := GenerateAPIKey()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(key, KeyPrefix) {
			t.Fatalf("key %q lacks prefix %q", key, KeyPrefix)
		}
		// 32 random bytes = 43 unpadded base64url characters.
		body := strings.TrimPrefix(key, KeyPrefix)
		if len(body) != 43 || strings.Trim(body, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_") != "" {
			t.Fatalf("key body %q is not 43 URL-safe characters", body)
		}
		if seen[key] {
			t.Fatalf("duplicate key %q", key)
		}
		seen[key] = true
	}
}
