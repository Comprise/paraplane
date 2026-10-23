package textx

import "testing"

func TestLine(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"newline and escape", "a\nb\x1b[31m", 100, "a b[31m"},
		{"bidi override", "\u202eabc", 100, "abc"},
		{"cr and tab", "a\r\nb\tc", 100, "a  b c"},
		{"line and paragraph separators", "a\u2028b\u2029c", 100, "a b c"},
		{"truncate runes", "абвгд", 3, "абв…"},
		{"exact fit", "абв", 3, "абв"},
		{"empty", "", 5, ""},
		{"max zero", "abc", 0, "…"},
		{"max zero empty", "", 0, ""},
		{"negative max", "abc", -3, "…"},
		{"c1 controls dropped", "a\u0085b\u009bc\u009f", 100, "abc"},
		{"suffix not counted in max", "abcdef", 3, "abc…"},
	}
	for _, c := range cases {
		if got := Line(c.in, c.max); got != c.want {
			t.Errorf("%s: Line(%q, %d) = %q, want %q", c.name, c.in, c.max, got, c.want)
		}
	}
}

func TestBlock(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"keeps newline", "a\nb", 100, "a\nb"},
		{"strips escape and bidi", "a\x1b[1m\n\u202eb", 100, "a[1m\nb"},
		{"tab and cr to space", "a\tb\rc", 100, "a b c"},
		{"line and paragraph separators to space", "a\u2028b\n\u2029c", 100, "a b\n c"},
		{"cut on rune boundary", "абв", 3, "а…"},
		{"cut ascii", "abcdef", 3, "abc…"},
		{"exact fit", "абв", 6, "абв"},
		{"max zero", "abc", 0, "…"},
		{"negative max", "abc", -1, "…"},
		{"max zero empty", "", 0, ""},
		{"c1 controls dropped", "a\u0085b\u009b\nc", 100, "ab\nc"},
		{"newline at the cut point", "ab\ncd", 2, "ab…"},
		{"newline just inside the cut", "ab\ncd", 3, "ab\n…"},
	}
	for _, c := range cases {
		if got := Block(c.in, c.max); got != c.want {
			t.Errorf("%s: Block(%q, %d) = %q, want %q", c.name, c.in, c.max, got, c.want)
		}
	}
}

func TestHasControl(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"plain", "go test ./...", false},
		{"cyrillic", "проверка", false},
		{"empty", "", false},
		{"space", "a b", false},
		{"tab", "a\tb", true},
		{"newline", "a\nb", true},
		{"nul", "a\x00b", true},
		{"del", "a\x7fb", true},
		{"escape", "\x1b[31m", true},
		{"line separator", "a\u2028b", true},
		{"paragraph separator", "a\u2029b", true},
		{"bidi override", "a\u202eb", true},
		{"zero width space", "a\u200bb", true},
		{"bom", "\uFEFFa", true},
	}
	for _, c := range cases {
		if got := HasControl(c.in); got != c.want {
			t.Errorf("%s: HasControl(%q) = %v, want %v", c.name, c.in, got, c.want)
		}
	}
}
