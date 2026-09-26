package repository

import (
	"database/sql"
	"fmt"
	"time"
)

// timeLayout serializes time.Time into the RFC3339 TEXT columns declared
// throughout db/migrations (docs/architecture/er.md §型・規約「日時」:
// "UTCのRFC3339文字列として保存する"). Parsing uses the coarser
// time.RFC3339 layout, which Go accepts regardless of how many fractional
// second digits (if any) timeLayout produced.
const timeLayout = time.RFC3339Nano

// formatTime renders t as a UTC RFC3339 string for storage.
func formatTime(t time.Time) string {
	return t.UTC().Format(timeLayout)
}

// parseTime parses an RFC3339 TEXT column value back into a UTC time.Time.
func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("repository: parse timestamp %q: %w", s, err)
	}
	return t.UTC(), nil
}

// parseNullableTime parses a nullable TEXT column value, returning nil when
// the column was NULL.
func parseNullableTime(s sql.NullString) (*time.Time, error) {
	if !s.Valid {
		return nil, nil
	}
	t, err := parseTime(s.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// nullableString adapts a *string domain field to a database/sql argument,
// mapping nil to SQL NULL.
func nullableString(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

// nullableFloat64 adapts a *float64 domain field to a database/sql
// argument, mapping nil to SQL NULL.
func nullableFloat64(f *float64) any {
	if f == nil {
		return nil
	}
	return *f
}

// nullableInt64 adapts a *int64 domain field to a database/sql argument,
// mapping nil to SQL NULL.
func nullableInt64(i *int64) any {
	if i == nil {
		return nil
	}
	return *i
}

// nullableTime adapts a *time.Time domain field to a database/sql
// argument, mapping nil to SQL NULL and a non-nil value through
// formatTime.
func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return formatTime(*t)
}

// nullInt64Scanner adapts a *int64 destination field (passed as
// nullInt64(&field)) to database/sql's Scanner interface, mapping a NULL
// column value to nil instead of leaving/erroring on a zero value.
type nullInt64Scanner struct {
	dest **int64
}

// nullInt64 wraps dest so it can be passed directly to Row.Scan /
// Rows.Scan for a nullable INTEGER column (e.g. a nullable FK).
func nullInt64(dest **int64) *nullInt64Scanner {
	return &nullInt64Scanner{dest: dest}
}

func (n *nullInt64Scanner) Scan(src any) error {
	if src == nil {
		*n.dest = nil
		return nil
	}
	var ni sql.NullInt64
	if err := ni.Scan(src); err != nil {
		return err
	}
	v := ni.Int64
	*n.dest = &v
	return nil
}

// nullFloatScanner adapts a *float64 destination field (passed as
// nullFloat(&field)) to database/sql's Scanner interface, mapping a NULL
// column value to nil instead of leaving/erroring on a zero value.
type nullFloatScanner struct {
	dest **float64
}

// nullFloat wraps dest so it can be passed directly to Row.Scan /
// Rows.Scan for a nullable NUMERIC column.
func nullFloat(dest **float64) *nullFloatScanner {
	return &nullFloatScanner{dest: dest}
}

func (n *nullFloatScanner) Scan(src any) error {
	if src == nil {
		*n.dest = nil
		return nil
	}
	var nf sql.NullFloat64
	if err := nf.Scan(src); err != nil {
		return err
	}
	v := nf.Float64
	*n.dest = &v
	return nil
}
