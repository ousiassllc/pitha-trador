package sqlitedb

import "testing"

func TestFileURI_EscapesPathMetacharacters(t *testing.T) {
	got := FileURI("/tmp/dir#1/a?b/c%41.db", "mode=rw")
	want := "file:/tmp/dir%231/a%3Fb/c%2541.db?mode=rw"
	if got != want {
		t.Errorf("FileURI = %q, want %q", got, want)
	}
}
