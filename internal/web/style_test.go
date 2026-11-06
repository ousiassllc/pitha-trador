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
	"strings"
	"testing"
)

// classAttrRe extracts literal `class="..."` attribute values from Templ
// source. Every internal/web/**/*.templ file spells its `class`
// attribute this way today (none use Templ's `class={ ... }` dynamic
// form) - a file that switched to the dynamic form would simply fall
// outside this test's coverage rather than fail it.
var classAttrRe = regexp.MustCompile(`class="([^"]+)"`)

// repoRoot walks up from the test's working directory (`go test` runs
// with the package directory as cwd) to the directory containing go.mod.
// It deliberately avoids runtime.Caller, whose file path is
// module-relative under `-trimpath`.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
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

// TestCompiledTextScaleMatchesTheGuide pins brand-guide §2's size scale and
// line heights in the compiled stylesheet (issue #953): Tailwind's own
// --text-xl/-3xl (20 / 30px) are off the guide's scale, and an @theme
// override that does not take effect would leave them in place unnoticed.
func TestCompiledTextScaleMatchesTheGuide(t *testing.T) {
	cssPath := filepath.Join(repoRoot(t), "static/src/dist/css/app.css")
	css, err := os.ReadFile(cssPath)
	if err != nil {
		t.Fatalf("reading compiled stylesheet %s (run `bun --cwd static run build` first): %v", cssPath, err)
	}
	for _, want := range []string{
		"--text-xs:.75rem", "--text-xs--line-height:1.6",
		"--text-sm:.875rem", "--text-sm--line-height:1.7",
		"--text-base:1rem", "--text-base--line-height:1.8",
		"--text-lg:1.125rem", "--text-lg--line-height:1.7",
		"--text-xl:1.5rem", "--text-xl--line-height:1.5",
		"--text-3xl:2.5rem", "--text-3xl--line-height:1.3",
	} {
		if !strings.Contains(string(css), want) {
			t.Errorf("compiled stylesheet lacks %q (text sizes and line heights follow brand-guide §2)", want)
		}
	}
}

// TestCompiledInlineAlertExitFadesOut pins the inline alert's exit (issue
// #955, brand-guide M5): an alert in a slot that htmx marks `htmx-swapping`
// animates out over --motion-fade-out with ease-in, and the tooltip's closed
// state uses the same exit timing, so its entrance and exit are not one
// transition in both directions.
func TestCompiledInlineAlertExitFadesOut(t *testing.T) {
	css, err := os.ReadFile(filepath.Join(repoRoot(t), "static/src/dist/css/app.css"))
	if err != nil {
		t.Fatalf("reading compiled stylesheet (run `bun --cwd static run build` first): %v", err)
	}
	for _, want := range []string{
		".htmx-swapping>[data-inline-alert]{animation:alert-out var(--motion-fade-out) var(--ease-in) both",
		"@keyframes alert-out",
	} {
		if !strings.Contains(string(css), want) {
			t.Errorf("compiled stylesheet lacks %q", want)
		}
	}
}

// classConcatRe finds a dynamic `class={ ... }` whose first operand is a string
// literal joined with `+`.
var classConcatRe = regexp.MustCompile(`class=\{[^}]*"\s*\+`)

// TestTemplClassExpressionsAreNotConcatenated is issue #957's regression test:
// `class={ "rounded-full text-xs" + tone.class() }` glues the last literal
// token to the first returned one ("text-xsborder"), so both classes silently
// vanish. A templ class expression takes several arguments, which templ joins
// with a space: `class={ "rounded-full text-xs", tone.class() }`.
func TestTemplClassExpressionsAreNotConcatenated(t *testing.T) {
	root := filepath.Join(repoRoot(t), "internal/web")
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
		for _, m := range classConcatRe.FindAllString(string(src), -1) {
			t.Errorf("%s: %q joins class names with `+`, which drops the separating space; pass them as separate templ arguments (issue #957)", rel, m)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
}

// TestCompiledDarkTokensFollowTheOS pins issue #957: the Dark values are
// reachable because they sit under `prefers-color-scheme: dark`, not behind an
// attribute nothing sets.
func TestCompiledDarkTokensFollowTheOS(t *testing.T) {
	css, err := os.ReadFile(filepath.Join(repoRoot(t), "static/src/dist/css/app.css"))
	if err != nil {
		t.Fatalf("reading compiled stylesheet (run `bun --cwd static run build` first): %v", err)
	}
	want := "@media (prefers-color-scheme:dark){:root{color-scheme:dark;--ink:#fff;"
	if !strings.Contains(string(css), want) {
		t.Errorf("compiled stylesheet lacks %q", want)
	}
}

// TestCompiledTableHeadingsAreSemiBold pins issue #963 (brand-guide §2/§4):
// Tailwind's preflight leaves the UA's `th { font-weight: bold }` in place, so
// without a base rule every <th> renders at 700 whatever its row inherits. Row
// headings (`scope="row"`) are body text and stay Regular.
func TestCompiledTableHeadingsAreSemiBold(t *testing.T) {
	css, err := os.ReadFile(filepath.Join(repoRoot(t), "static/src/dist/css/app.css"))
	if err != nil {
		t.Fatalf("reading compiled stylesheet (run `bun --cwd static run build` first): %v", err)
	}
	for _, want := range []string{
		"th{font-weight:600}",
		"th[scope=row]{font-weight:400}",
	} {
		if !strings.Contains(string(css), want) {
			t.Errorf("compiled stylesheet lacks %q", want)
		}
	}
}
