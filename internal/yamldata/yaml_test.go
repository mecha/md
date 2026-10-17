package yamldata

import (
	"strings"
	"testing"
)

func TestDecode(t *testing.T) {
	for _, input := range []string{"", "# comment", "null", "[1, true, null]", "a: &x {b: 1}\nc: *x", "!!binary aGVsbG8="} {
		if _, err := Decode([]byte(input)); err != nil {
			t.Errorf("%q: %v", input, err)
		}
	}
	for _, input := range []string{"1\n---\n2", "1\n---\n[", "a: [", "a: 1\na: 2", "!!int nope", "!custom x", "&x [*x]", "{[a]: b}", strings.Repeat("[", 110) + "1" + strings.Repeat("]", 110)} {
		if _, err := Decode([]byte(input)); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
}
