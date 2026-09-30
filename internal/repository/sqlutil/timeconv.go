package sqlutil

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

// FormatTime renders t as a UTC RFC3339 string for storage.
func FormatTime(t time.Time) string {
	return t.UTC().Format(timeLayout)
}

// ParseTime parses an RFC3339 TEXT column value back into a UTC time.Time.
func ParseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("repository: parse timestamp %q: %w", s, err)
	}
	return t.UTC(), nil
}

// ParseNullableTime parses a nullable TEXT column value, returning nil when
// the column was NULL.
func ParseNullableTime(s sql.NullString) (*time.Time, error) {
	if !s.Valid {
		return nil, nil
	}
	t, err := ParseTime(s.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// NullableString adapts a *string domain field to a database/sql argument,
// mapping nil to SQL NULL.
func NullableString(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

// NullableFloat64 adapts a *float64 domain field to a database/sql
// argument, mapping nil to SQL NULL.
func NullableFloat64(f *float64) any {
	if f == nil {
		return nil
	}
	return *f
}

// NullableInt64 adapts a *int64 domain field to a database/sql argument,
// mapping nil to SQL NULL.
func NullableInt64(i *int64) any {
	if i == nil {
		return nil
	}
	return *i
}

// NullableTime adapts a *time.Time domain field to a database/sql
// argument, mapping nil to SQL NULL and a non-nil value through
// FormatTime.
func NullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return FormatTime(*t)
}

// NullableBool adapts a *bool domain field to a database/sql argument,
// mapping nil to SQL NULL.
func NullableBool(b *bool) any {
	if b == nil {
		return nil
	}
	return *b
}

// nullInt64Scanner adapts a *int64 destination field (passed as
// NullInt64(&field)) to database/sql's Scanner interface, mapping a NULL
// column value to nil instead of leaving/erroring on a zero value.
type nullInt64Scanner struct {
	dest **int64
}

// NullInt64 wraps dest so it can be passed directly to Row.Scan /
// Rows.Scan for a nullable INTEGER column (e.g. a nullable FK).
func NullInt64(dest **int64) *nullInt64Scanner {
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
// NullFloat(&field)) to database/sql's Scanner interface, mapping a NULL
// column value to nil instead of leaving/erroring on a zero value.
type nullFloatScanner struct {
	dest **float64
}

// NullFloat wraps dest so it can be passed directly to Row.Scan /
// Rows.Scan for a nullable NUMERIC column.
func NullFloat(dest **float64) *nullFloatScanner {
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

// nullBoolScanner adapts a *bool destination field (passed as
// NullBool(&field)) to database/sql's Scanner interface, mapping a NULL
// column value to nil instead of leaving/erroring on a zero value.
type nullBoolScanner struct {
	dest **bool
}

// NullBool wraps dest so it can be passed directly to Row.Scan /
// Rows.Scan for a nullable BOOLEAN column.
func NullBool(dest **bool) *nullBoolScanner {
	return &nullBoolScanner{dest: dest}
}

func (n *nullBoolScanner) Scan(src any) error {
	if src == nil {
		*n.dest = nil
		return nil
	}
	var nb sql.NullBool
	if err := nb.Scan(src); err != nil {
		return err
	}
	v := nb.Bool
	*n.dest = &v
	return nil
}
