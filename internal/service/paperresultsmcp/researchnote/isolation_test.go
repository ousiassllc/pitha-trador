package researchnote_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Research notes are a record only (issue #1095): Policy, Risk, order placement,
// execution, the brokers, the Paper guard, the Live path, the Jev prompts / RAG /
// meta-model / self-improvement flow, the Feature Engine and the settings code
// must not read them. Two checks:
//
//  1. no non-test source file outside the few readers / writers below names the
//     tables or imports the packages;
//  2. the note packages themselves import nothing from the application but
//     the SQL helpers and the masking package (so they cannot call into
//     Policy, Risk, orders, brokers, Jev, the Feature Engine or settings).
func TestResearchNotesAreReadAndWrittenOnlyByTheirTools(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..")
	record := []string{
		"research_notes", "research_note_versions", "paperresultsmcp/researchnote",
	}
	allowed := []string{
		"internal/service/paperresultsmcp/researchnote/",            // the record's own code
		"internal/service/paperresultsmcp/mcpset/research_notes.go", // the MCP writer's two transactions and the screen's list
		"internal/service/paperresultsmcp/research_note_tools.go",   // registers the notetool specs in the dispatch table
		"internal/service/paperresultsmcp/errors.go",                // the tools' error-code texts (they name the list tool)
		"internal/web/handler/mcpsettings/research_notes",           // the Settings screen's read-only list
		"internal/web/organisms/mcp/research_notes",                 // its view
		"db/migrations/", // the schema
	}
	walkGo(t, root, func(rel string, src []byte) {
		for _, sym := range record {
			if !strings.Contains(string(src), sym) {
				continue
			}
			ok := false
			for _, prefix := range allowed {
				ok = ok || strings.HasPrefix(rel, prefix)
			}
			if !ok {
				t.Errorf("%s references the research notes (%q): only their MCP tools and the read-only Settings list may, never Policy, Risk, orders, brokers, the Paper guard, Live, Jev, the Feature Engine or settings", rel, sym)
			}
		}
	})
}

func TestResearchNotePackagesImportOnlyTheSQLHelpersAndTheMask(t *testing.T) {
	const module = "github.com/ousiassllc/pitha-trador/"
	ok := map[string]bool{
		module + "internal/repository/sqlutil":                        true,
		module + "internal/service/paperresultsmcp/query":             true,
		module + "internal/service/paperresultsmcp/researchnote/mask": true,
	}
	for _, dir := range []string{".", "mask"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, e.Name()), nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range f.Imports {
				path, _ := strconv.Unquote(imp.Path.Value)
				if strings.HasPrefix(path, module) && !ok[path] {
					t.Errorf("%s/%s imports %s: the note packages may import only the SQL helpers and the mask", dir, e.Name(), path)
				}
			}
		}
	}
}

// The note packages write only their own two tables: every INSERT / UPDATE /
// DELETE / DDL statement in their source names research_notes or
// research_note_versions, and no statement touches a version row except INSERT.
func TestResearchNotePackagesWriteOnlyTheirOwnTables(t *testing.T) {
	for _, dir := range []string{".", "mask", filepath.Join("..", "mcpset")} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") ||
				(strings.Contains(dir, "mcpset") && name != "research_notes.go") {
				continue
			}
			src, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			text := string(src)
			for _, verb := range []string{"INSERT OR IGNORE INTO ", "INSERT OR REPLACE INTO ", "INSERT INTO ", "UPDATE ", "DELETE FROM ", "DROP ", "ALTER TABLE ", "CREATE "} {
				for rest := text; ; {
					i := strings.Index(rest, "`"+verb)
					j := strings.Index(rest, "\""+verb)
					if i < 0 || (j >= 0 && j < i) {
						i = j
					}
					if i < 0 {
						break
					}
					stmt := rest[i+1+len(verb):]
					switch {
					case strings.HasPrefix(stmt, "research_note_versions"):
						if verb != "INSERT INTO " {
							t.Errorf("%s/%s: %q on a version row (versions are append-only)", dir, name, verb)
						}
					case strings.HasPrefix(stmt, "research_notes"):
					default:
						t.Errorf("%s/%s writes with %q to a table that is not a research-note table: %.60s", dir, name, verb, stmt)
					}
					rest = stmt
				}
			}
		}
	}
}

func walkGo(t *testing.T, root string, fn func(rel string, src []byte)) {
	t.Helper()
	for _, dir := range []string{"internal", "cmd"} {
		abs := filepath.Join(root, dir)
		if _, err := os.Stat(abs); err != nil {
			continue
		}
		err := filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			fn(filepath.ToSlash(rel), src)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	// The migrations are SQL, not Go: nothing to scan there; they are allow-listed.
}
