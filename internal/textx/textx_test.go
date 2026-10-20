package textx

import "testing"

func TestLine(t *testing.T) {
	cases := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"newline and escape", "a\nb\x1b[31m", 100, "a b[31m"},
		{"bidi override", "‮abc", 100, "abc"},
		{"cr and tab", "a\r\nb\tc", 100, "a  b c"},
		{"line and paragraph separators", "a\u2028b\u2029c", 100, "a b c"},
		{"truncate runes", "абвгд", 3, "абв…"},
		{"exact fit", "абв", 3, "абв"},
		{"empty", "", 5, ""},
	}
	for _, c := range cases {
		if got := Line(c.in, c.max); got != c.want {
			t.Errorf("%s: Line(%q, %d) = %q, want %q", c.name, c.in, c.max, got, c.want)
		}
	}
}

func TestBlock(t *testing.T) {
	cases := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"keeps newline", "a\nb", 100, "a\nb"},
		{"strips escape and bidi", "a\x1b[1m\n‮b", 100, "a[1m\nb"},
		{"tab and cr to space", "a\tb\rc", 100, "a b c"},
		{"line and paragraph separators to space", "a\u2028b\n\u2029c", 100, "a b\n c"},
		{"cut on rune boundary", "абв", 3, "а…"},
		{"cut ascii", "abcdef", 3, "abc…"},
		{"exact fit", "абв", 6, "абв"},
	}
	for _, c := range cases {
		if got := Block(c.in, c.max); got != c.want {
			t.Errorf("%s: Block(%q, %d) = %q, want %q", c.name, c.in, c.max, got, c.want)
		}
	}
}
