package migratebundle_test

import (
	"bytes"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/migratebundle"
	"github.com/ousiassllc/pitha-trador/internal/service/migratebundle/bundletest"
)

// attackerURL stands for a base URL a hostile bundle would plant to make the
// 立花 client send its credentials to another host.
const attackerURL = "https://attacker.invalid/"

var tachibanaBaseURLKeys = []string{config.KeyTachibanaDemoBaseURL, config.KeyTachibanaProdBaseURL}

// The 立花 API base URLs never leave the PC: not in the exported database and
// named in manifest.excluded_settings.
func TestExport_ExcludesTachibanaBaseURLs(t *testing.T) {
	conn, _ := bundletest.OpenDB(t)
	bundletest.Seed(t, conn)
	bundletest.Exec(t, conn, `INSERT INTO runtime_settings (key, value, updated_at) VALUES
		('`+config.KeyTachibanaDemoBaseURL+`', '"`+attackerURL+`"', 't'),
		('`+config.KeyTachibanaProdBaseURL+`', '"`+attackerURL+`"', 't')`)
	bundle, m := export(t, migratebundle.NewExporter(conn))
	for _, key := range tachibanaBaseURLKeys {
		if !slices.Contains(m.ExcludedSettings, key) {
			t.Errorf("manifest.excluded_settings lacks %q: %v", key, m.ExcludedSettings)
		}
	}
	dst, _, err := unpack(t, bundle)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	if raw, err := os.ReadFile(dst); err != nil || bytes.Contains(raw, []byte("attacker.invalid")) {
		t.Fatalf("the bundle's database carries the base URL (read err %v)", err)
	}
}

// A bundle that does carry a base URL (hand-edited, made by something else)
// is refused on import, like any other excluded key.
func TestUnpack_RefusesBundleCarryingTachibanaBaseURL(t *testing.T) {
	for _, key := range tachibanaBaseURLKeys {
		t.Run(key, func(t *testing.T) {
			conn, _ := bundletest.OpenDB(t)
			bundle, _ := export(t, migratebundle.NewExporter(conn))
			bundletest.Exec(t, conn, `INSERT INTO runtime_settings (key, value, updated_at) VALUES ('`+key+`', '"`+attackerURL+`"', 't')`)
			raw := unscrubbedCopy(t, conn)
			rows := bundletest.Counts(t, conn)["runtime_settings"]
			forged := rebuild(t, bundle, func(m *migratebundle.Manifest) {
				sum, size := sha(raw)
				m.DatabaseSHA256, m.DatabaseBytes = sum, size
				for i := range m.Tables {
					if m.Tables[i].Name == "runtime_settings" {
						m.Tables[i].Rows = rows
					}
				}
			}, func([]byte) []byte { return raw })
			if _, _, err := unpack(t, forged); !errors.Is(err, migratebundle.ErrNotSecretFree) {
				t.Fatalf("err = %v, want ErrNotSecretFree", err)
			}
		})
	}
}
