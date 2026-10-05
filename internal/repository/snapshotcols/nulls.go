package snapshotcols

import "database/sql"

// nullable maps a nil pointer to SQL NULL and otherwise dereferences it.
func nullable[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

// nullScanner scans a nullable column into a *T field: NULL becomes nil.
type nullScanner[T any] struct{ dest **T }

func (n nullScanner[T]) Scan(src any) error {
	if src == nil {
		*n.dest = nil
		return nil
	}
	var v T
	var err error
	switch p := any(&v).(type) {
	case *float64:
		var nf sql.NullFloat64
		err = nf.Scan(src)
		*p = nf.Float64
	case *int64:
		var ni sql.NullInt64
		err = ni.Scan(src)
		*p = ni.Int64
	case *bool:
		var nb sql.NullBool
		err = nb.Scan(src)
		*p = nb.Bool
	}
	if err != nil {
		return err
	}
	*n.dest = &v
	return nil
}

func nullFloat(dest **float64) sql.Scanner { return nullScanner[float64]{dest} }
func nullInt64(dest **int64) sql.Scanner   { return nullScanner[int64]{dest} }
func nullBool(dest **bool) sql.Scanner     { return nullScanner[bool]{dest} }
