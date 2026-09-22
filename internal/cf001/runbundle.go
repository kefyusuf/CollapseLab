package cf001

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const bundleManifestName = "manifest.json"

type RevisionEvidence struct {
	Commit     string `json:"commit"`
	Branch     string `json:"branch,omitempty"`
	Repository string `json:"repository,omitempty"`
	Dirty      bool   `json:"dirty"`
}

type ManifestMetadata struct {
	SchemaVersion     int
	ExperimentID      string
	ExperimentVersion int
	CreatedAt         time.Time
	RevisionSHA       string
	ConfigSHA256      string
}

type ArtifactDigest struct {
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

type BundleManifest struct {
	SchemaVersion     int                       `json:"schema_version"`
	RunID             string                    `json:"run_id"`
	ExperimentID      string                    `json:"experiment_id"`
	ExperimentVersion int                       `json:"experiment_version"`
	CreatedAt         time.Time                 `json:"created_at"`
	RevisionSHA       string                    `json:"revision_sha"`
	ConfigSHA256      string                    `json:"config_sha256"`
	Artifacts         map[string]ArtifactDigest `json:"artifacts"`
}

type RunBundle struct {
	dir       string
	runID     string
	finalized bool
}

func CreateRunBundle(root, runID string) (*RunBundle, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("bundle root is required")
	}
	if !validRunID(runID) {
		return nil, fmt.Errorf("invalid run id %q", runID)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create bundle root: %w", err)
	}

	dir := filepath.Join(root, runID)
	if err := os.Mkdir(dir, 0o755); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("run bundle %q already exists", runID)
		}
		return nil, fmt.Errorf("create run bundle: %w", err)
	}
	return &RunBundle{dir: dir, runID: runID}, nil
}

func validRunID(runID string) bool {
	if strings.TrimSpace(runID) == "" || runID == "." || runID == ".." {
		return false
	}
	return !strings.ContainsAny(runID, "/\\")
}

func (b *RunBundle) Dir() string {
	return b.dir
}

func (b *RunBundle) Write(rel string, data []byte) error {
	if b.finalized {
		return errors.New("run bundle is finalized")
	}
	path, err := b.safePath(rel)
	if err != nil {
		return err
	}
	if rel == bundleManifestName {
		return errors.New("manifest.json is reserved")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create artifact directory: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write artifact %q: %w", rel, err)
	}
	return nil
}

func (b *RunBundle) WriteJSON(rel string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %q: %w", rel, err)
	}
	data = append(data, '\n')
	return b.Write(rel, data)
}

func (b *RunBundle) safePath(rel string) (string, error) {
	if strings.TrimSpace(rel) == "" || filepath.IsAbs(rel) {
		return "", fmt.Errorf("invalid artifact path %q", rel)
	}
	clean := filepath.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("artifact path escapes bundle: %q", rel)
	}
	path := filepath.Join(b.dir, clean)
	relative, err := filepath.Rel(b.dir, path)
	if err != nil {
		return "", fmt.Errorf("resolve artifact path: %w", err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("artifact path escapes bundle: %q", rel)
	}
	return path, nil
}

func (b *RunBundle) Finalize(meta ManifestMetadata) (BundleManifest, error) {
	if b.finalized {
		return BundleManifest{}, errors.New("run bundle is already finalized")
	}
	if meta.SchemaVersion != 1 {
		return BundleManifest{}, errors.New("manifest schema_version must be 1")
	}
	if meta.ExperimentID == "" || meta.ExperimentVersion < 1 {
		return BundleManifest{}, errors.New("experiment identity is required")
	}
	if meta.CreatedAt.IsZero() {
		return BundleManifest{}, errors.New("manifest created_at is required")
	}
	if !validSHA256(meta.ConfigSHA256) {
		return BundleManifest{}, errors.New("manifest config_sha256 is invalid")
	}
	if strings.TrimSpace(meta.RevisionSHA) == "" {
		return BundleManifest{}, errors.New("manifest revision_sha is required")
	}

	artifacts, err := digestBundleFiles(b.dir)
	if err != nil {
		return BundleManifest{}, err
	}
	if len(artifacts) == 0 {
		return BundleManifest{}, errors.New("run bundle has no artifacts")
	}

	manifest := BundleManifest{
		SchemaVersion:     meta.SchemaVersion,
		RunID:             b.runID,
		ExperimentID:      meta.ExperimentID,
		ExperimentVersion: meta.ExperimentVersion,
		CreatedAt:         meta.CreatedAt.UTC(),
		RevisionSHA:       meta.RevisionSHA,
		ConfigSHA256:      meta.ConfigSHA256,
		Artifacts:         artifacts,
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return BundleManifest{}, fmt.Errorf("encode manifest: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(b.dir, bundleManifestName), data, 0o644); err != nil {
		return BundleManifest{}, fmt.Errorf("write manifest: %w", err)
	}
	b.finalized = true
	return manifest, nil
}

func VerifyRunBundle(dir string) error {
	manifestData, err := os.ReadFile(filepath.Join(dir, bundleManifestName))
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	var manifest BundleManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return fmt.Errorf("decode manifest: %w", err)
	}
	if manifest.SchemaVersion != 1 {
		return fmt.Errorf("unsupported manifest schema_version %d", manifest.SchemaVersion)
	}
	if !validSHA256(manifest.ConfigSHA256) {
		return errors.New("manifest config digest is invalid")
	}

	actualArtifacts, err := digestBundleFiles(dir)
	if err != nil {
		return err
	}
	if len(actualArtifacts) != len(manifest.Artifacts) {
		return fmt.Errorf("artifact set mismatch: manifest=%d actual=%d", len(manifest.Artifacts), len(actualArtifacts))
	}

	keys := make([]string, 0, len(manifest.Artifacts))
	for name := range manifest.Artifacts {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		expected := manifest.Artifacts[name]
		actual, ok := actualArtifacts[name]
		if !ok {
			return fmt.Errorf("artifact %q is missing", name)
		}
		if actual.SHA256 != expected.SHA256 {
			return fmt.Errorf("artifact %q digest mismatch", name)
		}
		if actual.Bytes != expected.Bytes {
			return fmt.Errorf("artifact %q size mismatch", name)
		}
	}

	configData, err := os.ReadFile(filepath.Join(dir, "experiment.yaml"))
	if err != nil {
		return fmt.Errorf("read experiment config: %w", err)
	}
	if got := SHA256Hex(configData); got != manifest.ConfigSHA256 {
		return fmt.Errorf("config digest mismatch: manifest=%s actual=%s", manifest.ConfigSHA256, got)
	}

	revisionData, err := os.ReadFile(filepath.Join(dir, "revision.json"))
	if err != nil {
		return fmt.Errorf("read revision evidence: %w", err)
	}
	var revision RevisionEvidence
	if err := json.Unmarshal(revisionData, &revision); err != nil {
		return fmt.Errorf("decode revision evidence: %w", err)
	}
	if revision.Dirty {
		return errors.New("revision evidence marks working tree dirty")
	}
	if revision.Commit != manifest.RevisionSHA {
		return fmt.Errorf("revision mismatch: manifest=%s evidence=%s", manifest.RevisionSHA, revision.Commit)
	}
	if filepath.Base(filepath.Clean(dir)) != manifest.RunID {
		return fmt.Errorf("run id mismatch: manifest=%s directory=%s", manifest.RunID, filepath.Base(filepath.Clean(dir)))
	}

	return nil
}

func digestBundleFiles(dir string) (map[string]ArtifactDigest, error) {
	artifacts := make(map[string]ArtifactDigest)
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == bundleManifestName {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink artifacts are not allowed: %s", rel)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		artifacts[rel] = ArtifactDigest{
			SHA256: SHA256Hex(data),
			Bytes:  info.Size(),
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("digest bundle: %w", err)
	}
	return artifacts, nil
}

func SHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
