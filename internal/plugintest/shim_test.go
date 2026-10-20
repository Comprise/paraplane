package plugintest

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// plugin lays out a plugin dir with the real shim and, if script is not
// empty, a fake binary for this platform running script.
func plugin(t *testing.T, script string) string {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("shim test needs a POSIX sh")
	}
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		t.Skip("unsupported arch for the shim")
	}
	dir := t.TempDir()
	shimSrc, err := os.ReadFile(filepath.Join(root, "bin", "paraplane"))
	if err != nil {
		t.Fatal(err)
	}
	write(t, dir, "bin/paraplane", string(shimSrc))
	if script != "" {
		name := "libexec/paraplane-" + runtime.GOOS + "-" + runtime.GOARCH
		write(t, dir, name, "#!/bin/sh\n"+script+"\n")
		if err := os.Chmod(filepath.Join(dir, filepath.FromSlash(name)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func shim(t *testing.T, dir, project string, args ...string) (int, string, string) {
	t.Helper()
	cmd := exec.Command("sh", append([]string{filepath.Join(dir, "bin", "paraplane")}, args...)...)
	cmd.Env = append(os.Environ(), "CLAUDE_PROJECT_DIR="+project)
	cmd.Stdin = strings.NewReader("{}")
	var out, errb strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return code, out.String(), errb.String()
}

func TestShimWithoutBinary(t *testing.T) {
	dir := plugin(t, "")
	enabled, disabled := t.TempDir(), t.TempDir()
	write(t, enabled, ".paraplane/config.yaml", "enabled: true\n")
	write(t, disabled, ".paraplane/config.yaml", "enabled: false\n")

	code, out, _ := shim(t, dir, t.TempDir(), "hook", "session-start")
	if code != 0 || !strings.Contains(out, `"systemMessage":"paraplane: the binary`) {
		t.Errorf("session-start: code=%d out=%q", code, out)
	}
	code, out, _ = shim(t, dir, enabled, "hook", "session-start")
	if code != 0 || !strings.Contains(out, `"systemMessage"`) {
		t.Errorf("enabled session-start: code=%d out=%q", code, out)
	}
	if code, out, _ := shim(t, dir, disabled, "hook", "session-start"); code != 0 || out != "" {
		t.Errorf("disabled session-start: code=%d out=%q", code, out)
	}
	if code, out, _ := shim(t, dir, t.TempDir(), "hook", "bash"); code != 0 || out != "" {
		t.Errorf("other hook: code=%d out=%q", code, out)
	}
	if code, _, errs := shim(t, dir, t.TempDir(), "check", "x.md"); code != 1 || !strings.Contains(errs, "was not found") {
		t.Errorf("CLI: code=%d stderr=%q", code, errs)
	}
}

// A crashing binary (a Go fatal error exits 2) never fails a hook; a CLI call
// passes the exit code through.
func TestShimHookCrashExitsZero(t *testing.T) {
	dir := plugin(t, `exit 2`)
	if code, _, _ := shim(t, dir, t.TempDir(), "hook", "bash"); code != 0 {
		t.Errorf("hook exit = %d, want 0", code)
	}
	if code, _, _ := shim(t, dir, t.TempDir(), "check", "x.md"); code != 2 {
		t.Errorf("CLI exit = %d, want 2 passed through", code)
	}
}

// A crashed session-start prints nothing, not its partial answer.
func TestShimSessionStartCrashSilent(t *testing.T) {
	dir := plugin(t, `echo '{"partial'; exit 1`)
	code, out, _ := shim(t, dir, t.TempDir(), "hook", "session-start")
	if code != 0 || out != "" {
		t.Errorf("code=%d out=%q", code, out)
	}
}

// A healthy session-start answer is passed through.
func TestShimSessionStartPassesOutput(t *testing.T) {
	dir := plugin(t, `echo '{"ok":true}'`)
	code, out, _ := shim(t, dir, t.TempDir(), "hook", "session-start")
	if code != 0 || strings.TrimSpace(out) != `{"ok":true}` {
		t.Errorf("code=%d out=%q", code, out)
	}
}
