package sqlutil_test

import (
	"reflect"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

func TestInAndNotIn(t *testing.T) {
	cond, args := sqlutil.NotIn("d.v", []string{"a", "b"})
	if cond != "d.v NOT IN (?,?)" || !reflect.DeepEqual(args, []any{"a", "b"}) {
		t.Errorf("NotIn = %q %v", cond, args)
	}
	cond, args = sqlutil.In("d.v", []string{"a"})
	if cond != "d.v IN (?)" || !reflect.DeepEqual(args, []any{"a"}) {
		t.Errorf("In = %q %v", cond, args)
	}
	if cond, args = sqlutil.NotIn("d.v", nil); cond != "" || args != nil {
		t.Errorf("empty NotIn = %q %v, want nothing to exclude", cond, args)
	}
}
