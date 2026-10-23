package pathset

import (
	"reflect"
	"testing"
)

func TestMatchAny(t *testing.T) {
	ps := []string{"a/**", "*.out", "["}
	for name, want := range map[string]bool{"a/b/c": true, "x.out": true, "b/x.out": false, "b": false, "[": false} {
		if got := MatchAny(ps, name); got != want {
			t.Errorf("MatchAny(%q) = %v, want %v", name, got, want)
		}
	}
	if MatchAny(nil, "a") {
		t.Error("MatchAny(nil) = true")
	}
}

// TestClassSpans: the one class parser that checkClass and foldClass share
// reads members and ranges as written, inverted ones included.
func TestClassSpans(t *testing.T) {
	cases := []struct {
		p     string
		spans []span
		end   int
		ok    bool
	}{
		{"[abc]", []span{{'a', 'a'}, {'b', 'b'}, {'c', 'c'}}, 4, true},
		{"[a-z0]x", []span{{'a', 'z'}, {'0', '0'}}, 5, true},
		{"[z-a]", []span{{'z', 'a'}}, 4, true},
		{"[a-]", []span{{'a', 'a'}, {'-', '-'}}, 3, true},
		{"[-a]", []span{{'-', '-'}, {'a', 'a'}}, 3, true},
		{"[ac-a]", []span{{'a', 'a'}, {'c', 'a'}}, 5, true},
		{"[a-b-c]", []span{{'a', 'b'}, {'-', '-'}, {'c', 'c'}}, 6, true},
		{"[é-ë]", []span{{'é', 'ë'}}, 6, true},
		{"[ab", nil, 0, false},
	}
	for _, tc := range cases {
		spans, end, ok := classSpans(tc.p, 0)
		if ok != tc.ok || ok && (end != tc.end || !reflect.DeepEqual(spans, tc.spans)) {
			t.Errorf("classSpans(%q) = %q, %d, %v; want %q, %d, %v", tc.p, spans, end, ok, tc.spans, tc.end, tc.ok)
		}
	}
}
