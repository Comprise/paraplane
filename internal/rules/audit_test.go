package rules

import (
	"reflect"
	"testing"

	"paraplane/internal/plan"
)

func auditPlan(t *testing.T) *plan.Plan {
	t.Helper()
	p, err := plan.Load(md(`gate: x
waves:
  - id: W1
    tasks:
      - {id: T1, title: a, owns: ["a/**"]}
      - {id: T2, title: b, owns: ["b/**"]}
  - id: W2
    tasks:
      - {id: T3, title: c, owns: ["c/**"], depends: [T1]}
`))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func findingTexts(fs []Finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.String())
	}
	return out
}

func TestAuditCommits(t *testing.T) {
	p := auditPlan(t)
	commits := []CommitFiles{
		{Hash: "abc1234", Tasks: []string{"T1"}, Files: []string{"a/x", "b/x", "docs/plan.md", "x.out"}},
		{Hash: "def5678", Waves: []string{"W1", "W2"}, Files: []string{"a/y", "c/y", "d/x"}},
		{Hash: "9999999", Waves: []string{"W9"}, Files: []string{"z"}},
		{Hash: "8888888", Tasks: []string{"T3"}, Files: []string{"a/z"}},
	}
	fs, err := AuditCommits(p, []string{"W1"}, []string{"a/w"}, commits, "docs/plan.md", []string{"*.out"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"error A1 W1: b/x changed by T1 (commit abc1234) but T1 does not own it",
		"error A1 W1: d/x changed in commit def5678 but no task of W1, W2 owns it",
	}
	if got := findingTexts(fs); !reflect.DeepEqual(got, want) {
		t.Errorf("W1:\n got %q\nwant %q", got, want)
	}

	fs, _ = AuditCommits(p, []string{"W2"}, nil, commits, "docs/plan.md", nil)
	want = []string{
		"error A1 W2: d/x changed in commit def5678 but no task of W1, W2 owns it",
		"error A1 W2: a/z changed by T3 (commit 8888888) but T3 does not own it",
	}
	if got := findingTexts(fs); !reflect.DeepEqual(got, want) {
		t.Errorf("W2:\n got %q\nwant %q", got, want)
	}

	// The working tree keeps the v1.1.2 texts; a stage lists its waves.
	fs, _ = AuditCommits(p, []string{"W1", "W2"}, []string{"z/q"}, nil, "docs/plan.md", nil)
	if got := findingTexts(fs); !reflect.DeepEqual(got, []string{"error A1 W1,W2: z/q changed but no task of W1, W2 owns it"}) {
		t.Errorf("stage working tree: %q", got)
	}
	// A file reported for one commit is not repeated.
	dup := []CommitFiles{{Hash: "abc1234", Tasks: []string{"T1", "T2"}, Files: []string{"c/q", "c/q"}}}
	fs, _ = AuditCommits(p, []string{"W1"}, nil, dup, "docs/plan.md", nil)
	if got := findingTexts(fs); !reflect.DeepEqual(got, []string{"error A1 W1: c/q changed by T1, T2 (commit abc1234) but none of them owns it"}) {
		t.Errorf("dup: %q", got)
	}
}
