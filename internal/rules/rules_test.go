package rules

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"paraplane/internal/layout"
	"paraplane/internal/plan"
)

// defaultHot is the hot_files default of spec section 9.
var defaultHot = []string{
	"go.mod", "go.sum", "**/package.json", "**/package-lock.json", "**/pnpm-lock.yaml",
	"**/yarn.lock", "**/Cargo.toml", "**/Cargo.lock", "**/pyproject.toml", "**/uv.lock",
	"**/poetry.lock", "**/migrations/**",
}

func md(block string) []byte {
	return []byte("# Plan\n\n```paraplane\n" + block + "```\n")
}

func opts() Options { return Options{HotFiles: defaultHot} }

// only returns the findings with the given code.
func only(r Result, code string) []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.Code == code {
			out = append(out, f)
		}
	}
	return out
}

// one asserts exactly one finding with code, where, severity and every id in msg.
func one(t *testing.T, r Result, code, where string, sev Severity, ids ...string) Finding {
	t.Helper()
	fs := only(r, code)
	if len(fs) != 1 {
		t.Fatalf("%s findings = %v; want exactly one (all: %v)", code, fs, r.Findings)
	}
	f := fs[0]
	if f.Where != where || f.Severity != sev {
		t.Errorf("%s: where %q severity %q; want %q %q", code, f.Where, f.Severity, where, sev)
	}
	for _, id := range ids {
		if !strings.Contains(f.Msg, id) {
			t.Errorf("%s: msg %q does not mention %q", code, f.Msg, id)
		}
	}
	return f
}

func none(t *testing.T, r Result, code string) {
	t.Helper()
	if fs := only(r, code); len(fs) != 0 {
		t.Errorf("unexpected %s findings: %v", code, fs)
	}
}

const twoWaves = `gate: "make"
waves:
  - id: W1
    tasks:
      - {id: T1, title: a, owns: ["a/**"], verify: x}
      - {id: T2, title: b, owns: ["b/**"], verify: x}
  - id: W2
    tasks:
      - {id: T3, title: c, owns: ["c/**"], reads: ["a/x.go"], depends: [T1], verify: x}
`

func TestCleanPlan(t *testing.T) {
	r := Check(md(twoWaves), opts())
	if len(r.Findings) != 0 || r.Plan == nil || r.Suggested != nil {
		t.Fatalf("findings %v, plan %v, suggested %v", r.Findings, r.Plan, r.Suggested)
	}
}

func TestE0(t *testing.T) {
	r := Check([]byte("# no block\n"), opts())
	f := one(t, r, "E0", "", Error, "paraplane")
	if f.Msg != plan.ErrNoBlock.Error() || r.Plan != nil || len(r.Findings) != 1 {
		t.Errorf("E0 = %+v, plan %v, findings %v", f, r.Plan, r.Findings)
	}
	if got := f.String(); got != "error E0: no ```paraplane block" {
		t.Errorf("String = %q", got)
	}

	for name, in := range map[string][]byte{
		"many":   []byte("```paraplane\nwaves: []\n```\n```paraplane\nwaves: []\n```\n"),
		"syntax": md("waves: [\n"),
		"alias":  md("waves:\n  - id: W1\n    tasks:\n      - {id: T1, title: a, owns: [**/x]}\n"),
		"limit":  md("waves:\n  - id: W1\n    tasks:\n" + strings.Repeat("      - {id: T, title: a}\n", 501)),
	} {
		r := Check(in, opts())
		if len(r.Findings) != 1 || r.Findings[0].Code != "E0" || r.Plan != nil {
			t.Errorf("%s: findings %v, plan %v", name, r.Findings, r.Plan)
		}
	}
}

func TestE1(t *testing.T) {
	cases := map[string]struct {
		block, where string
		ids          []string
	}{
		"no waves":    {"gate: x\nwaves: []\n", "", nil},
		"empty wave":  {"gate: x\nwaves:\n  - id: W1\n    tasks: []\n", "W1", []string{"W1"}},
		"no title":    {"gate: x\nwaves:\n  - id: W1\n    tasks:\n      - {id: T1, owns: [a]}\n", "T1", []string{"T1"}},
		"dup id":      {"gate: x\nwaves:\n  - id: W1\n    tasks:\n      - {id: T1, title: a, owns: [a]}\n      - {id: T1, title: b, owns: [b]}\n", "T1", []string{"T1"}},
		"dup wave id": {"gate: x\nwaves:\n  - id: T1\n    tasks:\n      - {id: T1, title: a, owns: [a]}\n", "T1", []string{"T1"}},
		"bad id":      {"gate: x\nwaves:\n  - id: W1\n    tasks:\n      - {id: 1T, title: a, owns: [a]}\n", "1T", []string{"1T"}},
		"no task id":  {"gate: x\nwaves:\n  - id: W1\n    tasks:\n      - {title: a, owns: [a]}\n", "W1", []string{"W1"}},
		"no wave id":  {"gate: x\nwaves:\n  - tasks:\n      - {id: T1, title: a, owns: [a]}\n", "", nil},
		"unknown dep": {"gate: x\nwaves:\n  - id: W1\n    tasks:\n      - {id: T1, title: a, owns: [a], depends: [T9]}\n", "T1", []string{"T1", "T9"}},
		"bad path":    {"gate: x\nwaves:\n  - id: W1\n    tasks:\n      - {id: T1, title: a, owns: [\"../a\"]}\n", "T1", []string{"T1", "../a"}},
		"bad reads":   {"gate: x\nwaves:\n  - id: W1\n    tasks:\n      - {id: T1, title: a, owns: [a], reads: [\"/abs\"]}\n", "T1", []string{"T1", "/abs"}},
		"unknown key": {"gate: x\nwaves:\n  - id: W1\n    tasks:\n      - {id: T1, title: a, depnds: [T0]}\n", "", []string{"depnds"}},
		"bad version": {"version: 2\nwaves: []\n", "", []string{"version"}},
		"wrong type":  {"waves: 3\n", "", nil},
		"slash brace": {"gate: x\nwaves:\n  - id: W1\n    tasks:\n      - {id: T1, title: a, owns: [\"{a/b,c}\"]}\n", "T1", []string{"T1"}},
		"backslash":   {"gate: x\nwaves:\n  - id: W1\n    tasks:\n      - {id: T1, title: a, owns: [\"a\\\\b\"]}\n", "T1", []string{"T1"}},
		"long id":     {"gate: x\nwaves:\n  - id: W1\n    tasks:\n      - {id: T" + strings.Repeat("x", 32) + ", title: a, owns: [a]}\n", "T" + strings.Repeat("x", 32), nil},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := Check(md(c.block), opts())
			one(t, r, "E1", c.where, Error, c.ids...)
			none(t, r, "E0")
		})
	}
}

func TestE1SchemaKeepsNoPlan(t *testing.T) {
	r := Check(md("waves:\n  - id: W1\n    tasks:\n      - {id: T1, title: a, depnds: [T0]}\n"), opts())
	if r.Plan != nil || len(r.Findings) != 1 {
		t.Errorf("plan %v, findings %v", r.Plan, r.Findings)
	}
}

func TestE1WaveIDInDepends(t *testing.T) {
	r := Check(md(`gate: x
waves:
  - id: W1
    tasks:
      - {id: T1, title: a, owns: [a]}
  - id: W2
    tasks:
      - {id: T2, title: b, owns: [b], depends: [W1]}
`), opts())
	f := one(t, r, "E1", "T2", Error, "T2", "W1")
	if !strings.Contains(f.Msg, "unknown task") {
		t.Errorf("msg %q lacks %q", f.Msg, "unknown task")
	}
}

func TestE1DropsBadPathOnly(t *testing.T) {
	// The bad path is dropped; the rest of the task still counts (no W4).
	r := Check(md("gate: x\nwaves:\n  - id: W1\n    tasks:\n      - {id: T1, title: a, owns: [a, \"../b\"], verify: v}\n"), opts())
	one(t, r, "E1", "T1", Error, "../b")
	none(t, r, "W4")
	if got := r.Plan.Waves[0].Tasks[0].Owns; !reflect.DeepEqual(got, []string{"a"}) {
		t.Errorf("owns = %q", got)
	}
}

func TestE1SkipsConflicts(t *testing.T) {
	// With E1 the owns conflict is not reported, but E7, W1, W2, W4 are.
	r := Check(md(`waves:
  - id: W1
    tasks:
      - {id: T1, owns: [a]}
      - {id: T2, title: b, owns: [a]}
      - {id: T3, title: c}
`), Options{HotFiles: defaultHot, MaxParallel: 2})
	one(t, r, "E1", "T1", Error)
	none(t, r, "E2")
	one(t, r, "E7", "W1", Error)
	if n := len(only(r, "W1")); n != 3 {
		t.Errorf("W1 count = %d", n)
	}
	one(t, r, "W2", "W1", Warning)
	one(t, r, "W4", "T3", Warning)
}

func TestE2(t *testing.T) {
	r := Check(md(`gate: x
waves:
  - id: W1
    tasks:
      - {id: T1, title: a, owns: [internal/a.go], verify: v}
      - {id: T2, title: b, owns: [internal/a.go], verify: v}
`), opts())
	f := one(t, r, "E2", "W1", Error, "T1", "T2")
	if got := f.String(); got != "error E2 W1: T1 and T2 both own internal/a.go" {
		t.Errorf("String = %q", got)
	}

	r = Check(md(`gate: x
waves:
  - id: W1
    tasks:
      - {id: T1, title: a, owns: ["internal/**"], verify: v}
      - {id: T2, title: b, owns: ["internal/a/*.go"], verify: v}
`), opts())
	f = one(t, r, "E2", "W1", Error, "T1", "T2", "internal/**", "internal/a/*.go")
	if !strings.Contains(f.Msg, "may overlap") {
		t.Errorf("msg %q lacks %q", f.Msg, "may overlap")
	}
}

func TestE2AndE5SamePair(t *testing.T) {
	r := Check(md(`gate: x
waves:
  - id: W1
    tasks:
      - {id: T1, title: a, owns: [go.mod, a], verify: v}
      - {id: T2, title: b, owns: [go.mod, b], verify: v}
`), opts())
	one(t, r, "E2", "W1", Error, "T1", "T2", "go.mod")
	one(t, r, "E5", "W1", Error, "T1", "T2", "go.mod")
	if r.Errors() != 2 {
		t.Errorf("errors = %d", r.Errors())
	}
}

func TestE3(t *testing.T) {
	r := Check(md(`gate: x
waves:
  - id: W1
    tasks:
      - {id: T1, title: a, owns: [a], depends: [T2], verify: v}
  - id: W2
    tasks:
      - {id: T2, title: b, owns: [b], verify: v}
`), opts())
	f := one(t, r, "E3", "T1", Error, "T1", "T2")
	if !strings.Contains(f.Msg, "later") {
		t.Errorf("msg %q", f.Msg)
	}

	r = Check(md(`gate: x
waves:
  - id: W1
    tasks:
      - {id: T1, title: a, owns: [a], depends: [T2], verify: v}
      - {id: T2, title: b, owns: [b], verify: v}
`), opts())
	f = one(t, r, "E3", "T1", Error, "T1", "T2")
	if !strings.Contains(f.Msg, "same wave") {
		t.Errorf("msg %q", f.Msg)
	}
}

func TestCheckCycleNoSuggest(t *testing.T) {
	r := Check(md(`gate: x
waves:
  - id: W1
    tasks:
      - {id: T1, title: a, owns: [a], depends: [T2], verify: v}
      - {id: T2, title: b, owns: [a], depends: [T1], verify: v}
  - id: W2
    tasks:
      - {id: T3, title: c, owns: [c], verify: v}
`), opts())
	if n := len(only(r, "E3")); n != 2 {
		t.Errorf("E3 = %d, findings %v", n, r.Findings)
	}
	none(t, r, "E2")
	none(t, r, "W3")
	if r.Suggested != nil {
		t.Errorf("Suggested = %v", r.Suggested)
	}
}

func TestE4(t *testing.T) {
	r := Check(md(`gate: x
waves:
  - id: W1
    tasks:
      - {id: T1, title: a, owns: ["internal/a/**"], verify: v}
      - {id: T2, title: b, owns: [b], reads: [internal/a/x.go], verify: v}
`), opts())
	f := one(t, r, "E4", "W1", Error, "T1", "T2", "internal/a/x.go")
	if !strings.Contains(f.Msg, "may overlap") {
		t.Errorf("msg %q", f.Msg)
	}
	// The reader is named first.
	if strings.Index(f.Msg, "T2") > strings.Index(f.Msg, "T1") {
		t.Errorf("reader should come first: %q", f.Msg)
	}

	r = Check(md(`gate: x
waves:
  - id: W1
    tasks:
      - {id: T1, title: a, owns: [b], reads: [a.go], verify: v}
      - {id: T2, title: b, owns: [a.go], verify: v}
`), opts())
	f = one(t, r, "E4", "W1", Error, "T1", "T2", "a.go")
	if f.Msg != "T1 reads a.go, which T2 owns" {
		t.Errorf("msg %q", f.Msg)
	}
}

func TestE5(t *testing.T) {
	r := Check(md(`gate: x
waves:
  - id: W1
    tasks:
      - {id: T1, title: a, owns: [migrations/0003_a.sql], verify: v}
      - {id: T2, title: b, owns: [migrations/0003_b.sql], verify: v}
`), opts())
	one(t, r, "E5", "W1", Error, "T1", "T2", "**/migrations/**")
	none(t, r, "E2")
}

func TestE6(t *testing.T) {
	r := Check(md(`gate: x
waves:
  - id: W1
    tasks:
      - {id: T1, title: a, owns: [internal/a.go], verify: v}
  - id: W2
    tasks:
      - {id: T2, title: b, owns: [internal/a.go], verify: v}
`), opts())
	one(t, r, "E6", "T2", Error, "T1", "T2", "internal/a.go", "W1")
	none(t, r, "W3")

	// A direct dependency allows it.
	r = Check(md(`gate: x
waves:
  - id: W1
    tasks:
      - {id: T1, title: a, owns: [internal/a.go], verify: v}
  - id: W2
    tasks:
      - {id: T2, title: b, owns: ["internal/**"], depends: [T1], verify: v}
`), opts())
	none(t, r, "E6")
}

func TestE6TransitiveOK(t *testing.T) {
	r := Check(md(`gate: x
waves:
  - id: W1
    tasks:
      - {id: T1, title: a, owns: [internal/a.go], verify: v}
  - id: W2
    tasks:
      - {id: T2, title: b, owns: [b], depends: [T1], verify: v}
  - id: W3
    tasks:
      - {id: T3, title: c, owns: [internal/a.go], depends: [T2], verify: v}
`), opts())
	if len(r.Findings) != 0 {
		t.Errorf("findings %v", r.Findings)
	}
}

func TestE7(t *testing.T) {
	r := Check(md(`waves:
  - id: W1
    gate: "make"
    tasks:
      - {id: T1, title: a, owns: [a], verify: v}
  - id: W2
    tasks:
      - {id: T2, title: b, owns: [b], depends: [T1], verify: v}
`), opts())
	one(t, r, "E7", "W2", Error, "W2")
}

func TestE7DefaultGate(t *testing.T) {
	block := `waves:
  - id: W1
    tasks:
      - {id: T1, title: a, owns: [a], verify: v}
`
	if r := Check(md(block), opts()); len(only(r, "E7")) != 1 {
		t.Errorf("no gate: %v", r.Findings)
	}
	o := opts()
	o.DefaultGate = "make test"
	if r := Check(md(block), o); len(r.Findings) != 0 {
		t.Errorf("default gate: %v", r.Findings)
	}
}

func TestW1(t *testing.T) {
	r := Check(md("gate: x\nwaves:\n  - id: W1\n    tasks:\n      - {id: T1, title: a, owns: [a]}\n"), opts())
	f := one(t, r, "W1", "T1", Warning, "T1")
	if !strings.HasPrefix(f.String(), "warning W1 T1: ") {
		t.Errorf("String = %q", f.String())
	}
}

func TestW2(t *testing.T) {
	var b strings.Builder
	b.WriteString("gate: x\nwaves:\n  - id: W1\n    tasks:\n")
	for i := range 7 {
		fmt.Fprintf(&b, "      - {id: T%d, title: a, owns: [f%d], verify: v}\n", i, i)
	}
	r := Check(md(b.String()), opts())
	one(t, r, "W2", "W1", Warning, "W1", "7", "6")

	o := opts()
	o.MaxParallel = 7
	none(t, Check(md(b.String()), o), "W2")
}

func TestW3(t *testing.T) {
	r := Check(md(`gate: x
waves:
  - id: W1
    tasks:
      - {id: T1, title: a, owns: [a], verify: v}
  - id: W2
    tasks:
      - {id: T2, title: b, owns: [b], verify: v}
  - id: W3
    tasks:
      - {id: T3, title: c, owns: [c], depends: [T1], verify: v}
`), opts())
	f := one(t, r, "W3", "", Warning, "2", "3")
	const w3 = "the layout can be shorter: 2 waves instead of 3 (`paraplane check --suggest` prints it)"
	if f.Msg != w3 {
		t.Errorf("W3 msg = %q, want %q", f.Msg, w3)
	}
	want := [][]string{{"T1", "T2"}, {"T3"}}
	if !reflect.DeepEqual(r.Suggested, want) {
		t.Errorf("Suggested = %v, want %v", r.Suggested, want)
	}

	// Singular: one wave instead of two.
	r = Check(md(`gate: x
waves:
  - id: W1
    tasks:
      - {id: T1, title: a, owns: [a], verify: v}
  - id: W2
    tasks:
      - {id: T2, title: b, owns: [b], verify: v}
`), opts())
	f = one(t, r, "W3", "", Warning)
	const w3one = "the layout can be shorter: 1 wave instead of 2 (`paraplane check --suggest` prints it)"
	if f.Msg != w3one {
		t.Errorf("W3 msg = %q, want %q", f.Msg, w3one)
	}

	// W3 is not reported when there are errors.
	r = Check(md(`waves:
  - id: W1
    tasks:
      - {id: T1, title: a, owns: [a], verify: v}
  - id: W2
    tasks:
      - {id: T2, title: b, owns: [b], verify: v}
`), opts())
	none(t, r, "W3")
	if r.Suggested != nil {
		t.Errorf("Suggested with errors = %v", r.Suggested)
	}
}

func TestW4(t *testing.T) {
	r := Check(md("gate: x\nwaves:\n  - id: W1\n    tasks:\n      - {id: T1, title: a, verify: v}\n"), opts())
	one(t, r, "W4", "T1", Warning, "T1")
}

func TestW5(t *testing.T) {
	block := "gate: x\nwaves:\n  - id: W1\n    tasks:\n      - {id: T1, title: a, owns: [internal/a, \"internal/b/**\", internal/c.go], verify: v}\n"
	var asked []string
	o := opts()
	o.IsDir = func(rel string) bool {
		asked = append(asked, rel)
		return rel == "internal/a"
	}
	r := Check(md(block), o)
	one(t, r, "W5", "T1", Warning, "internal/a", "internal/a/**")
	if !reflect.DeepEqual(asked, []string{"internal/a", "internal/c.go"}) {
		t.Errorf("IsDir asked %q", asked)
	}
	none(t, Check(md(block), opts()), "W5")
}

func TestFindingsOrder(t *testing.T) {
	r := Check(md(`waves:
  - id: W1
    tasks:
      - {id: T1, title: a}
      - {id: T2, title: b, owns: [b], depends: [T9]}
  - id: W2
    tasks:
      - {id: T3, title: c, owns: [c]}
`), opts())
	var got []string
	for _, f := range r.Findings {
		got = append(got, f.Code+" "+f.Where)
	}
	want := []string{"E1 T2", "E7 W1", "E7 W2", "W1 T1", "W1 T2", "W1 T3", "W4 T1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("order = %q, want %q", got, want)
	}
}

// specExample is the block of spec section 4.
const specExample = `version: 1                  # необязательно; поддерживается только 1
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

func TestCheckSpecExample(t *testing.T) {
	r := Check(md(specExample), opts())
	if r.Errors() != 0 || r.Plan == nil {
		t.Fatalf("findings %v", r.Findings)
	}
	if got := r.Summary(); got != "plan ok with 2 warning(s): 2 waves, 3 tasks" {
		t.Errorf("Summary = %q (findings %v)", got, r.Findings)
	}
}

func TestSummary(t *testing.T) {
	if got := Check(md(twoWaves), opts()).Summary(); got != "plan ok: 2 waves, 3 tasks" {
		t.Errorf("ok: %q", got)
	}
	r := Check(md("gate: x\nwaves:\n  - id: W1\n    tasks:\n      - {id: T1, title: a}\n"), opts())
	if r.Errors() != 0 || r.Warnings() != 2 || r.Summary() != "plan ok with 2 warning(s): 1 waves, 1 tasks" {
		t.Errorf("warnings: %d %d %q", r.Errors(), r.Warnings(), r.Summary())
	}
	r = Check(md("waves:\n  - id: W1\n    tasks:\n      - {id: T1}\n"), opts())
	if r.Summary() != "plan invalid: 2 error(s), 2 warning(s)" {
		t.Errorf("invalid: %q %v", r.Summary(), r.Findings)
	}
	if got := Check(nil, opts()).Summary(); got != "plan invalid: 1 error(s), 0 warning(s)" {
		t.Errorf("E0: %q", got)
	}
}

func TestAudit(t *testing.T) {
	p, err := plan.Load(md(`gate: x
waves:
  - id: W1
    tasks:
      - {id: T1, title: a, owns: ["a/**"]}
  - id: W2
    tasks:
      - {id: T2, title: b, owns: ["internal/b/**", go.mod]}
      - {id: T3, title: c, owns: [cmd/main.go]}
`))
	if err != nil {
		t.Fatal(err)
	}
	changed := []string{"internal/b/x.go", "a/y.go", "go.mod", "docs/plan.md", "cover.out", "cmd/main.go", "internal/x.go"}
	fs, err := AuditCommits(p, []string{"W2"}, changed, nil, "docs/plan.md", []string{"*.out"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range fs {
		if f.Code != "A1" || f.Severity != Error || f.Where != "W2" {
			t.Errorf("finding %+v", f)
		}
		got = append(got, f.String())
	}
	want := []string{
		"error A1 W2: a/y.go changed but no task of W2 owns it",
		"error A1 W2: internal/x.go changed but no task of W2 owns it",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Audit = %q, want %q", got, want)
	}

	if fs, err := AuditCommits(p, []string{"W1"}, []string{"a/y.go"}, nil, "docs/plan.md", nil); err != nil || len(fs) != 0 {
		t.Errorf("W1: %v %v", fs, err)
	}
	if _, err := AuditCommits(p, []string{"W9"}, changed, nil, "docs/plan.md", nil); err == nil || !strings.Contains(err.Error(), "W9") {
		t.Errorf("unknown wave: err = %v", err)
	}
	if _, err := AuditCommits(p, []string{"T2"}, changed, nil, "docs/plan.md", nil); err == nil {
		t.Errorf("task id as wave: no error")
	}

	// A stage audits several waves at once: a file of any of them is owned.
	fs, err = AuditCommits(p, []string{"W1", "W2"}, changed, nil, "docs/plan.md", []string{"*.out"})
	got = nil
	for _, f := range fs {
		got = append(got, f.String())
	}
	if want := []string{"error A1 W1,W2: internal/x.go changed but no task of W1, W2 owns it"}; err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("AuditWaves = %q, %v; want %q", got, err, want)
	}
	if _, err := AuditCommits(p, []string{"W1", "W9"}, changed, nil, "docs/plan.md", nil); err == nil || !strings.Contains(err.Error(), "W9") {
		t.Errorf("unknown wave in a stage: err = %v", err)
	}

	// Check cleans the paths Audit matches.
	r := Check(md("gate: x\nwaves:\n  - id: W1\n    tasks:\n      - {id: T1, title: a, verify: v, owns: [./a//x.go]}\n"), opts())
	if r.Errors() > 0 {
		t.Fatalf("check: %v", r.Findings)
	}
	if fs, err := AuditCommits(r.Plan, []string{"W1"}, []string{"a/x.go"}, nil, "docs/plan.md", nil); err != nil || len(fs) != 0 {
		t.Errorf("cleaned owns: %v %v", fs, err)
	}
}

// bigPlan writes a plan of 500 tasks × 4 owns in 4 waves; pattern(n) gives
// the n-th pattern.
func bigPlan(pattern func(n int) string) []byte {
	var b strings.Builder
	b.WriteString("gate: g\nwaves:\n")
	n := 0
	for w := range 4 {
		fmt.Fprintf(&b, "- id: W%d\n  tasks:\n", w+1)
		for i := range 125 {
			fmt.Fprintf(&b, "  - {id: T%d_%d, title: t, verify: v, owns: [", w, i)
			for k := range 4 {
				n++
				if k > 0 {
					b.WriteByte(',')
				}
				fmt.Fprintf(&b, "%q", pattern(n))
			}
			b.WriteString("]}\n")
		}
	}
	return md(b.String())
}

func TestCheckFast(t *testing.T) {
	in := bigPlan(func(n int) string {
		switch n % 4 {
		case 0:
			return fmt.Sprintf("pkg%d/**", n)
		case 1:
			return fmt.Sprintf("cmd/c%d/*.go", n)
		case 2:
			return fmt.Sprintf("web/w%d/{a,b}/x.ts", n)
		}
		return fmt.Sprintf("docs/d%d.md", n)
	})
	start := time.Now()
	r := Check(in, opts())
	d := time.Since(start)
	if r.Errors() != 0 {
		t.Fatalf("errors: %v", r.Findings[:min(5, len(r.Findings))])
	}
	t.Logf("Check of 500 tasks: %v", d)
	if !raceEnabled && d > time.Second {
		t.Errorf("Check took %v", d)
	}
}

func TestCheckDeadline(t *testing.T) {
	braces := strings.Repeat("{a,aa}", 8) + strings.Repeat("?", 40)
	as := strings.Repeat("a", 100)
	in := bigPlan(func(n int) string {
		if n%2 == 0 {
			return fmt.Sprintf("%s/z%04d", braces, n)
		}
		return fmt.Sprintf("%s/*z%04d", as, n)
	})
	if len(in) > plan.MaxBlock {
		t.Fatalf("hostile plan is %d bytes", len(in))
	}
	o := opts()
	start := time.Now()
	o.Deadline = start.Add(50 * time.Millisecond)
	r := Check(in, o)
	d := time.Since(start)
	if len(r.Findings) != 1 || r.Findings[0].Code != "E0" || !r.TimedOut || r.Findings[0].Msg != layout.ErrDeadline.Error() {
		t.Fatalf("findings = %v", r.Findings[:min(5, len(r.Findings))])
	}
	if r.Plan != nil || r.Suggested != nil {
		t.Errorf("plan %v, suggested %v", r.Plan, r.Suggested)
	}
	t.Logf("hostile plan: E0 after %v", d)
	if !raceEnabled && d > time.Second {
		t.Errorf("Check took %v", d)
	}
}

func codesWhere(r Result) []string {
	var got []string
	for _, f := range r.Findings {
		got = append(got, f.Code+" "+f.Where)
	}
	return got
}

func TestFindingsOrderUnknownDepends(t *testing.T) {
	r := Check(md(`gate: x
waves:
  - id: W1
    tasks:
      - {id: T1, title: a, owns: [a], depends: [T9], verify: v}
      - {id: T2, owns: [b], verify: v}
`), opts())
	if got, want := codesWhere(r), []string{"E1 T1", "E1 T2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("order = %q, want %q", got, want)
	}
}

func TestFindingsOrderE6(t *testing.T) {
	r := Check(md(`gate: x
waves:
  - id: W1
    tasks:
      - {id: T1, title: a, owns: [x.go], verify: v}
      - {id: T2, title: b, owns: [y.go], verify: v}
  - id: W2
    tasks:
      - {id: T3, title: c, owns: [y.go], verify: v}
      - {id: T4, title: d, owns: [x.go], verify: v}
`), opts())
	if got, want := codesWhere(r), []string{"E6 T3", "E6 T4"}; !reflect.DeepEqual(got, want) {
		t.Errorf("order = %q, want %q", got, want)
	}
}

func TestInvalidWaveIDWhere(t *testing.T) {
	r := Check(md(`waves:
  - id: 1W
    tasks:
      - {id: T1, title: a, owns: [a], verify: v}
      - {title: b, owns: [b]}
`), Options{MaxParallel: 1})
	got := codesWhere(r)
	want := []string{"E1 1W", "E1 ", "E7 ", "W1 ", "W2 "}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("findings = %q, want %q", got, want)
	}
	for _, f := range r.Findings[1:] {
		if !strings.Contains(f.Msg, "wave 1") {
			t.Errorf("%s msg %q does not name wave 1", f.Code, f.Msg)
		}
	}
}

func TestAuditNilPlan(t *testing.T) {
	if _, err := AuditCommits(nil, []string{"W1"}, []string{"a"}, nil, "plan.md", nil); err == nil {
		t.Error("Audit(nil) returned no error")
	}
}

// TestE3WaveAndTaskNames: E3 uses the Where and name rules of waves and
// tasks, not the raw ids.
func TestE3WaveAndTaskNames(t *testing.T) {
	r := Check(md(`gate: x
waves:
  - id: 1W
    tasks:
      - {id: T1, title: a, owns: [a], depends: [T2], verify: v}
      - {id: T2, title: b, owns: [b], verify: v}
  - id: W2
    tasks:
      - {id: T3, title: c, owns: [c], verify: v}
`), opts())
	f := one(t, r, "E3", "T1", Error, "T1", "T2")
	if !strings.Contains(f.Msg, "wave 1") || strings.Contains(f.Msg, "1W") {
		t.Errorf("msg %q must name the wave by number", f.Msg)
	}

	// A task without an id: Where is the wave, the name is its position.
	r = Check(md(`gate: x
waves:
  - id: W1
    tasks:
      - {id: T1, title: a, owns: [a], verify: v}
      - {title: b, owns: [b], depends: [T1], verify: v}
`), opts())
	f = one(t, r, "E3", "W1", Error, "2 of wave W1", "T1")

	// A duplicate task id: Where is the wave, not the repeated id.
	r = Check(md(`gate: x
waves:
  - id: W1
    tasks:
      - {id: T1, title: a, owns: [a], verify: v}
      - {id: T1, title: b, owns: [b], depends: [T1], verify: v}
`), opts())
	one(t, r, "E3", "W1", Error, "T1")
}

// TestInvalidTaskIDNamedByPosition: like a wave, a task whose id cannot be
// used is named by its position in every finding but the one about the id.
func TestInvalidTaskIDNamedByPosition(t *testing.T) {
	r := Check(md(`gate: x
waves:
  - id: W1
    tasks:
      - {id: T1, title: a, owns: [a], verify: v}
      - {id: 2T, owns: ['/abs'], depends: [Nope]}
`), opts())
	for _, code := range []string{"E1", "W1", "W4"} {
		for _, f := range only(r, code) {
			if strings.Contains(f.Msg, "invalid task id") {
				continue
			}
			if !strings.Contains(f.Msg, "task 2 of wave W1") || strings.Contains(f.Msg, "2T") || f.Where != "W1" {
				t.Errorf("%s at %q: msg %q must name the task by position", code, f.Where, f.Msg)
			}
		}
	}
	var msgs []string
	for _, f := range only(r, "E1") {
		msgs = append(msgs, f.Msg)
	}
	for _, want := range []string{"invalid task id \"2T\"", "task 2 of wave W1 has no title", "task 2 of wave W1: invalid owns path", "task 2 of wave W1 depends on unknown task Nope"} {
		if !strings.Contains(strings.Join(msgs, "\n"), want) {
			t.Errorf("no E1 %q in %q", want, msgs)
		}
	}
}

// TestE1UnknownDependsOfIDLessTask: the depends of a task without an id are
// still checked, and the finding names the task by position.
func TestE1UnknownDependsOfIDLessTask(t *testing.T) {
	r := Check(md(`gate: x
waves:
  - id: W1
    tasks:
      - {id: T1, title: a, owns: [a], verify: v}
      - {title: b, owns: [b], depends: [Nope], verify: v}
`), opts())
	var hit bool
	for _, f := range only(r, "E1") {
		if strings.Contains(f.Msg, "unknown task") {
			hit = true
			if f.Where != "W1" || f.Msg != "task 2 of wave W1 depends on unknown task Nope" {
				t.Errorf("where %q msg %q", f.Where, f.Msg)
			}
		}
	}
	if !hit {
		t.Errorf("no unknown-task E1 in %v", r.Findings)
	}
}
