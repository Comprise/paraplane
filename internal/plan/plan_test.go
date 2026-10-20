package plan

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
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
	md := "```paraplane\nwaves: []\n```\n\n~~~paraplane\nwaves: []\n~~~\n"
	if _, err := Extract([]byte(md)); !errors.Is(err, ErrManyBlocks) {
		t.Fatalf("err = %v, want ErrManyBlocks", err)
	}
}

func TestParseUnknownKey(t *testing.T) {
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
	for i := 0; i < tasks; i++ {
		fmt.Fprintf(&sb, "      - id: T%d\n        title: t\n", i)
		if ownsPer > 0 {
			sb.WriteString("        owns:\n")
			for j := 0; j < ownsPer; j++ {
				fmt.Fprintf(&sb, "          - o%d/%d\n", i, j)
			}
		}
		if readsPer > 0 {
			sb.WriteString("        reads:\n")
			for j := 0; j < readsPer; j++ {
				fmt.Fprintf(&sb, "          - r%d/%d\n", i, j)
			}
		}
	}
	return sb.String()
}

func TestParseLimits(t *testing.T) {
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
	p, err := Parse([]byte("  \n# only a comment\n"))
	if err != nil || p == nil || len(p.Waves) != 0 {
		t.Fatalf("Parse(empty) = %#v, %v", p, err)
	}
}

func TestLoadOK(t *testing.T) {
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
	if _, err := Load([]byte("# nothing\n")); !errors.Is(err, ErrNoBlock) {
		t.Errorf("err = %v, want ErrNoBlock", err)
	}
	var se *SchemaError
	if _, err := Load([]byte("```paraplane\nversion: 2\n```\n")); !errors.As(err, &se) {
		t.Errorf("err = %v, want *SchemaError", err)
	}
}

func TestExtractBOM(t *testing.T) {
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
