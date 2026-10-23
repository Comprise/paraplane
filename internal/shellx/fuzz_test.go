package shellx

import (
	"slices"
	"strings"
	"testing"
)

// FuzzWords checks that Words and CommandNames never panic, that every word
// is made of bytes of the command (a slash may be added by cp -t DIR) and is
// no longer than it, that every kind is in range and that every command name
// is one of the words.
func FuzzWords(f *testing.F) {
	for _, s := range []string{
		"cat <<EOF >o\nbody x\nEOF\nls y",
		"a <<A; b <<B\n1\nA\n2\nB\nc",
		"echo $((1<<2))\nls x",
		"echo $[1<<2] y",
		"for ((i=0; i<3; i++)); do echo >f; done",
		"cp -vt D a x/b",
		"install --target-directory=D/ a x/b",
		"sed -i'' -e 's/a/b/' -- f.md",
		"sed -i \"\" s/a/b/ f",
		"tee -a f 2>&1 >&2 &>g >|h",
		"echo 'unterminated",
		`echo "a\"b" c\ d # comment` + "\n`ls`#x",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, cmd string) {
		ws := Words(cmd)
		texts := make([]string, 0, len(ws))
		for _, w := range ws {
			if w.Kind < Read || w.Kind > Write {
				t.Fatalf("%q: kind %d out of range", cmd, w.Kind)
			}
			if len(w.Text) > len(cmd) {
				t.Fatalf("%q: word %q longer than the command", cmd, w.Text)
			}
			for i := 0; i < len(w.Text); i++ {
				if c := w.Text[i]; c != '/' && strings.IndexByte(cmd, c) < 0 {
					t.Fatalf("%q: word %q has byte %q not in the command", cmd, w.Text, c)
				}
			}
			texts = append(texts, w.Text)
		}
		for _, n := range CommandNames(cmd) {
			if !slices.Contains(texts, n) {
				t.Fatalf("%q: command name %q is not a word", cmd, n)
			}
		}
	})
}
