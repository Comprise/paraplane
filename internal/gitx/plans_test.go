package gitx

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPlansSince(t *testing.T) {
	needGit(t, 2, 24, "--end-of-options")
	dir := initRepo(t)
	write(t, filepath.Join(dir, "f"), "0")
	commitAll(t, dir, "base")
	base, _ := Head(dir)
	msgs := []string{
		"a\n\nParaplane-Plan: docs/a.md\nParaplane-Wave: W1\n",
		"plain",
		"b\n\nParaplane-Plan: docs/b.md\nParaplane-Task: T1\n",
		"a again\n\nParaplane-Plan: docs/a.md\nParaplane-Task: T2\n",
	}
	for i, m := range msgs {
		write(t, filepath.Join(dir, "f"), strings.Repeat("x", i+1))
		commitAll(t, dir, m)
	}
	head, _ := Head(dir)
	got, err := PlansSince(dir, base, head)
	if err != nil || !reflect.DeepEqual(got, []string{"docs/a.md", "docs/b.md"}) {
		t.Errorf("since base: %v %v", got, err)
	}
	// Without a start only HEAD is read.
	got, err = PlansSince(dir, "", head)
	if err != nil || !reflect.DeepEqual(got, []string{"docs/a.md"}) {
		t.Errorf("HEAD only: %v %v", got, err)
	}
	write(t, filepath.Join(dir, "f"), "plain")
	commitAll(t, dir, "plain after")
	plain, _ := Head(dir)
	if got, err := PlansSince(dir, "", plain); err != nil || len(got) != 0 {
		t.Errorf("plain HEAD: %v %v", got, err)
	}
	if got, err := PlansSince(dir, head, head); err != nil || len(got) != 0 {
		t.Errorf("no new commits: %v %v", got, err)
	}
}
