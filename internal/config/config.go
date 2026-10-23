// Package config loads and validates the optional .paraplane/config.yaml and
// finds the project root.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"paraplane/internal/fsx"
	"paraplane/internal/pathset"
	"paraplane/internal/textx"

	"go.yaml.in/yaml/v3"
)

// FileName is the config file, relative to the project root.
const FileName = ".paraplane/config.yaml"

// MaxSize caps the size of the config file that is read.
const MaxSize = 1 << 20

const maxGateLen = 256

// Config is the effective configuration.
type Config struct {
	Enabled     bool
	Mode        string // "block" | "warn"
	PlanPaths   []string
	HotFiles    []string
	DefaultGate string
	MaxParallel int
	PlansDir    string
	AuditIgnore []string
	Progress    string // "wave" | "task": when the progress hook shows progress
}

// Default returns the defaults of spec section 9.
func Default() Config {
	return Config{
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
}

// Problems lists the settings of the file that were ignored. Load returns it
// as the error alongside a config that applies every valid setting.
type Problems []string

func (p Problems) Error() string { return FileName + ": ignored " + strings.Join(p, "; ") }

var knownKeys = []string{"enabled", "mode", "plan_paths", "hot_files", "default_gate", "max_parallel", "plans_dir", "audit_ignore", "progress"}

// Load reads root/.paraplane/config.yaml. A missing file gives Default() and
// nil. An unreadable, oversized or non-YAML file gives Default() and an error.
// Invalid settings are dropped (the default stays) and returned as Problems.
func Load(root string) (Config, error) {
	data, err := readLimited(filepath.Join(root, filepath.FromSlash(FileName)))
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Default(), err
	}
	var raw map[string]yaml.Node
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return Default(), err
	}
	cfg := Default()
	var pr Problems
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		n := raw[k]
		if !slices.Contains(knownKeys, k) {
			pr = append(pr, "unknown key "+safeKey(k))
			continue
		}
		if n.Kind == yaml.ScalarNode && n.Tag == "!!null" {
			continue // `key:` with no value keeps the default
		}
		if err := apply(&cfg, k, &n); err != nil {
			pr = append(pr, fmt.Sprintf("%s (%v)", k, err))
		}
	}
	if len(pr) > 0 {
		return cfg, pr
	}
	return cfg, nil
}

func readLimited(path string) ([]byte, error) {
	data, err := fsx.ReadRegular(path, MaxSize)
	if errors.Is(err, fsx.ErrTooLarge) {
		return nil, fmt.Errorf("%s: larger than %d bytes", FileName, MaxSize)
	}
	if errors.Is(err, fsx.ErrNotRegular) {
		return nil, fmt.Errorf("%s: %w", FileName, fsx.ErrNotRegular)
	}
	return data, err
}

// safeKey shows an unknown key only when it is a short lowercase word: the
// text can reach Claude's context and comes from a cloned repository.
func safeKey(k string) string {
	if k != "" && len(k) <= 32 && strings.Trim(k, "abcdefghijklmnopqrstuvwxyz_") == "" {
		return k
	}
	return "(name not shown)"
}

func apply(cfg *Config, key string, n *yaml.Node) error {
	switch key {
	case "enabled":
		var v bool
		if err := n.Decode(&v); err != nil {
			return errors.New("must be true or false")
		}
		cfg.Enabled = v
	case "mode":
		var v string
		if err := n.Decode(&v); err != nil || (v != "block" && v != "warn") {
			return errors.New(`must be "block" or "warn"`)
		}
		cfg.Mode = v
	case "progress":
		var v string
		if err := n.Decode(&v); err != nil || (v != "wave" && v != "task") {
			return errors.New(`must be "wave" or "task"`)
		}
		cfg.Progress = v
	case "max_parallel":
		var v int
		if err := n.Decode(&v); err != nil || v < 1 || v > 32 {
			return errors.New("must be an integer 1..32")
		}
		cfg.MaxParallel = v
	case "default_gate":
		var v string
		if err := n.Decode(&v); err != nil || len(v) > maxGateLen {
			return fmt.Errorf("must be a single-line string up to %d bytes", maxGateLen)
		}
		if textx.HasControl(v) {
			return errors.New("must not contain control, line separator or invisible format characters")
		}
		cfg.DefaultGate = v
	case "plans_dir":
		var v string
		if err := n.Decode(&v); err != nil {
			return errors.New("must be a string")
		}
		if strings.ContainsAny(v, "*?[]{}") {
			return errors.New("must not contain glob characters")
		}
		c, err := pathset.Clean(v)
		if err != nil {
			return errors.New("invalid path")
		}
		cfg.PlansDir = c
	case "plan_paths":
		return applyList(&cfg.PlanPaths, n)
	case "hot_files":
		return applyList(&cfg.HotFiles, n)
	case "audit_ignore":
		return applyList(&cfg.AuditIgnore, n)
	}
	return nil
}

// applyList sets dst to the cleaned patterns, or leaves it untouched when any
// entry is invalid. Entry values are not quoted in the error.
func applyList(dst *[]string, n *yaml.Node) error {
	var in []string
	if err := n.Decode(&in); err != nil {
		return errors.New("must be a list of strings")
	}
	out := make([]string, 0, len(in))
	for i, p := range in {
		c, err := pathset.Clean(p)
		if err != nil {
			return fmt.Errorf("entry %d is not a valid relative path pattern", i+1)
		}
		out = append(out, c)
	}
	*dst = out
	return nil
}

// FindRoot walks up from dir to the nearest directory holding .git or
// FileName; without a marker it returns dir. It never climbs into $HOME from
// below it, so a stray ~/.git does not make the whole home the project.
func FindRoot(dir string) string {
	home, _ := os.UserHomeDir()
	cur := dir
	for {
		if home != "" && cur == home && cur != dir {
			return dir
		}
		for _, m := range []string{".git", filepath.FromSlash(FileName)} {
			if _, err := os.Stat(filepath.Join(cur, m)); err == nil {
				return cur
			}
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return dir
		}
		cur = parent
	}
}
