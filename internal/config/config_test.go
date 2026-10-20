package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func write(t *testing.T, root, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(FileName))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDefault(t *testing.T) {
	want := Config{
		Enabled:   true,
		Mode:      "block",
		PlanPaths: []string{"docs/plans/**/*.md", "docs/superpowers/plans/**/*.md", ".paraplane/plans/**/*.md"},
		HotFiles: []string{"go.mod", "go.sum", "**/package.json", "**/package-lock.json", "**/pnpm-lock.yaml",
			"**/yarn.lock", "**/Cargo.toml", "**/Cargo.lock", "**/pyproject.toml", "**/uv.lock",
			"**/poetry.lock", "**/migrations/**"},
		DefaultGate: "",
		MaxParallel: 6,
		PlansDir:    "docs/plans",
		AuditIgnore: []string{},
	}
	if got := Default(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Default() = %+v, want %+v", got, want)
	}
}

func TestLoadMissing(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil || !reflect.DeepEqual(cfg, Default()) {
		t.Fatalf("got %+v, %v", cfg, err)
	}
}

func TestLoadPartial(t *testing.T) {
	root := t.TempDir()
	write(t, root, "enabled: false\nmode: loud\nmax_parallel: 99\ncolour: x\naudit_ignore:\n  - ../x\nhot_files: [\"a.lock\"]\n")
	cfg, err := Load(root)
	var pr Problems
	if !errors.As(err, &pr) || len(pr) != 4 {
		t.Fatalf("want 4 problems, got %v", err)
	}
	for _, k := range []string{"mode", "max_parallel", "colour", "audit_ignore"} {
		if !strings.Contains(err.Error(), k) {
			t.Errorf("no %q in %v", k, err)
		}
	}
	if !strings.HasPrefix(err.Error(), FileName+": ignored ") {
		t.Errorf("bad message %q", err)
	}
	d := Default()
	if cfg.Enabled || cfg.Mode != d.Mode || cfg.MaxParallel != d.MaxParallel ||
		!reflect.DeepEqual(cfg.AuditIgnore, d.AuditIgnore) || !reflect.DeepEqual(cfg.HotFiles, []string{"a.lock"}) {
		t.Fatalf("unexpected %+v", cfg)
	}
}

func TestLoadBroken(t *testing.T) {
	root := t.TempDir()
	write(t, root, ": :")
	cfg, err := Load(root)
	var pr Problems
	if err == nil || errors.As(err, &pr) || !reflect.DeepEqual(cfg, Default()) {
		t.Fatalf("got %+v, %v", cfg, err)
	}
	write(t, root, "default_gate: "+strings.Repeat("x", MaxSize))
	if cfg, err = Load(root); err == nil || !reflect.DeepEqual(cfg, Default()) {
		t.Fatalf("oversize: %+v, %v", cfg, err)
	}
}

func TestFindRoot(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := FindRoot(sub); got != sub {
		t.Fatalf("no marker: got %s", got)
	}
	write(t, root, "enabled: true\n")
	if got := FindRoot(sub); got != root {
		t.Fatalf("config marker: got %s", got)
	}
	root2 := t.TempDir()
	sub2 := filepath.Join(root2, "x")
	os.MkdirAll(filepath.Join(root2, ".git"), 0o755)
	os.MkdirAll(sub2, 0o755)
	if got := FindRoot(sub2); got != root2 {
		t.Fatalf(".git marker: got %s", got)
	}
}
