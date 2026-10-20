package hooks

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"paraplane/internal/fsx"
	"paraplane/internal/layout"
	"paraplane/internal/pathset"
	"paraplane/internal/rules"
	"paraplane/internal/textx"
)

// checkTimeout bounds the plan check in a hook (spec section 4).
const checkTimeout = 5 * time.Second

// maxFindings is how many findings a hook lists before "… N more".
const maxFindings = 20

// Text limits for values that come from the plan or the file system.
const (
	maxLine   = 1000     // runes in one finding line
	maxPath   = 300      // runes in the plan path or a config error
	maxE0     = 500      // runes in the E0 message
	maxLayout = 64 << 10 // bytes of the shorter layout
)

// longLayout replaces a shorter layout that is longer than maxLayout.
const longLayout = "The shorter layout is too long to show here: run `paraplane check --suggest <file>` for the shorter layout."

// postNow is the clock of Options; tests move it.
var postNow = time.Now

// Options builds the rules options from env.Config: W5 looks at the file
// system under env.Root, and the check must finish within 5 s.
func Options(env Env) rules.Options {
	root := env.Root
	return rules.Options{
		HotFiles:    env.Config.HotFiles,
		DefaultGate: env.Config.DefaultGate,
		MaxParallel: env.Config.MaxParallel,
		IsDir: func(rel string) bool {
			fi, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
			return err == nil && fi.IsDir()
		},
		Deadline: postNow().Add(checkTimeout),
	}
}

// planFindings lists the findings one per line, at most maxFindings, then
// "… N more". Finding lines carry plan text, so only W3, whose text is all
// paraplane's own (it names the CLI), is adapted to the agent.
func planFindings(a Agent, fs []rules.Finding) string {
	var b strings.Builder
	for i, f := range fs {
		if i == maxFindings {
			fmt.Fprintf(&b, "\n… %d more", len(fs)-i)
			break
		}
		if i > 0 {
			b.WriteByte('\n')
		}
		line := textx.Line(f.String(), maxLine)
		if f.Code == "W3" {
			line = a.text(line)
		}
		b.WriteString(line)
	}
	return b.String()
}

// planWarnings is the text for a result with warnings only; W3 adds the
// shorter layout, or a pointer to `paraplane check --suggest` when the
// layout is longer than maxLayout (a cut block would not be valid YAML).
// Only paraplane's own strings are adapted to the agent, never the layout.
func planWarnings(a Agent, r rules.Result) string {
	s := "paraplane: plan warnings (not blocking):\n" + planFindings(a, r.Findings)
	if r.Suggested != nil && r.Plan != nil {
		if l := textx.Block(layout.Render(r.Plan, r.Suggested), math.MaxInt); len(l) <= maxLayout {
			s += "\nShorter layout:\n" + l
		} else {
			s += "\n" + a.text(longLayout)
		}
	}
	return s
}

// readPlan reads the regular file name of at most fsx.MaxFile bytes; ok is
// false when there is no such file, or it is a symlink, a FIFO, a device, a
// directory or too large (fsx never opens those, so the hook cannot block).
func readPlan(name string) (data []byte, ok bool, err error) {
	data, err = fsx.ReadRegular(name, fsx.MaxFile)
	switch {
	case err == nil:
		return data, true, nil
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, fsx.ErrNotRegular), errors.Is(err, fsx.ErrTooLarge):
		return nil, false, nil
	}
	return nil, false, err
}

// contextOutput is a response that only adds context for the agent.
func contextOutput(event, text string) ([]byte, error) {
	return encodeJSON(hookOutput{Hook: &hookSpecific{EventName: event, AdditionalContext: text}})
}

// rootRel turns a file path into a path relative to root with "/"
// separators; a backslash counts as a separator on every platform. A
// relative path is taken relative to root. ok is false outside root. An
// absolute path outside root is compared again with symlinks resolved in
// root and in the path's directory (macOS: /var is /private/var); its last
// element is not resolved, so a symlinked plan file stays a symlink.
func rootRel(root, fp string) (string, bool) {
	if rel, ok := rootRelLexical(root, fp); ok || fp == "" || root == "" || !slashAbs(anySlash(fp)) {
		return rel, ok
	}
	r, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", false
	}
	p := filepath.FromSlash(anySlash(fp))
	dir, err := filepath.EvalSymlinks(filepath.Dir(p))
	if err != nil {
		return "", false
	}
	return rootRelLexical(r, filepath.Join(dir, filepath.Base(p)))
}

// rootRelLexical is rootRel without resolving symlinks.
func rootRelLexical(root, fp string) (string, bool) {
	if fp == "" || root == "" {
		return "", false
	}
	p := anySlash(fp)
	if slashAbs(p) {
		r := strings.TrimRight(anySlash(root), "/")
		if len(p) <= len(r)+1 || p[len(r)] != '/' || !samePrefix(p[:len(r)], r) {
			return "", false
		}
		p = p[len(r)+1:]
	}
	p = path.Clean(p)
	if p == "." || p == ".." || strings.HasPrefix(p, "../") || slashAbs(p) {
		return "", false
	}
	return p, true
}

// anySlash turns both OS separators and backslashes into "/".
func anySlash(s string) string { return strings.ReplaceAll(filepath.ToSlash(s), `\`, "/") }

// slashAbs reports whether a "/"-separated path is absolute ("/x" or "C:/x").
func slashAbs(p string) bool {
	return strings.HasPrefix(p, "/") || (len(p) >= 3 && p[1] == ':' && p[2] == '/')
}

// samePrefix compares path prefixes; case-insensitively on Windows.
func samePrefix(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// isPlanPath reports whether rel matches one of plan_paths.
func isPlanPath(env Env, rel string) bool {
	for _, pat := range env.Config.PlanPaths {
		if pathset.Match(pat, rel) {
			return true
		}
	}
	return false
}
