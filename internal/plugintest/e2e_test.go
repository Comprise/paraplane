package plugintest

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// copyTree copies src to dst, keeping file modes.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	info, err := os.Stat(src)
	if err != nil {
		t.Fatal(err)
	}
	if info.IsDir() {
		if err := os.MkdirAll(dst, 0o755); err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			copyTree(t, filepath.Join(src, e.Name()), filepath.Join(dst, e.Name()))
		}
		return
	}
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm()|0o200)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

// hookCommands reads every command of a hooks file: the event it answers, and
// the arguments after the shim path.
func hookCommands(t *testing.T, rel string) (events []string, args [][]string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
	const shimPath = `"${CLAUDE_PLUGIN_ROOT}"/bin/paraplane `
	for event, groups := range f.Hooks {
		for _, g := range groups {
			for _, h := range g.Hooks {
				rest, ok := strings.CutPrefix(h.Command, shimPath)
				if !ok {
					t.Errorf("%s: command %q does not start with %s", rel, h.Command, shimPath)
					continue
				}
				events = append(events, event)
				args = append(args, strings.Fields(rest))
			}
		}
	}
	return events, args
}

// Every hook command of hooks.json and codex.json runs through the shim and a
// freshly built binary of the shipped tree with a minimal payload: exit 0 and
// an answer that is empty or valid JSON.
func TestE2EHooksThroughShim(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("needs a POSIX sh")
	}
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		t.Skip("unsupported arch for the shim")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("needs the go tool to build the binary")
	}
	tree := t.TempDir()
	for _, p := range shipList(t) {
		if p == "libexec" {
			continue
		}
		copyTree(t, filepath.Join(root, filepath.FromSlash(p)), filepath.Join(tree, filepath.FromSlash(p)))
	}
	binPath := filepath.Join(tree, "libexec", "paraplane-"+runtime.GOOS+"-"+runtime.GOARCH)
	if err := os.MkdirAll(filepath.Dir(binPath), 0o755); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", binPath, "./cmd/paraplane")
	build.Dir = filepath.Join(root)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	project := t.TempDir()
	if out, err := exec.Command("git", "-C", project, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init: %v\n%s", err, out)
	}

	answers := 0
	for _, file := range []string{"hooks/hooks.json", "hooks/codex.json"} {
		events, argv := hookCommands(t, file)
		if len(events) == 0 {
			t.Fatalf("%s: no hook commands", file)
		}
		for i, event := range events {
			payload, err := json.Marshal(map[string]any{
				"hook_event_name": event,
				"session_id":      "e2e",
				"cwd":             project,
				"tool_name":       "Bash",
				"tool_input":      map[string]any{"command": "paraplane version"},
			})
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("sh", append([]string{filepath.Join(tree, "bin", "paraplane")}, argv[i]...)...)
			cmd.Dir = project
			cmd.Env = append(os.Environ(), "CLAUDE_PROJECT_DIR="+project, "CLAUDE_PLUGIN_ROOT="+tree)
			cmd.Stdin = strings.NewReader(string(payload))
			var stdout, stderr strings.Builder
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			// The shim hides a crashed hook binary (exit 0, no output), so the
			// same argv also runs against the binary directly: rc 0, no stderr.
			direct := exec.Command(binPath, argv[i]...)
			direct.Dir = project
			direct.Env = cmd.Env
			direct.Stdin = strings.NewReader(string(payload))
			var dout, derr strings.Builder
			direct.Stdout, direct.Stderr = &dout, &derr
			if err := direct.Run(); err != nil || derr.Len() != 0 {
				t.Errorf("%s %s: direct binary: %v (stderr %q)", file, strings.Join(argv[i], " "), err, derr.String())
			}
			if err := cmd.Run(); err != nil {
				t.Errorf("%s %s: %v (stderr %q)", file, strings.Join(argv[i], " "), err, stderr.String())
				continue
			}
			if out := strings.TrimSpace(stdout.String()); out != "" {
				if !json.Valid([]byte(out)) {
					t.Errorf("%s %s: answer is not JSON: %q", file, strings.Join(argv[i], " "), out)
				}
				answers++
			}
		}
	}
	// A run that crashed everywhere would look like silence; at least one hook
	// (session-start of the shipped tree) must have produced a real answer.
	if answers == 0 {
		t.Error("no hook produced a JSON answer")
	}
}
