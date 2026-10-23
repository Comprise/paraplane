package plugintest

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A CRLF checkout breaks the shim (#!/bin/sh\r never runs), so the repo pins
// LF for it explicitly.
func TestGitattributesPinLF(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || f[0] != "bin/paraplane" {
			continue
		}
		for _, a := range f[1:] {
			if a == "eol=lf" {
				found = true
			}
		}
	}
	if !found {
		t.Error(".gitattributes does not set eol=lf for bin/paraplane")
	}
}

// shipList reads ship.txt, the one list of what the release ships.
func shipList(t *testing.T) []string {
	t.Helper()
	f, err := os.Open(filepath.Join(root, "ship.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var paths []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if p := strings.TrimSpace(sc.Text()); p != "" {
			paths = append(paths, p)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return paths
}

func TestShipListExists(t *testing.T) {
	paths := shipList(t)
	if len(paths) == 0 {
		t.Fatal("ship.txt is empty")
	}
	seen := map[string]bool{}
	for _, p := range paths {
		if seen[p] {
			t.Errorf("ship.txt lists %s twice", p)
		}
		seen[p] = true
		if strings.ContainsAny(p, " \t") || strings.HasPrefix(p, "/") || strings.Contains(p, "..") {
			t.Errorf("ship.txt: %q is not a plain relative path", p)
		}
		if p == "libexec" {
			continue // built by make dist or the release, not committed
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(p))); err != nil {
			t.Errorf("ship.txt: %v", err)
		}
	}
	for _, must := range []string{".claude-plugin", "bin", "hooks", "libexec", ".gitattributes"} {
		if !seen[must] {
			t.Errorf("ship.txt does not list %s", must)
		}
	}
}

// Every README.md has its Russian pair and the other way round.
func TestReadmePairs(t *testing.T) {
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		dir := filepath.Dir(p)
		pair := ""
		switch d.Name() {
		case "README.md":
			pair = "README.ru.md"
		case "README.ru.md":
			pair = "README.md"
		default:
			return nil
		}
		if _, err := os.Stat(filepath.Join(dir, pair)); err != nil {
			rel, _ := filepath.Rel(root, p)
			t.Errorf("%s has no %s next to it", filepath.ToSlash(rel), pair)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Without a binary, enabled: false is found in the nearest dir up from the
// project dir with .git or .paraplane/config.yaml, as the binary finds it.
func TestShimDisabledWalksUp(t *testing.T) {
	dir := plugin(t, "")

	cfgRoot := t.TempDir()
	write(t, cfgRoot, ".paraplane/config.yaml", "enabled: false\n")
	write(t, cfgRoot, "sub/deep/x.txt", "")
	if code, out, _ := shim(t, dir, filepath.Join(cfgRoot, "sub", "deep"), "hook", "session-start"); code != 0 || out != "" {
		t.Errorf("config above: code=%d out=%q", code, out)
	}

	// .git is the nearer marker and has no config: the plugin is on, even
	// though a dir further up disables it.
	outer := t.TempDir()
	write(t, outer, ".paraplane/config.yaml", "enabled: false\n")
	write(t, outer, "repo/.git/HEAD", "ref: refs/heads/main\n")
	write(t, outer, "repo/sub/x.txt", "")
	if code, out, _ := shim(t, dir, filepath.Join(outer, "repo", "sub"), "hook", "session-start"); code != 0 || !strings.Contains(out, `"systemMessage"`) {
		t.Errorf("git root without config: code=%d out=%q", code, out)
	}
}

// Every hook is buffered: a crash prints nothing, a clean exit passes the
// answer through.
func TestShimHooksBuffered(t *testing.T) {
	crash := plugin(t, `echo '{"hookSpecificOutput":{"permissionDecision":"de'; exit 2`)
	for _, h := range []string{"pre-tool-use", "post-tool-use"} {
		if code, out, _ := shim(t, crash, t.TempDir(), "hook", h); code != 0 || out != "" {
			t.Errorf("%s crash: code=%d out=%q", h, code, out)
		}
	}
	ok := plugin(t, `echo '{"ok":true}'`)
	if code, out, _ := shim(t, ok, t.TempDir(), "hook", "pre-tool-use"); code != 0 || strings.TrimSpace(out) != `{"ok":true}` {
		t.Errorf("pre-tool-use ok: code=%d out=%q", code, out)
	}
}

// The workflows take what ships from ship.txt (directly or through make tree,
// which reads it) and stamp the release with the tag minus its leading v.
func TestWorkflowsUseShipListAndTagVersion(t *testing.T) {
	read := func(rel string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		return strings.ReplaceAll(string(data), "\r\n", "\n")
	}
	mk := read("Makefile")
	if !strings.Contains(mk, "< ship.txt") {
		t.Error("Makefile: make tree does not read ship.txt")
	}
	ci := read(".github/workflows/ci.yml")
	if !strings.Contains(ci, "make tree") {
		t.Error("ci.yml does not build the shipped tree with make tree (which reads ship.txt)")
	}
	rel := read(".github/workflows/release.yml")
	if n := strings.Count(rel, "$(cat ship.txt)"); n < 2 {
		t.Errorf("release.yml reads ship.txt %d time(s), want the archive and the release branch both from it", n)
	}
	for _, want := range []string{
		`-X main.version=${GITHUB_REF_NAME#v}`,
		`--arg v "${GITHUB_REF_NAME#v}" '.version=$v' .claude-plugin/plugin.json`,
		`--arg v "${GITHUB_REF_NAME#v}" '.version=$v' .codex-plugin/plugin.json`,
	} {
		if !strings.Contains(rel, want) {
			t.Errorf("release.yml does not contain %s", want)
		}
	}
}
