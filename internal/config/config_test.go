package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"paraplane/internal/fsx"
	"paraplane/internal/pathset"
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
		Progress:    "wave",
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

func TestLoadProgress(t *testing.T) {
	for body, want := range map[string]string{
		"progress: task\n": "task",
		"progress: wave\n": "wave",
	} {
		root := t.TempDir()
		write(t, root, body)
		if cfg, err := Load(root); err != nil || cfg.Progress != want {
			t.Errorf("%q: got %q, %v; want %q", body, cfg.Progress, err, want)
		}
	}
	for _, body := range []string{"progress: daily\nmax_parallel: 3\n", "progress: [x]\nmax_parallel: 3\n"} {
		root := t.TempDir()
		write(t, root, body)
		cfg, err := Load(root)
		var pr Problems
		if !errors.As(err, &pr) || len(pr) != 1 || !strings.Contains(err.Error(), "progress") {
			t.Errorf("%q: want one progress problem, got %v", body, err)
		}
		if cfg.Progress != "wave" || cfg.MaxParallel != 3 {
			t.Errorf("%q: got progress %q, max_parallel %d", body, cfg.Progress, cfg.MaxParallel)
		}
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
	if cfg, err = Load(root); err == nil || !strings.Contains(err.Error(), "larger than") || !reflect.DeepEqual(cfg, Default()) {
		t.Fatalf("oversize: %+v, %v", cfg, err)
	}
}

func TestFindRoot(t *testing.T) {
	base := t.TempDir()
	t.Setenv("HOME", base) // FindRoot stops at HOME, so ancestors of base are never inspected
	t.Setenv("USERPROFILE", base)
	root := filepath.Join(base, "proj")
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
	root2 := filepath.Join(base, "proj2")
	sub2 := filepath.Join(root2, "x")
	if err := os.MkdirAll(filepath.Join(root2, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sub2, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := FindRoot(sub2); got != root2 {
		t.Fatalf(".git marker: got %s", got)
	}
}

func TestLoadEveryKey(t *testing.T) {
	cases := []struct {
		name, body string
		check      func(Config) bool
	}{
		{"enabled", "enabled: false\n", func(c Config) bool { return !c.Enabled }},
		{"mode", "mode: warn\n", func(c Config) bool { return c.Mode == "warn" }},
		{"plan_paths", "plan_paths: [\"p/**/*.md\"]\n", func(c Config) bool { return reflect.DeepEqual(c.PlanPaths, []string{"p/**/*.md"}) }},
		{"hot_files", "hot_files: [\"x.lock\"]\n", func(c Config) bool { return reflect.DeepEqual(c.HotFiles, []string{"x.lock"}) }},
		{"default_gate", "default_gate: go test ./...\n", func(c Config) bool { return c.DefaultGate == "go test ./..." }},
		{"max_parallel", "max_parallel: 3\n", func(c Config) bool { return c.MaxParallel == 3 }},
		{"plans_dir", "plans_dir: my/plans\n", func(c Config) bool { return c.PlansDir == "my/plans" }},
		{"audit_ignore", "audit_ignore: [\"gen/**\"]\n", func(c Config) bool { return reflect.DeepEqual(c.AuditIgnore, []string{"gen/**"}) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			write(t, root, tc.body)
			cfg, err := Load(root)
			if err != nil || !tc.check(cfg) {
				t.Fatalf("got %+v, %v", cfg, err)
			}
		})
	}
}

func TestLoadMaxParallelBounds(t *testing.T) {
	for _, tc := range []struct {
		v    string
		ok   bool
		want int
	}{{"0", false, 6}, {"1", true, 1}, {"32", true, 32}, {"33", false, 6}} {
		root := t.TempDir()
		write(t, root, "max_parallel: "+tc.v+"\n")
		cfg, err := Load(root)
		var pr Problems
		if tc.ok != (err == nil) || (!tc.ok && !errors.As(err, &pr)) || cfg.MaxParallel != tc.want {
			t.Errorf("%s: got %d, %v", tc.v, cfg.MaxParallel, err)
		}
	}
}

func TestLoadInvalidValues(t *testing.T) {
	for name, body := range map[string]string{
		"gate newline": "default_gate: \"a\\nb\"\n",
		"plans_dir ..": "plans_dir: ..\n",
		"gate NUL":     "default_gate: \"a\\0b\"\n",
		"gate tab":     "default_gate: \"a\\tb\"\n",
		"gate ESC":     "default_gate: \"a\\eb\"\n",
		"gate DEL":     "default_gate: \"a\\x7fb\"\n",
		"gate U+2028":  "default_gate: \"a\\u2028b\"\n",
		"gate U+2029":  "default_gate: \"a\\u2029b\"\n",
		"gate U+202E":  "default_gate: \"a\u202eb\"\n",
		"gate U+200B":  "default_gate: \"a\u200bb\"\n",
		"gate U+FEFF":  "default_gate: \"a\ufeffb\"\n",
	} {
		root := t.TempDir()
		write(t, root, body)
		cfg, err := Load(root)
		var pr Problems
		if !errors.As(err, &pr) || len(pr) != 1 || !reflect.DeepEqual(cfg, Default()) {
			t.Errorf("%s: got %+v, %v", name, cfg, err)
		}
	}
}

// TestLoadPlansDirGlobChars checks the plans_dir glob-character rule by its
// message. Values marked valid are path patterns pathset.Clean accepts, so
// only that rule rejects them; a lone '{' or '}' is also an invalid glob, and
// the message shows the glob rule still answers first.
func TestLoadPlansDirGlobChars(t *testing.T) {
	for _, tc := range []struct {
		v     string
		valid bool
	}{
		{"}", false}, {"a}b", false}, {"{", false}, {"a{b", false},
		{"{a,b}", true}, {"a{b}", true}, {"]", true}, {"a]b", true}, {"[ab]", true},
		{"*", true}, {"a/*", true}, {"?", true}, {"a?", true},
	} {
		if _, err := pathset.Clean(tc.v); (err == nil) != tc.valid {
			t.Fatalf("%q: pathset.Clean error %v, want valid=%v", tc.v, err, tc.valid)
		}
		root := t.TempDir()
		write(t, root, "plans_dir: '"+tc.v+"'\n")
		cfg, err := Load(root)
		var pr Problems
		if !errors.As(err, &pr) || len(pr) != 1 || !strings.Contains(pr[0], "glob characters") || !reflect.DeepEqual(cfg, Default()) {
			t.Errorf("%q: got %+v, %v", tc.v, cfg, err)
		}
	}
}

func TestLoadGateMessages(t *testing.T) {
	for body, want := range map[string]string{
		"default_gate: \"a\\tb\"\n":                                   "control",
		"default_gate: \"a\\u202eb\"\n":                               "format characters",
		"default_gate: '" + strings.Repeat("x", maxGateLen+1) + "'\n": "up to",
	} {
		root := t.TempDir()
		write(t, root, body)
		_, err := Load(root)
		var pr Problems
		if !errors.As(err, &pr) || len(pr) != 1 || !strings.Contains(pr[0], want) {
			t.Errorf("%q: got %v, want %q", body, err, want)
		}
	}
}

func TestLoadNullKeepsDefault(t *testing.T) {
	root := t.TempDir()
	write(t, root, "mode: null\nmax_parallel:\nplan_paths: ~\n")
	cfg, err := Load(root)
	if err != nil || !reflect.DeepEqual(cfg, Default()) {
		t.Fatalf("got %+v, %v", cfg, err)
	}
}

func TestLoadSpecialFileErrors(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, filepath.FromSlash(FileName))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := mkfifo(p); err != nil {
		t.Skip("mkfifo:", err)
	}
	type res struct {
		cfg Config
		err error
	}
	ch := make(chan res, 1)
	go func() { c, e := Load(root); ch <- res{c, e} }()
	select {
	case r := <-ch:
		if r.err == nil || !reflect.DeepEqual(r.cfg, Default()) {
			t.Fatalf("fifo: %+v, %v", r.cfg, r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Load blocked on FIFO")
	}
}

func TestLoadSymlinkAndDirErrors(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, filepath.FromSlash(FileName))
	if err := os.MkdirAll(p, 0o755); err != nil { // directory as config
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil {
		t.Fatal("dir config: want error")
	}
	if runtime.GOOS == "windows" {
		return
	}
	root = t.TempDir()
	write(t, root, "enabled: false\n")
	target := filepath.Join(root, "real.yaml")
	if err := os.WriteFile(target, []byte("enabled: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p = filepath.Join(root, filepath.FromSlash(FileName))
	os.Remove(p)
	if err := os.Symlink(target, p); err != nil {
		t.Skip("symlink:", err)
	}
	if cfg, err := Load(root); !errors.Is(err, fsx.ErrNotRegular) || !cfg.Enabled {
		t.Fatalf("symlink config: %+v, %v", cfg, err)
	}
}
