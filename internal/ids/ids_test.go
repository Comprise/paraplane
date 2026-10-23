package ids

import (
	"strings"
	"testing"
)

func TestValid(t *testing.T) {
	cases := []struct {
		in string
		ok bool
	}{
		{"T1", true},
		{"W2.a", true},
		{"a" + strings.Repeat("b", 31), true},
		{"1x", false},
		{"", false},
		{"a" + strings.Repeat("b", 32), false},
		{"a b", false},
		{"ä", false},
	}
	for _, c := range cases {
		if got := Valid(c.in); got != c.ok {
			t.Errorf("Valid(%q) = %v, want %v", c.in, got, c.ok)
		}
	}
}

func TestParseList(t *testing.T) {
	out, ok := ParseList("T1,T2")
	if !ok || len(out) != 2 || out[0] != "T1" || out[1] != "T2" {
		t.Fatalf("ParseList(T1,T2) = %v, %v", out, ok)
	}
	for _, in := range []string{"T1,", ",T1", "T1,,T2", "", "T1,1x"} {
		if _, ok := ParseList(in); ok {
			t.Errorf("ParseList(%q) ok, want false", in)
		}
	}
}

func FuzzParseList(f *testing.F) {
	for _, s := range []string{"T1,T2", "", "a", ",", "T1,,T2"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		out, ok := ParseList(in)
		if ok && strings.Join(out, ",") != in {
			t.Fatalf("round-trip: %q -> %v", in, out)
		}
	})
}
