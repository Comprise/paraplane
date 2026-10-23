package plugintest

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type workflowFile struct {
	Jobs map[string]struct {
		Uses           string `yaml:"uses"`
		TimeoutMinutes any    `yaml:"timeout-minutes"`
		Steps          []struct {
			Uses string `yaml:"uses"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

var pinnedUses = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_./-]+@[0-9a-f]{40}$`)

// Every action is pinned to a full commit SHA (a tag can be moved) and every
// job has a timeout (the default is 360 minutes of a stuck runner).
func TestWorkflowsPinned(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.yml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no workflows found: %v", err)
	}
	for _, f := range files {
		name := filepath.Base(f)
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var wf workflowFile
		if err := yaml.Unmarshal(data, &wf); err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if len(wf.Jobs) == 0 {
			t.Errorf("%s: no jobs", name)
		}
		for id, job := range wf.Jobs {
			if job.Uses != "" {
				// A reusable workflow call: local, and timeouts live in the callee.
				if !strings.HasPrefix(job.Uses, "./") {
					t.Errorf("%s: job %s calls %s, want a local workflow", name, id, job.Uses)
				}
			} else if job.TimeoutMinutes == nil {
				t.Errorf("%s: job %s has no timeout-minutes", name, id)
			}
			for _, s := range job.Steps {
				if s.Uses == "" || strings.HasPrefix(s.Uses, "./") {
					continue
				}
				if !pinnedUses.MatchString(s.Uses) {
					t.Errorf("%s: job %s: %q is not pinned to a 40-hex commit SHA", name, id, s.Uses)
				}
			}
		}
	}
}
