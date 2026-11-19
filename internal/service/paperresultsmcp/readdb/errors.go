package readdb

import (
	"errors"
	"strings"

	"modernc.org/sqlite"
)

// SQLite primary result codes (sqlite3.h).
const (
	sqliteBusy   = 5
	sqliteLocked = 6
)

// IsBusy reports a read that failed because the file was locked by a writer.
func IsBusy(err error) bool {
	if strings.Contains(err.Error(), "database is locked") {
		return true
	}
	var se *sqlite.Error
	if errors.As(err, &se) {
		primary := se.Code() & 0xff
		return primary == sqliteBusy || primary == sqliteLocked
	}
	return false
}

// IsMissingSchema reports a table or column the tool needs that the file
// lacks (an older schema).
func IsMissingSchema(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "no such table") || strings.Contains(msg, "no such column")
}
