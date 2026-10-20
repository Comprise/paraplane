package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"paraplane/internal/config"
	"paraplane/internal/fsx"
	"paraplane/internal/hooks"
	"paraplane/internal/layout"
	"paraplane/internal/rules"
	"paraplane/internal/textx"
)

// maxPlanFile caps the size of a plan file the CLI reads (spec section 7).
const maxPlanFile = 1 << 20

// Text limits for values that come from the plan, the config or the file system.
const (
	maxLine = 1000 // runes in one finding line
	maxName = 300  // runes in a file name
)

func runCheck(args []string, stdin io.Reader, stdout, stderr io.Writer, cwd string) int {
	suggest, name, n := false, "", 0
	for _, a := range args {
		switch {
		case a == "--suggest":
			suggest = true
		case strings.HasPrefix(a, "-") && a != "-":
			n = -1
		default:
			name = a
			n++
		}
		if n < 0 {
			break
		}
	}
	if n != 1 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	root := config.FindRoot(cwd)
	env := loadEnv(stdout, root)
	var md []byte
	var err error
	if name == "-" {
		md, err = readLimited(stdin, "stdin")
	} else {
		md, _, err = readPlanFile(name, cwd, root)
	}
	if err != nil {
		printErr(stdout, err)
		return 1
	}
	r := rules.Check(md, cliOptions(env))
	for _, f := range r.Findings {
		fmt.Fprintln(stdout, textx.Line(f.String(), maxLine))
	}
	if suggest && r.Suggested != nil && r.Plan != nil {
		fmt.Fprintln(stdout, "Shorter layout:")
		fmt.Fprint(stdout, strings.TrimSuffix(textx.Block(layout.Render(r.Plan, r.Suggested), math.MaxInt), "\n")+"\n")
	}
	fmt.Fprintln(stdout, r.Summary())
	if r.Errors() > 0 {
		return 1
	}
	return 0
}

// loadEnv loads the config of root and prints its problems as
// "warning: config: …" lines to w.
func loadEnv(w io.Writer, root string) hooks.Env {
	cfg, err := config.Load(root)
	var ps config.Problems
	switch {
	case errors.As(err, &ps):
		for _, p := range ps {
			fmt.Fprintln(w, "warning: config: "+textx.Line(config.FileName+": ignored "+p, maxLine))
		}
	case err != nil:
		fmt.Fprintln(w, "warning: config: "+textx.Line(err.Error(), maxLine)+" (using defaults)")
	}
	return hooks.Env{Root: root, Config: cfg, ConfigErr: err}
}

// cliOptions are the hook options without a deadline: the CLI may take its time.
func cliOptions(env hooks.Env) rules.Options {
	o := hooks.Options(env)
	o.Deadline = time.Time{}
	return o
}

// printErr prints err as one clean "error: …" line.
func printErr(w io.Writer, err error) {
	fmt.Fprintln(w, "error: "+textx.Line(err.Error(), maxLine))
}

// readLimited reads at most maxPlanFile bytes from r.
func readLimited(r io.Reader, name string) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxPlanFile+1))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if len(data) > maxPlanFile {
		return nil, fmt.Errorf("%s is larger than %d bytes", name, maxPlanFile)
	}
	return data, nil
}

// readPlanFile reads the plan name (relative to cwd): a regular file, not a
// symlink or a FIFO (read through fsx), inside root, of at most maxPlanFile bytes. rel is its
// path from root with "/" separators.
func readPlanFile(name, cwd, root string) (data []byte, rel string, err error) {
	shown := textx.Line(name, maxName)
	abs := name
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(cwd, abs)
	}
	abs = filepath.Clean(abs)
	// The directories on the way may be symlinks (macOS /var → /private/var):
	// compare the real paths. The file itself must not be one.
	dir, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", shown, unwrapPath(err))
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, "", fmt.Errorf("project root: %w", unwrapPath(err))
	}
	r, err := filepath.Rel(realRoot, filepath.Join(dir, filepath.Base(abs)))
	if err != nil || r == "." || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) || filepath.IsAbs(r) {
		return nil, "", fmt.Errorf("%s is outside the project root", shown)
	}
	data, err = fsx.ReadRegular(filepath.Join(realRoot, r), maxPlanFile)
	switch {
	case err == nil:
		return data, filepath.ToSlash(r), nil
	case errors.Is(err, fsx.ErrTooLarge):
		return nil, "", fmt.Errorf("%s is larger than %d bytes", shown, maxPlanFile)
	case errors.Is(err, fsx.ErrNotRegular):
		return nil, "", fmt.Errorf("%s is not a regular file (a symlink, directory or special file)", shown)
	case errors.Is(err, fs.ErrNotExist):
		return nil, "", fmt.Errorf("%s: no such file", shown)
	}
	return nil, "", fmt.Errorf("%s: %w", shown, unwrapPath(err))
}

// unwrapPath drops the path from a *fs.PathError: the caller names the file.
func unwrapPath(err error) error {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}
