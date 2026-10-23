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

// A binary that exists but lost its executable bit is reported as such, not
// as missing, and hooks still exit 0.
func TestShimBinaryNotExecutable(t *testing.T) {
	dir := plugin(t, `echo '{"ok":true}'`)
	name := filepath.Join(dir, "libexec", "paraplane-"+runtime.GOOS+"-"+runtime.GOARCH)
	if err := os.Chmod(name, 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := shim(t, dir, t.TempDir(), "hook", "session-start")
	if code != 0 || !strings.Contains(out, "is not executable") || strings.Contains(out, `"ok"`) {
		t.Errorf("session-start: code=%d out=%q", code, out)
	}
	if code, out, _ := shim(t, dir, t.TempDir(), "hook", "pre-tool-use"); code != 0 || out != "" {
		t.Errorf("other hook: code=%d out=%q", code, out)
	}
	if code, _, errs := shim(t, dir, t.TempDir(), "check", "x.md"); code != 1 || !strings.Contains(errs, "is not executable") {
		t.Errorf("CLI: code=%d stderr=%q", code, errs)
	}
}

// Hooks other than session-start pass a healthy answer through unchanged.
func TestShimOtherHooksPassOutput(t *testing.T) {
	dir := plugin(t, `echo '{"ok":true}'`)
	for _, h := range []string{"pre-tool-use", "post-tool-use", "permission-request", "prompt-submit", "stop"} {
		if code, out, _ := shim(t, dir, t.TempDir(), "hook", h); code != 0 || strings.TrimSpace(out) != `{"ok":true}` {
			t.Errorf("%s: code=%d out=%q", h, code, out)
		}
	}
}

// On a platform no binary is built for, the message names the platform and
// does not tell the user to reinstall (that cannot help); uname is the only
// external program the platform lookup needs.
func TestShimUnsupportedPlatformMessage(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("shim test needs a POSIX sh")
	}
	dir := plugin(t, "")
	fake := t.TempDir()
	write(t, fake, "uname", "#!/bin/sh\necho 'FreeBSD amd64'\n")
	if err := os.Chmod(filepath.Join(fake, "uname"), 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (int, string, string) {
		cmd := exec.Command("sh", append([]string{filepath.Join(dir, "bin", "paraplane")}, args...)...)
		cmd.Env = append(os.Environ(), "PATH="+fake+string(os.PathListSeparator)+os.Getenv("PATH"), "CLAUDE_PROJECT_DIR="+t.TempDir())
		cmd.Stdin = strings.NewReader("{}")
		var out, errb strings.Builder
		cmd.Stdout, cmd.Stderr = &out, &errb
		code := 0
		if err := cmd.Run(); err != nil {
			ee, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatal(err)
			}
			code = ee.ExitCode()
		}
		return code, out.String(), errb.String()
	}
	code, out, _ := run("hook", "session-start")
	low := strings.ToLower(out)
	if code != 0 || !strings.Contains(low, "freebsd/amd64") || !strings.Contains(low, "not built for this platform") || strings.Contains(low, "reinstall") {
		t.Errorf("session-start: code=%d out=%q", code, out)
	}
	code, _, errs := run("check", "x.md")
	low = strings.ToLower(errs)
	if code != 1 || !strings.Contains(low, "freebsd/amd64") || !strings.Contains(low, "not built for this platform") {
		t.Errorf("CLI: code=%d stderr=%q", code, errs)
	}
}
