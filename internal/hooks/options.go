package hooks

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"paraplane/internal/layout"
	"paraplane/internal/rules"
	"paraplane/internal/textx"
)

// postCheckTimeout bounds the plan check in a hook (spec section 4).
const postCheckTimeout = 5 * time.Second

// postMaxFile caps the size of a plan file a hook reads.
const postMaxFile = 1 << 20

// postMaxFindings is how many findings a hook lists before "… N more".
const postMaxFindings = 20

// Text limits for values that come from the plan or the file system.
const (
	postMaxLine   = 1000     // runes in one finding line
	postMaxPath   = 300      // runes in the plan path
	postMaxE0     = 500      // runes in the E0 message
	postMaxLayout = 64 << 10 // bytes of the shorter layout
)

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
		Deadline: postNow().Add(postCheckTimeout),
	}
}

// postFindings lists the findings one per line, at most postMaxFindings,
// then "… N more".
func postFindings(fs []rules.Finding) string {
	var b strings.Builder
	for i, f := range fs {
		if i == postMaxFindings {
			fmt.Fprintf(&b, "\n… %d more", len(fs)-i)
			break
		}
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(textx.Line(f.String(), postMaxLine))
	}
	return b.String()
}

// postWarnings is the text for a result with warnings only; W3 adds the
// shorter layout.
func postWarnings(r rules.Result) string {
	s := "paraplane: plan warnings (not blocking):\n" + postFindings(r.Findings)
	if r.Suggested != nil && r.Plan != nil {
		s += "\nShorter layout:\n" + textx.Block(layout.Render(r.Plan, r.Suggested), postMaxLayout)
	}
	return s
}

// postReadFile reads a regular file of at most postMaxFile bytes; ok is
// false when there is no such file. The type is checked with Lstat before
// opening, so a symlink, a FIFO or a device is never opened (opening a FIFO
// would block the hook until its timeout).
func postReadFile(name string) (data []byte, ok bool, err error) {
	fi, err := os.Lstat(name)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if !postRegular(fi) {
		return nil, false, nil
	}
	f, err := os.Open(name)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	defer f.Close()
	if fi, err = f.Stat(); err != nil {
		return nil, false, err
	}
	if !postRegular(fi) {
		return nil, false, nil
	}
	data, err = io.ReadAll(io.LimitReader(f, postMaxFile+1))
	if err != nil {
		return nil, false, err
	}
	if len(data) > postMaxFile {
		return nil, false, nil
	}
	return data, true, nil
}

// postRegular reports whether fi is a regular file within postMaxFile.
func postRegular(fi os.FileInfo) bool {
	return fi.Mode().IsRegular() && fi.Size() <= postMaxFile
}

// postContext is a response that only adds context for Claude.
func postContext(event, text string) ([]byte, error) {
	return encodeJSON(hookOutput{Hook: &hookSpecific{EventName: event, AdditionalContext: text}})
}
