package cf001

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCreateRunBundleRejectsExistingRunID(t *testing.T) {
	root := t.TempDir()

	first, err := CreateRunBundle(root, "run-001")
	if err != nil {
		t.Fatal(err)
	}
	if first.Dir() == "" {
		t.Fatal("bundle directory is empty")
	}

	if _, err := CreateRunBundle(root, "run-001"); err == nil {
		t.Fatal("expected existing run id to be rejected")
	}
}

func TestRunBundleRejectsPathTraversal(t *testing.T) {
	root := t.TempDir()
	bundle, err := CreateRunBundle(root, "run-001")
	if err != nil {
		t.Fatal(err)
	}

	if err := bundle.Write("../escape.json", []byte("{}")); err == nil {
		t.Fatal("expected path traversal to be rejected")
	}
	if _, err := os.Stat(filepath.Join(root, "escape.json")); !os.IsNotExist(err) {
		t.Fatalf("escape file must not exist, stat err=%v", err)
	}
}

func TestFinalizeAndVerifyRunBundleBindsRevisionAndConfig(t *testing.T) {
	root := t.TempDir()
	bundle, err := CreateRunBundle(root, "run-001")
	if err != nil {
		t.Fatal(err)
	}

	config := []byte("schema_version: 1\nid: CF-001\n")
	if err := bundle.Write("experiment.yaml", config); err != nil {
		t.Fatal(err)
	}
	if err := bundle.WriteJSON("revision.json", RevisionEvidence{
		Commit:     strings.Repeat("a", 40),
		Branch:     "feat/cf-001-coordinated-omission",
		Repository: "https://github.com/kefyusuf/CollapseLab",
		Dirty:      false,
	}); err != nil {
		t.Fatal(err)
	}
	if err := bundle.WriteJSON("environment.json", map[string]any{
		"go_version": "go1.27.1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := bundle.Write("timeline.jsonl", []byte("{\"event\":\"run.created\"}\n")); err != nil {
		t.Fatal(err)
	}
	if err := bundle.Write("closed/k6-summary.json", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if err := bundle.Write("open/k6-summary.json", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if err := bundle.Write("metrics/closed.json", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if err := bundle.Write("metrics/open.json", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if err := bundle.Write("assertions.json", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if err := bundle.Write("report.md", []byte("# CF-001\n")); err != nil {
		t.Fatal(err)
	}

	createdAt := time.Date(2026, 9, 22, 20, 30, 0, 0, time.UTC)
	manifest, err := bundle.Finalize(ManifestMetadata{
		SchemaVersion:      1,
		ExperimentID:       "CF-001",
		ExperimentVersion:  1,
		CreatedAt:          createdAt,
		RevisionSHA:        strings.Repeat("a", 40),
		ConfigSHA256:       SHA256Hex(config),
	})
	if err != nil {
		t.Fatal(err)
	}

	if manifest.RunID != "run-001" {
		t.Fatalf("run id = %q", manifest.RunID)
	}
	if len(manifest.Artifacts) != 10 {
		t.Fatalf("artifact count = %d, want 10", len(manifest.Artifacts))
	}
	if _, ok := manifest.Artifacts["manifest.json"]; ok {
		t.Fatal("manifest must not hash itself")
	}

	if err := VerifyRunBundle(bundle.Dir()); err != nil {
		t.Fatalf("verify bundle: %v", err)
	}
}

func TestVerifyRunBundleDetectsArtifactTampering(t *testing.T) {
	root := t.TempDir()
	bundle, err := CreateRunBundle(root, "run-001")
	if err != nil {
		t.Fatal(err)
	}

	config := []byte("schema_version: 1\nid: CF-001\n")
	if err := bundle.Write("experiment.yaml", config); err != nil {
		t.Fatal(err)
	}
	if err := bundle.WriteJSON("revision.json", RevisionEvidence{
		Commit: strings.Repeat("b", 40),
		Dirty:  false,
	}); err != nil {
		t.Fatal(err)
	}
	if err := bundle.Write("evidence.json", []byte("{\"value\":1}\n")); err != nil {
		t.Fatal(err)
	}

	if _, err := bundle.Finalize(ManifestMetadata{
		SchemaVersion:     1,
		ExperimentID:      "CF-001",
		ExperimentVersion: 1,
		CreatedAt:         time.Now().UTC(),
		RevisionSHA:       strings.Repeat("b", 40),
		ConfigSHA256:      SHA256Hex(config),
	}); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(bundle.Dir(), "evidence.json"), []byte("{\"value\":2}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err = VerifyRunBundle(bundle.Dir())
	if err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("expected digest verification failure, got %v", err)
	}
}

func TestVerifyRunBundleRejectsRevisionMismatch(t *testing.T) {
	root := t.TempDir()
	bundle, err := CreateRunBundle(root, "run-001")
	if err != nil {
		t.Fatal(err)
	}

	config := []byte("schema_version: 1\nid: CF-001\n")
	if err := bundle.Write("experiment.yaml", config); err != nil {
		t.Fatal(err)
	}
	if err := bundle.WriteJSON("revision.json", RevisionEvidence{
		Commit: strings.Repeat("c", 40),
		Dirty:  false,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := bundle.Finalize(ManifestMetadata{
		SchemaVersion:     1,
		ExperimentID:      "CF-001",
		ExperimentVersion: 1,
		CreatedAt:         time.Now().UTC(),
		RevisionSHA:       strings.Repeat("d", 40),
		ConfigSHA256:      SHA256Hex(config),
	}); err != nil {
		t.Fatal(err)
	}

	err = VerifyRunBundle(bundle.Dir())
	if err == nil || !strings.Contains(err.Error(), "revision") {
		t.Fatalf("expected revision mismatch, got %v", err)
	}
}
