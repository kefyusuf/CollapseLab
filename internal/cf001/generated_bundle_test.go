package cf001

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGeneratedRunBundleVerifies(t *testing.T) {
	dir := os.Getenv("CF001_BUNDLE_DIR")
	if dir == "" {
		t.Skip("CF001_BUNDLE_DIR is not set")
	}

	if err := VerifyRunBundle(dir); err != nil {
		t.Fatalf("verify generated run bundle: %v", err)
	}

	manifestData, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest BundleManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.ExperimentID != "CF-001" || manifest.ExperimentVersion != 1 {
		t.Fatalf("unexpected experiment identity: %+v", manifest)
	}

	revisionData, err := os.ReadFile(filepath.Join(dir, "revision.json"))
	if err != nil {
		t.Fatal(err)
	}
	var revision RevisionEvidence
	if err := json.Unmarshal(revisionData, &revision); err != nil {
		t.Fatal(err)
	}
	if revision.Dirty {
		t.Fatal("generated canonical bundle is bound to a dirty revision")
	}
	if revision.Commit != manifest.RevisionSHA {
		t.Fatalf("revision mismatch: evidence=%s manifest=%s", revision.Commit, manifest.RevisionSHA)
	}
}
