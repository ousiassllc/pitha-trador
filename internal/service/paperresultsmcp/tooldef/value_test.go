package tooldef

import (
	"encoding/json"
	"testing"
)

func TestValueText(t *testing.T) {
	for _, c := range []struct {
		raw  string
		want string
		ok   bool
	}{
		{`"abc"`, "abc", true}, {` "1" `, "1", true}, {`12`, "12", true}, {`-1.5`, "-1.5", true}, {`1e3`, "1000", true},
		{`true`, "", false}, {`null`, "", false}, {`[1]`, "", false}, {`{}`, "", false}, {``, "", false}, {`"`, "", false},
	} {
		got, ok := ValueText(json.RawMessage(c.raw))
		if got != c.want || ok != c.ok {
			t.Errorf("ValueText(%q) = %q, %v; want %q, %v", c.raw, got, ok, c.want, c.ok)
		}
	}
}
