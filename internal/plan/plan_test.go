package plan

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

const specBlock = `version: 1                  # необязательно; поддерживается только 1
gate: "go test ./..."       # необязательно; gate по умолчанию для волн плана
waves:
  - id: W1
    gate: "go vet ./... && go test ./..."   # необязательно; перекрывает gate плана
    tasks:
      - id: T1
        title: "Пакет config"
        owns: ["internal/config/**"]
        reads: []
        depends: []
        verify: "go test ./internal/config/"
      - id: T2
        title: "Пакет plan"
        owns: ["internal/plan/**"]
  - id: W2
    tasks:
      - id: T3
        title: "Валидатор"
        owns: ["internal/rules/**"]
        reads: [internal/plan/plan.go]
        depends: [T2]
`

func TestExtract(t *testing.T) {
	t.Parallel()
	body := "waves: []\n"
	cases := []struct {
		name, open, close string
	}{
		{"backticks", "```paraplane", "```"},
		{"tildes", "~~~paraplane", "~~~"},
		{"four backticks", "````paraplane", "````"},
		{"info words and indent", "  ``` paraplane extra", "  `````"},
		{"tildes longer close", "~~~ paraplane", "~~~~~~  "},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := "# Plan\n\nText with `code`.\n\n```go\nfunc x() {}\n```\n\n"
			fence := c.open + "\n" + body + c.close
			md := before + fence + "\n\nAfter.\n"
			b, err := Extract([]byte(md))
			if err != nil {
				t.Fatalf("Extract: %v", err)
			}
			if string(b.Body) != body {
				t.Errorf("Body = %q, want %q", b.Body, body)
			}
			if got := md[b.Start:b.End]; got != fence {
				t.Errorf("md[Start:End] = %q, want %q", got, fence)
			}
		})
	}
}

func TestExtractCRLFAndUnclosed(t *testing.T) {
	t.Parallel()
	md := "x\r\n```paraplane\r\nwaves: []\r\n```\r\ny\r\n"
	b, err := Extract([]byte(md))
	if err != nil {
		t.Fatal(err)
	}
	if string(b.Body) != "waves: []\r\n" {
		t.Errorf("Body = %q", b.Body)
	}
	if got := md[b.Start:b.End]; got != "```paraplane\r\nwaves: []\r\n```" {
		t.Errorf("block = %q", got)
	}

	md = "x\n```paraplane\nwaves: []\n``` not a close\n"
	b, err = Extract([]byte(md))
	if err != nil {
		t.Fatal(err)
	}
	if string(b.Body) != "waves: []\n``` not a close\n" || b.End != len(md) {
		t.Errorf("unclosed: Body = %q, End = %d (len %d)", b.Body, b.End, len(md))
	}
}

func TestExtractNotFences(t *testing.T) {
	t.Parallel()
	for _, md := range []string{
		"    ```paraplane\n    waves: []\n    ```\n", // indented 4: code block, not fence
		"``paraplane\nwaves: []\n``\n",               // too short
		"```paraplane`x\nwaves: []\n```\n",           // backtick in info
		"```paraplanes\nwaves: []\n```\n",            // other info word
		"```yaml paraplane\nwaves: []\n```\n",        // not the first word
	} {
		if _, err := Extract([]byte(md)); !errors.Is(err, ErrNoBlock) {
			t.Errorf("Extract(%q) err = %v, want ErrNoBlock", md, err)
		}
	}
}

func TestExtractIgnoresNestedExample(t *testing.T) {
	t.Parallel()
	md := "# How to\n\n````markdown\n```paraplane\nwaves: []\n```\n````\n"
	if _, err := Extract([]byte(md)); !errors.Is(err, ErrNoBlock) {
		t.Fatalf("err = %v, want ErrNoBlock", err)
	}
	md = "~~~\n```paraplane\nwaves: []\n```\n~~~\n"
	if _, err := Extract([]byte(md)); !errors.Is(err, ErrNoBlock) {
		t.Fatalf("tilde wrapper: err = %v, want ErrNoBlock", err)
	}
	// An unclosed fence swallows the rest of the file.
	md = "```text\nfoo\n\n```paraplane\nwaves: []\n"
	if _, err := Extract([]byte(md)); !errors.Is(err, ErrNoBlock) {
		t.Fatalf("unclosed wrapper: err = %v, want ErrNoBlock", err)
	}
	// A nested example plus one real block is fine.
	md = "````markdown\n```paraplane\nx: 1\n```\n````\n\n```paraplane\nwaves: []\n```\n"
	b, err := Extract([]byte(md))
	if err != nil {
		t.Fatal(err)
	}
	if string(b.Body) != "waves: []\n" {
		t.Errorf("Body = %q", b.Body)
	}
}

func TestExtractMany(t *testing.T) {
	t.Parallel()
	md := "```paraplane\nwaves: []\n```\n\n~~~paraplane\nwaves: []\n~~~\n"
	if _, err := Extract([]byte(md)); !errors.Is(err, ErrManyBlocks) {
		t.Fatalf("err = %v, want ErrManyBlocks", err)
	}
}

func TestParseUnknownKey(t *testing.T) {
	t.Parallel()
	body := "waves:\n  - id: W1\n    tasks:\n      - id: T1\n        title: x\n        depnds: [T0]\n"
	_, err := Parse([]byte(body))
	var se *SchemaError
	if !errors.As(err, &se) {
		t.Fatalf("err = %#v, want *SchemaError", err)
	}
	if !strings.Contains(se.Error(), "depnds") {
		t.Errorf("message %q lacks depnds", se.Error())
	}
	if strings.Contains(se.Error(), "\n") {
		t.Errorf("message %q is multi-line", se.Error())
	}
}

func TestParseWrongType(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		"waves: 5\n",
		"version: x\n",
		"waves:\n  - id: W1\n    tasks:\n      - id: T1\n        owns: {a: b}\n",
	} {
		var se *SchemaError
		if _, err := Parse([]byte(body)); !errors.As(err, &se) {
			t.Errorf("Parse(%q) err = %#v, want *SchemaError", body, err)
		}
	}
}

func TestParseAliasHint(t *testing.T) {
	t.Parallel()
	for _, owns := range []string{"[**/x]", "[*.go]", "[a/{x}.go]"} {
		body := "waves:\n  - id: W1\n    tasks:\n      - id: T1\n        title: x\n        owns: " + owns + "\n"
		_, err := Parse([]byte(body))
		var se *SyntaxError
		if !errors.As(err, &se) {
			t.Fatalf("owns %s: err = %#v, want *SyntaxError", owns, err)
		}
		if !strings.HasSuffix(se.Error(), "quote glob patterns") {
			t.Errorf("owns %s: message %q lacks hint", owns, se.Error())
		}
		if strings.Contains(se.Error(), "\n") {
			t.Errorf("owns %s: message %q is multi-line", owns, se.Error())
		}
	}
}

func TestParseSyntaxNoHint(t *testing.T) {
	t.Parallel()
	_, err := Parse([]byte("waves:\n  - id: W1\n   bad: [\n"))
	var se *SyntaxError
	if !errors.As(err, &se) {
		t.Fatalf("err = %#v, want *SyntaxError", err)
	}
	if strings.Contains(se.Error(), "\n") {
		t.Errorf("multi-line message %q", se.Error())
	}
}

func genPlan(tasks, ownsPer, readsPer int) string {
	var sb strings.Builder
	sb.WriteString("waves:\n  - id: W1\n    tasks:\n")
	for i := range tasks {
		fmt.Fprintf(&sb, "      - id: T%d\n        title: t\n", i)
		if ownsPer > 0 {
			sb.WriteString("        owns:\n")
			for j := range ownsPer {
				fmt.Fprintf(&sb, "          - o%d/%d\n", i, j)
			}
		}
		if readsPer > 0 {
			sb.WriteString("        reads:\n")
			for j := range readsPer {
				fmt.Fprintf(&sb, "          - r%d/%d\n", i, j)
			}
		}
	}
	return sb.String()
}

func TestParseLimits(t *testing.T) {
	t.Parallel()
	ok := []struct {
		name string
		body string
	}{
		{"500 tasks", genPlan(MaxTasks, 0, 0)},
		{"100 paths in task", genPlan(1, 50, 50)},
		{"2000 paths in plan", genPlan(20, 50, 50)},
	}
	for _, c := range ok {
		if _, err := Parse([]byte(c.body)); err != nil {
			t.Errorf("%s: unexpected err %v", c.name, err)
		}
	}
	bad := []struct {
		name string
		body string
	}{
		{"501 tasks", genPlan(MaxTasks+1, 0, 0)},
		{"101 paths in task", genPlan(1, 51, 50)},
		{"2001 paths in plan", genPlan(20, 50, 50) + "      - id: X\n        title: t\n        owns: [x]\n"},
		{"body > 256 KB", "waves: []\n" + strings.Repeat("#", MaxBlock)},
	}
	for _, c := range bad {
		var le *LimitError
		if _, err := Parse([]byte(c.body)); !errors.As(err, &le) {
			t.Errorf("%s: err = %#v, want *LimitError", c.name, err)
		}
	}
	var se *SchemaError
	if _, err := Parse([]byte("version: 2\nwaves: []\n")); !errors.As(err, &se) {
		t.Errorf("version 2: err = %#v, want *SchemaError", err)
	}
	for _, v := range []string{"", "version: 1\n"} {
		if _, err := Parse([]byte(v + "waves: []\n")); err != nil {
			t.Errorf("%q: unexpected err %v", v, err)
		}
	}
}

func TestParseEmpty(t *testing.T) {
	t.Parallel()
	p, err := Parse([]byte("  \n# only a comment\n"))
	if err != nil || p == nil || len(p.Waves) != 0 {
		t.Fatalf("Parse(empty) = %#v, %v", p, err)
	}
}

func TestLoadOK(t *testing.T) {
	t.Parallel()
	md := "# Plan\n\nSome text.\n\n```paraplane\n" + specBlock + "```\n"
	p, err := Load([]byte(md))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p.Version != 1 || p.Gate != "go test ./..." {
		t.Errorf("Version/Gate = %d/%q", p.Version, p.Gate)
	}
	if len(p.Waves) != 2 || p.Waves[0].Gate != "go vet ./... && go test ./..." || p.Waves[1].Gate != "" {
		t.Fatalf("waves = %#v", p.Waves)
	}
	ts := p.Tasks()
	if len(ts) != 3 {
		t.Fatalf("len(Tasks) = %d", len(ts))
	}
	ids := []string{ts[0].ID, ts[1].ID, ts[2].ID}
	if !reflect.DeepEqual(ids, []string{"T1", "T2", "T3"}) {
		t.Errorf("ids = %v", ids)
	}
	t1, t3 := ts[0], ts[2]
	if t1.Title != "Пакет config" || t1.Verify != "go test ./internal/config/" ||
		!reflect.DeepEqual(t1.Owns, []string{"internal/config/**"}) {
		t.Errorf("T1 = %#v", t1)
	}
	if !reflect.DeepEqual(t3.Depends, []string{"T2"}) {
		t.Errorf("T3.Depends = %v", t3.Depends)
	}
	if !reflect.DeepEqual(t3.Reads, []string{"internal/plan/plan.go"}) {
		t.Errorf("T3.Reads = %v", t3.Reads)
	}
	// Tasks returns pointers into the plan.
	ts[0].Title = "changed"
	if p.Waves[0].Tasks[0].Title != "changed" {
		t.Error("Tasks() does not point into the plan")
	}
}

func TestLoadErrors(t *testing.T) {
	t.Parallel()
	if _, err := Load([]byte("# nothing\n")); !errors.Is(err, ErrNoBlock) {
		t.Errorf("err = %v, want ErrNoBlock", err)
	}
	var se *SchemaError
	if _, err := Load([]byte("```paraplane\nversion: 2\n```\n")); !errors.As(err, &se) {
		t.Errorf("err = %v, want *SchemaError", err)
	}
}

func TestExtractBOM(t *testing.T) {
	t.Parallel()
	md := []byte("\xef\xbb\xbf```paraplane\nversion: 1\n```\n")
	b, err := Extract(md)
	if err != nil {
		t.Fatal(err)
	}
	if string(b.Body) != "version: 1\n" {
		t.Errorf("Body = %q", b.Body)
	}
	if b.Start != 3 || string(md[b.Start:b.End]) != "```paraplane\nversion: 1\n```" {
		t.Errorf("Start/End = %d/%d, slice %q", b.Start, b.End, md[b.Start:b.End])
	}
	if _, err := Load(md); err != nil {
		t.Errorf("Load: %v", err)
	}
}

func TestParseVersionZero(t *testing.T) {
	t.Parallel()
	var se *SchemaError
	_, err := Parse([]byte("version: 0\nwaves: []\n"))
	if !errors.As(err, &se) || !strings.Contains(err.Error(), "unsupported version 0 (only 1 is supported)") {
		t.Errorf("version 0: err = %#v", err)
	}
	for _, in := range []string{"waves: []\n", "version: null\nwaves: []\n", "version: ~\nwaves: []\n", "version:\nwaves: []\n"} {
		if _, err := Parse([]byte(in)); err != nil {
			t.Errorf("absent/null version %q: %v", in, err)
		}
	}
}

func TestParseLimitBeforeVersion(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("version: 0\nwaves:\n  - id: W1\n    tasks:\n")
	for i := 0; i <= MaxTasks; i++ {
		fmt.Fprintf(&sb, "      - id: T%d\n", i)
	}
	var le *LimitError
	if _, err := Parse([]byte(sb.String())); !errors.As(err, &le) {
		t.Errorf("err = %#v, want *LimitError", err)
	}
}

func TestExtractDeindentsIndentedFence(t *testing.T) {
	t.Parallel()
	md := "intro\n  ```paraplane\n  waves: []\n    x: 1\n bad: 2\n\n  ```\nend\n"
	b, err := Extract([]byte(md))
	if err != nil {
		t.Fatal(err)
	}
	if want := "waves: []\n  x: 1\nbad: 2\n\n"; string(b.Body) != want {
		t.Errorf("Body = %q, want %q", b.Body, want)
	}
	if got := md[b.Start:b.End]; got != "  ```paraplane\n  waves: []\n    x: 1\n bad: 2\n\n  ```" {
		t.Errorf("offsets index %q", got)
	}
}

func TestExtractDeindentsTabs(t *testing.T) {
	t.Parallel()
	// CommonMark tab stops are every 4 columns from the start of the line; a
	// tab straddling the fence's indent leaves the columns past it as spaces.
	for _, tc := range []struct{ ind, body, want string }{
		{"  ", "\tx\n", "  x\n"},
		{" ", "\tx\n", "   x\n"},
		{"   ", "\tx\n", " x\n"},
		{"  ", " \tx\n", "  x\n"},
		{"  ", "  \tx\n", "\tx\n"},
		{"   ", "\t\tx\n", " \tx\n"},
		{"  ", "\t\n", "  \n"},
		{"  ", "x\t\n", "x\t\n"},
	} {
		md := tc.ind + "```paraplane\n" + tc.body + tc.ind + "```\n"
		b, err := Extract([]byte(md))
		if err != nil {
			t.Fatal(err)
		}
		if string(b.Body) != tc.want {
			t.Errorf("indent %d, body %q: Body = %q, want %q", len(tc.ind), tc.body, b.Body, tc.want)
		}
		if got := md[b.Start:b.End]; got != strings.TrimSuffix(md, "\n") {
			t.Errorf("indent %d, body %q: offsets index %q", len(tc.ind), tc.body, got)
		}
	}
}

func TestParseTabDeindentKeepsLines(t *testing.T) {
	t.Parallel()
	// The body keeps its line count, so a YAML error points at the same line
	// as for the same plan indented with spaces.
	parse := func(md string) error {
		b, err := Extract([]byte(md))
		if err != nil {
			t.Fatal(err)
		}
		_, err = Parse(b.Body)
		return err
	}
	md := "  ```paraplane\n  waves:\n\t- id: W1\n\t  a: b: c\n  ```\n"
	errTab, errRef := parse(md), parse(strings.ReplaceAll(md, "\t", "    "))
	if errTab == nil || errRef == nil || errTab.Error() != errRef.Error() || !strings.Contains(errTab.Error(), "line 3") {
		t.Errorf("tab: %v; spaces: %v", errTab, errRef)
	}
}

func TestExtractDeindentsCRLF(t *testing.T) {
	t.Parallel()
	md := " ```paraplane\r\n a: 1\r\n ```\r\n"
	b, err := Extract([]byte(md))
	if err != nil {
		t.Fatal(err)
	}
	if want := "a: 1\r\n"; string(b.Body) != want {
		t.Errorf("Body = %q, want %q", b.Body, want)
	}
}

func TestExtractClosingFenceRules(t *testing.T) {
	t.Parallel()
	// indented >= 4 does not close; shorter does not close
	for _, closer := range []string{"    ```", "``"} {
		md := "```paraplane\na: 1\n" + closer + "\nb: 2\n"
		b, err := Extract([]byte(md))
		if err != nil {
			t.Fatal(err)
		}
		if want := "a: 1\n" + closer + "\nb: 2\n"; string(b.Body) != want {
			t.Errorf("closer %q: Body = %q, want %q", closer, b.Body, want)
		}
		if b.End != len(md) {
			t.Errorf("closer %q: End = %d, want %d", closer, b.End, len(md))
		}
	}
}

func TestExtractManyWithUnclosed(t *testing.T) {
	t.Parallel()
	md := "```paraplane\na: 1\n```\n\n```paraplane\nb: 2\n"
	if _, err := Extract([]byte(md)); !errors.Is(err, ErrManyBlocks) {
		t.Errorf("err = %v, want ErrManyBlocks", err)
	}
}

func TestLoadWithinDeadline(t *testing.T) {
	t.Parallel()
	md := []byte("# p\n\n```paraplane\n" + specBlock + "```\n")
	past := time.Now().Add(-time.Second)
	if _, err := LoadWithin(md, past); !errors.Is(err, ErrDeadline) {
		t.Fatalf("past deadline: err = %v, want ErrDeadline", err)
	}
	if p, err := LoadWithin(md, time.Now().Add(time.Minute)); err != nil || len(p.Waves) != 2 {
		t.Fatalf("future deadline: %v %v", p, err)
	}
	if _, err := LoadWithin(md, time.Time{}); err != nil {
		t.Fatalf("zero deadline: %v", err)
	}
}

// TestDecodeErrorKinds pins the messages of the YAML library that decodeError
// relies on: one case per error kind.
func TestDecodeErrorKinds(t *testing.T) {
	t.Parallel()
	wrap := func(owns string) string {
		return "waves:\n  - id: W1\n    tasks:\n      - id: T1\n        title: x\n        owns: " + owns + "\n"
	}
	cases := []struct {
		name, body string
		kind       string // "schema" | "syntax"
		contains   string
		hint       bool
	}{
		{"unknown field", "waves:\n  - id: W1\n    tasks:\n      - id: T1\n        depnds: [T0]\n", "schema", "depnds", false},
		{"wrong type", "waves: 5\n", "schema", "cannot unmarshal", false},
		{"alias star", wrap("[*.go]"), "syntax", "", true},
		{"alias double star", wrap("[**/x]"), "syntax", "", true},
		{"brace", wrap("[a/{x}.go]"), "syntax", "", true},
		{"unclosed flow", "waves:\n  - id: W1\n   bad: [\n", "syntax", "", false},
		{"two documents", "version: 1\n---\nversion: 1\n", "syntax", "more than one YAML document", false},
		{"tab indent", "waves:\n\t- id: W1\n", "syntax", "", false},
	}
	for _, c := range cases {
		_, err := Parse([]byte(c.body))
		switch c.kind {
		case "schema":
			if _, ok := errors.AsType[*SchemaError](err); !ok {
				t.Errorf("%s: err = %#v, want *SchemaError", c.name, err)
				continue
			}
		default:
			if _, ok := errors.AsType[*SyntaxError](err); !ok {
				t.Errorf("%s: err = %#v, want *SyntaxError", c.name, err)
				continue
			}
		}
		msg := err.Error()
		if strings.Contains(msg, "\n") {
			t.Errorf("%s: multi-line message %q", c.name, msg)
		}
		if c.contains != "" && !strings.Contains(msg, c.contains) {
			t.Errorf("%s: message %q lacks %q", c.name, msg, c.contains)
		}
		if got := strings.HasSuffix(msg, "quote glob patterns"); got != c.hint {
			t.Errorf("%s: hint = %v, want %v (%q)", c.name, got, c.hint, msg)
		}
	}
}
