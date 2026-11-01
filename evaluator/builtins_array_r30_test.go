package evaluator

import (
	"testing"

	"zuzo/object"
)

func TestR30ContainsHashKey(t *testing.T) {
	h := &object.Hash{Pairs: map[object.HashKey]object.HashPair{}}
	h.SetKey(&object.String{Value: "x"}, &object.Integer{Value: 1})

	for _, tc := range []struct {
		key  string
		want bool
	}{{"x", true}, {"missing", false}} {
		got := builtinContains([]object.Object{h, &object.String{Value: tc.key}}...)
		b, ok := got.(*object.Boolean)
		if !ok {
			t.Fatalf("expected boolean, got %T (%s)", got, got.Inspect())
		}
		if b.Value != tc.want {
			t.Errorf("包含(h, %q) = %v，期望 %v", tc.key, b.Value, tc.want)
		}
	}
}
