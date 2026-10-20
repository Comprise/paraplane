package plugintest

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"paraplane/internal/plan"
	"paraplane/internal/rules"
)

func checkClean(t *testing.T, md []byte) {
	t.Helper()
	res := rules.Check(md, rules.Options{})
	for _, f := range res.Findings {
		if f.Severity == rules.Error {
			t.Errorf("%s", f)
		}
	}
	if res.Plan == nil {
		t.Errorf("no plan loaded")
	}
}

func TestRulesExamplePlan(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(root, "skills", "rules", "examples", "plan.md"))
	if err != nil {
		t.Fatal(err)
	}
	const fence = "````markdown\n"
	i := bytes.Index(data, []byte(fence))
	j := bytes.LastIndex(data, []byte("\n````"))
	if i < 0 || j <= i {
		t.Fatal("no four-backtick fence in the example")
	}
	inner := data[i+len(fence) : j+1]
	if _, err := plan.Extract(inner); err != nil {
		t.Fatalf("inner block: %v", err)
	}
	checkClean(t, inner)
}

func TestSpecExamplePlan(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(root, "docs", "superpowers", "specs", "2026-10-20-paraplane-design.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, rest, ok := bytes.Cut(data, []byte("## 4. Блок `paraplane`"))
	if !ok {
		t.Fatal("section 4 not found")
	}
	_, rest, ok = bytes.Cut(rest, []byte("```yaml\n"))
	if !ok {
		t.Fatal("no yaml block in section 4")
	}
	body, _, ok := bytes.Cut(rest, []byte("```"))
	if !ok {
		t.Fatal("yaml block not closed")
	}
	md := append(append([]byte("```paraplane\n"), body...), []byte("```\n")...)
	checkClean(t, md)
}
