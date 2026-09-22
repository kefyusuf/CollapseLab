package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"collapselab/internal/cf001"
)

func main() {
	var (
		configPath = flag.String(
			"config",
			"experiments/cf-001-coordinated-omission/experiment.yaml",
			"CF-001 experiment configuration",
		)
		runsRoot = flag.String(
			"runs-root",
			"runs/cf-001",
			"directory that receives revision-bound run bundles",
		)
		repoRoot = flag.String(
			"repo-root",
			".",
			"CollapseLab repository root",
		)
	)
	flag.Parse()

	root, err := filepath.Abs(*repoRoot)
	if err != nil {
		log.Fatalf("resolve repo root: %v", err)
	}
	config, err := absoluteFromRoot(root, *configPath)
	if err != nil {
		log.Fatal(err)
	}
	runs, err := absoluteFromRoot(root, *runsRoot)
	if err != nil {
		log.Fatal(err)
	}

	result, err := cf001.RunCF001(
		context.Background(),
		cf001.RunnerOptions{
			ConfigPath: config,
			RunsRoot:  runs,
		},
		cf001.RunnerDependencies{
			Revision: cf001.GitRevisionSource{RepoRoot: root},
			Lab:      &cf001.DockerCF001Lab{RepoRoot: root},
		},
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Fprintln(os.Stderr, "CF-001 status:", result.Evaluation.Status)
	fmt.Fprintln(os.Stdout, result.BundleDir)
}

func absoluteFromRoot(root, path string) (string, error) {
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	joined := filepath.Join(root, path)
	absolute, err := filepath.Abs(joined)
	if err != nil {
		return "", fmt.Errorf("resolve path %q: %w", path, err)
	}
	return absolute, nil
}
