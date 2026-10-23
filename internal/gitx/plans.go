package gitx

import (
	"errors"
	"strings"
)

// PlansSince returns the plans that the commits from..to name in a
// Paraplane-Plan trailer next to a Paraplane-Wave or Paraplane-Task trailer,
// in order of first appearance (oldest commit first). With from empty only
// the commit to is read. It needs git 2.22 or later.
func PlansSince(root, from, to string) ([]string, error) {
	v, ok, err := version(root)
	if err != nil {
		return nil, err
	}
	if !ok || !v.atLeast(2, 22, 0) {
		return nil, errors.New("git 2.22 or later is required for progress")
	}
	args := []string{"log", "--no-show-signature", "--reverse", "--format=" + planTrailers + "%x1e"}
	rng := to
	if from != "" {
		rng = from + ".." + to
		args = append(args, planGreps...)
	} else {
		args = append(args, "-1") // the commit itself; -1 after --grep would pick an older match
	}
	out, err := run(root, append(args, "--end-of-options", rng)...)
	if err != nil {
		return nil, err
	}
	var plans []string
	for _, rec := range strings.Split(string(out), "\x1e") {
		var plan string
		for _, line := range strings.Split(rec, "\n") {
			if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "paraplane-plan") {
				plan = strings.TrimSpace(v)
			}
		}
		if w, tasks := paraplaneLines(rec, plan); plan != "" && (len(w) > 0 || len(tasks) > 0) {
			plans = appendNew(plans, plan)
		}
	}
	return plans, nil
}
