package gitx

import (
	"bytes"
	"context"
	"fmt"
)

// PlansSinceContext returns the plans that the commits from..to name in a
// Paraplane-Plan trailer next to a Paraplane-Wave or Paraplane-Task trailer,
// in order of first appearance (oldest commit first). With from empty only
// the commit to is read. from (when set) and to must be full hashes. It
// needs git 2.24 or later (ErrOldGit) and refuses like guardLazyFetch.
func PlansSinceContext(ctx context.Context, root, from, to string) ([]string, error) {
	if err := requireGit(ctx, root, minGit[0], minGit[1], minGit[2]); err != nil {
		return nil, fmt.Errorf("%w for progress", err)
	}
	if err := checkHashes(to); err != nil {
		return nil, err
	}
	args := []string{"log", "--no-show-signature", "--reverse", "--format=" + planTrailers + "%x1e"}
	rng := to
	if from != "" {
		if err := checkHashes(from); err != nil {
			return nil, err
		}
		rng = from + ".." + to
		args = append(args, planGreps...)
	} else {
		args = append(args, "-1") // the commit itself; -1 after --grep would pick an older match
	}
	if err := guardLazyFetch(ctx, root); err != nil {
		return nil, err
	}
	out, err := run(ctx, root, append(args, "--end-of-options", rng)...)
	if err != nil {
		return nil, err
	}
	var plans []string
	for rec := range bytes.SplitSeq(out, recSep) {
		if l := parseLines(rec); l.plan != "" && len(l.waves)+len(l.tasks) > 0 {
			plans = appendNew(plans, l.plan)
		}
	}
	return plans, nil
}
