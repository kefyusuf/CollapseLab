package cf001

import (
	"context"
	"os"
	"strings"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGitRevisionSourceTreatsUntrackedFilesAsDirtyButHonorsGitignore(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "collapselab@example.test")
	runGit(t, root, "config", "user.name", "CollapseLab Test")

	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("/runs/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("baseline\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", ".gitignore", "tracked.txt")
	runGit(t, root, "commit", "-m", "baseline")

	source := GitRevisionSource{RepoRoot: root}
	clean, err := source.Revision(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if clean.Dirty {
		t.Fatal("fresh committed repository must be clean")
	}

	if err := os.MkdirAll(filepath.Join(root, "runs", "cf-001"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "runs", "cf-001", "evidence.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ignoredOnly, err := source.Revision(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ignoredOnly.Dirty {
		t.Fatal("gitignored evidence must not dirty the revision")
	}

	if err := os.WriteFile(filepath.Join(root, "untracked.go"), []byte("package example\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	withUntracked, err := source.Revision(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !withUntracked.Dirty {
		t.Fatal("untracked source must mark revision dirty")
	}
}

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}


func TestGitRevisionSourceSanitizesCredentialsFromOrigin(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "collapselab@example.test")
	runGit(t, root, "config", "user.name", "CollapseLab Test")

	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("baseline\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "tracked.txt")
	runGit(t, root, "commit", "-m", "baseline")
	runGit(t, root, "remote", "add", "origin", "https://user:super-secret@example.test/owner/repo.git")

	evidence, err := (GitRevisionSource{RepoRoot: root}).Revision(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(evidence.Repository, "user") || strings.Contains(evidence.Repository, "super-secret") {
		t.Fatalf("repository evidence leaked remote credentials: %q", evidence.Repository)
	}
	if evidence.Repository != "https://example.test/owner/repo.git" {
		t.Fatalf("sanitized repository = %q", evidence.Repository)
	}
}
