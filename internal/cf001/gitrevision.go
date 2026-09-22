package cf001

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type GitRevisionSource struct {
	RepoRoot string
}

func (g GitRevisionSource) Revision(ctx context.Context) (RevisionEvidence, error) {
	root := strings.TrimSpace(g.RepoRoot)
	if root == "" {
		root = "."
	}

	commit, err := gitOutput(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return RevisionEvidence{}, fmt.Errorf("resolve git commit: %w", err)
	}
	status, err := gitOutput(ctx, root, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return RevisionEvidence{}, fmt.Errorf("resolve git status: %w", err)
	}
	branch, err := gitOutput(ctx, root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return RevisionEvidence{}, fmt.Errorf("resolve git branch: %w", err)
	}

	repository, _ := gitOutput(ctx, root, "remote", "get-url", "origin")

	return RevisionEvidence{
		Commit:     commit,
		Branch:     branch,
		Repository: repository,
		Dirty:      status != "",
	}, nil
}

func gitOutput(ctx context.Context, root string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}
