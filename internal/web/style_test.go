// Package web_test guards against issue #74's regression: every
// internal/web/**/*.templ file that sets a literal `class="..."` MUST
// name only real Tailwind utility classes (docs/components/overview.md
// §1 "スタイリング | Tailwind CSS | ユーティリティファーストCSS"), never a
// hand-rolled BEM-style hook (e.g. the old `header-container`, `badge`,
// `badge-long`, `status-dot` classes) that Tailwind's scanner never
// generates a rule for - so it silently renders unstyled. `make build`
// (`static/package.json`'s `build` script) runs before `go build`/`go
// test` compiles static/src/embed.go's `go:embed dist`, so
// static/src/dist/css/app.css is always the current compiled stylesheet
// by the time this test runs.
package web_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// classAttrRe extracts literal `class="..."` attribute values from Templ
// source. Every internal/web/**/*.templ file spells its `class`
// attribute this way today (none use Templ's `class={ ... }` dynamic
// form) - a file that switched to the dynamic form would simply fall
// outside this test's coverage rather than fail it.
var classAttrRe = regexp.MustCompile(`class="([^"]+)"`)

// repoRoot walks up from this test file's own path to the directory
// containing go.mod, so the test works regardless of the working
// directory `go test` is invoked from.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (go.mod) above internal/web")
		}
		dir = parent
	}
}

// templClassTokens returns every whitespace-separated class token used
// across every `class="..."` attribute in root's *.templ files.
func templClassTokens(t *testing.T, root string) map[string][]string {
	t.Helper()
	tokens := make(map[string][]string) // token -> files it appears in
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".templ") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		for _, m := range classAttrRe.FindAllStringSubmatch(string(src), 1<<30) {
			for _, tok := range strings.Fields(m[1]) {
				tokens[tok] = append(tokens[tok], rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	return tokens
}

// cssEscape mirrors Tailwind's own selector escaping (verified against
// tailwindcss v4.3.3's actual output): every character outside
// [A-Za-z0-9_-] is emitted as a literal backslash-escape, e.g.
// `hover:text-slate-900` -> `hover\:text-slate-900`,
// `py-0.5` -> `py-0\.5`, `w-1/2` -> `w-1\/2`.
func cssEscape(token string) string {
	var b strings.Builder
	for _, r := range token {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('\\')
			b.WriteRune(r)
		}
	}
	return b.String()
}

// TestTemplClassesAreCompiledTailwindUtilities is issue #74's regression
// test: it fails whenever a .templ file's `class` attribute names a
// token Tailwind's build never produced a rule for (a custom BEM-style
// hook like the old `header-container`/`badge-long`/`status-dot`, or a
// typo'd utility), which is exactly how every page rendered unstyled.
func TestTemplClassesAreCompiledTailwindUtilities(t *testing.T) {
	root := repoRoot(t)
	tokens := templClassTokens(t, filepath.Join(root, "internal/web"))
	if len(tokens) == 0 {
		t.Fatal("found no class=\"...\" attributes under internal/web - test fixture broke")
	}

	cssPath := filepath.Join(root, "static/src/dist/css/app.css")
	css, err := os.ReadFile(cssPath)
	if err != nil {
		t.Fatalf("reading compiled stylesheet %s (run `bun --cwd static run build` first, as `make build`/CI do before `go test`): %v", cssPath, err)
	}
	cssStr := string(css)

	for token, files := range tokens {
		escaped := cssEscape(token)
		pattern := regexp.MustCompile(`\.` + regexp.QuoteMeta(escaped) + `(?:[^A-Za-z0-9_\\-]|$)`)
		if !pattern.MatchString(cssStr) {
			t.Errorf("class %q (used in %s) has no compiled Tailwind rule in %s - it is not a real Tailwind utility class, so it renders unstyled (issue #74)", token, strings.Join(files, ", "), cssPath)
		}
	}
}
