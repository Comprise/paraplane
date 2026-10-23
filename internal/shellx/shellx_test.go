package shellx

import (
	"slices"
	"strings"
	"testing"
)

func TestShellLexHeredoc(t *testing.T) {
	t.Parallel()
	for cmd, want := range map[string]string{
		"cat <<EOF >o\nbody x\nEOF\nls y":   "cat o ls y",
		"cat <<-EOF\n\tbody\n\tEOF\nls":     "cat ls",
		"cat << 'E O'\nbody\nE O\nls":       "cat ls",
		"cat <<EOF\nEOFX\nEOF":              "cat",
		"a <<A; b <<B\n1\nA\n2\nB\nc":       "a b c",
		"cat <<EOF":                         "cat",
		"cat <<<word":                       "cat word",
		"cat <<EOF\n  EOF\nstill body\nEOF": "cat", // <<: no blanks stripped
		"cat <<EOF\n\tEOF\nstill body\nEOF": "cat", // <<: no tabs stripped
		"cat <<EOF\n\tEOF\nEOF\nls x":       "cat ls x",
		"cat <<1>out\nbody\n1":              "cat out",
		"echo $((1<<2))\nls x":              "echo $ 1 2 ls x",
		"echo $[1<<2] y":                    "echo $[1 2] y",
	} {
		var got []string
		for _, w := range Words(cmd) {
			got = append(got, w.Text)
		}
		if g := strings.Join(got, " "); g != want {
			t.Errorf("%q: words %q, want %q", cmd, g, want)
		}
	}
}

func TestShellWordsCopyTarget(t *testing.T) {
	t.Parallel()
	kinds := func(cmd string) map[string]Kind {
		got := map[string]Kind{}
		for _, w := range Words(cmd) {
			got[w.Text] = w.Kind
		}
		return got
	}
	for _, cmd := range []string{"cp -t D a x/b", "mv -tD a x/b", "install --target-directory=D/ a x/b", "cp -vt D a x/b"} {
		k := kinds(cmd)
		if k["D/a"] != Write || k["D/b"] != Write || k["a"] != Read || k["x/b"] != Read {
			t.Errorf("%q: kinds %v; want D/a and D/b written, the sources read", cmd, k)
		}
	}
	if k := kinds("cp a b"); k["a"] != Read || k["b"] != Write {
		t.Errorf("cp a b: kinds %v", k)
	}
}

func TestSedPlain(t *testing.T) {
	t.Parallel()
	for script, want := range map[string]string{
		"s/a/b/":                   "b",
		"s|a|b/c|g":                "b/c",
		`s/a\/b/c\/d/`:             "c/d",
		"/x/s/a/b/;3s,c,d,":        "b d",
		"1,$ s/a/b/2\n/x/!s/c/d/I": "b d",
		`\#x#s/a/b/`:               "b",
		`s/a/\&/`:                  `\&`,
	} {
		got, ok := sedPlain(script)
		if !ok || strings.Join(got, " ") != want {
			t.Errorf("%q: got %q, %v; want %q", script, got, ok, want)
		}
	}
	for _, script := range []string{"p", "8d", "s/unterminated", "s/a/&/", `s/(a)/\1/`, "y/a/b/", "s/a/b/w f", "s/a/b/e", "{s/a/b/}", "/x/d", "s/a/b/;d", ""} {
		if script == "" {
			continue // an empty script is checked by markSed (no scripts)
		}
		if got, ok := sedPlain(script); ok {
			t.Errorf("%q: plain with %q", script, got)
		}
	}
}

func TestWordsSedEdit(t *testing.T) {
	t.Parallel()
	ws := Words("sed -i s/a/b/ f.md; sed -i -f x.sed g.md; sed s/a/b/ h.md")
	got := map[string]Word{}
	for _, w := range ws {
		got[w.Text] = w
	}
	if w := got["f.md"]; w.Kind != Edit || w.Old != "s/a/b/" || !slices.Equal(w.News, []string{"b"}) {
		t.Errorf("sed -i s/a/b/ f.md: %+v", w)
	}
	if got["g.md"].Kind != Write {
		t.Errorf("sed -i -f: %+v, want a write", got["g.md"])
	}
	if got["h.md"].Kind != Read {
		t.Errorf("sed without -i: %+v, want a read", got["h.md"])
	}
}

func TestCommandNames(t *testing.T) {
	t.Parallel()
	for cmd, want := range map[string][]string{
		"cd x && git commit -m m":       {"cd", "git"},
		"echo git commit":               {"echo"},
		"GIT_DIR=x git commit >o; (ls)": {"git", "ls"},
		">o git log | tee f":            {"git", "tee"},
		"cat <<EOF\ngit commit\nEOF\nx": {"cat", "x"},
		"":                              nil,
		"<in git commit":                {"git"},
		"2<f git commit":                {"git"},
		"git commit <msg.txt":           {"git"},
		"<<<x git log; 12<<<y tee f":    {"git", "tee"},
		"2 <f":                          {"2"},
		"cat <<1\n1\ngit":               {"cat", "git"},
	} {
		if got := CommandNames(cmd); !slices.Equal(got, want) {
			t.Errorf("%q: names %q, want %q", cmd, got, want)
		}
	}
}
