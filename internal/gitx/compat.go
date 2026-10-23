package gitx

import "context"

// The functions below keep the signatures the callers outside gitx use until
// they move to the context versions; each passes context.Background(), so
// every git call keeps its own 10 s limit. Task R13 removes them.

// Changed is ChangedContext without a caller budget.
func Changed(root string) ([]string, error) { return ChangedContext(context.Background(), root) }

// PlanCommits is PlanCommitsContext without a caller budget.
func PlanCommits(root, planPath string) (Commits, error) {
	return PlanCommitsContext(context.Background(), root, planPath)
}

// PlanCommitFiles is PlanCommitFilesContext without a caller budget.
func PlanCommitFiles(root, planPath string) ([]CommitFiles, error) {
	return PlanCommitFilesContext(context.Background(), root, planPath)
}

// PlansSince is PlansSinceContext without a caller budget.
func PlansSince(root, from, to string) ([]string, error) {
	return PlansSinceContext(context.Background(), root, from, to)
}

// Head is HeadContext without a caller budget.
func Head(root string) (string, error) { return HeadContext(context.Background(), root) }

// GitDir is GitDirContext without a caller budget.
func GitDir(root string) (string, error) { return GitDirContext(context.Background(), root) }

// IsAncestor is IsAncestorContext without a caller budget.
func IsAncestor(root, a, b string) (bool, error) {
	return IsAncestorContext(context.Background(), root, a, b)
}
