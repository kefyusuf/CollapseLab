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
		count = flag.Int("count", 3, "number of canonical CF-001 repetitions")
		configPath = flag.String(
			"config",
			"experiments/cf-001-coordinated-omission/experiment.yaml",
			"CF-001 experiment configuration",
		)
		runsRoot = flag.String(
			"runs-root",
			"runs/cf-001",
			"directory that receives revision-bound child bundles",
		)
		summaryPath = flag.String(
			"summary",
			"runs/cf-001/repetition-summary.json",
			"curated repetition summary output",
		)
		reportPath = flag.String(
			"report",
			"runs/cf-001/repetition-report.md",
			"human-readable repetition report output",
		)
		repoRoot = flag.String("repo-root", ".", "CollapseLab repository root")
	)
	flag.Parse()

	root, err := filepath.Abs(*repoRoot)
	if err != nil {
		log.Fatalf("resolve repo root: %v", err)
	}

	resolve := func(path string) string {
		if filepath.IsAbs(path) {
			return filepath.Clean(path)
		}
		return filepath.Join(root, path)
	}

	result, err := cf001.RunCF001Repetitions(
		context.Background(),
		cf001.RepetitionOptions{
			Count:       *count,
			ConfigPath:  resolve(*configPath),
			RunsRoot:    resolve(*runsRoot),
			SummaryPath: resolve(*summaryPath),
			ReportPath:  resolve(*reportPath),
		},
		cf001.RunnerDependencies{
			Revision: cf001.GitRevisionSource{RepoRoot: root},
			Lab:      &cf001.DockerCF001Lab{RepoRoot: root},
		},
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Fprintln(os.Stderr, "CF-001 repetition gate:", passLabel(result.Assessment.Passed))
	fmt.Fprintln(os.Stderr, "CF-001 hypothesis consensus:", result.Assessment.HypothesisConsensus)
	fmt.Fprintln(os.Stdout, result.SummaryPath)
}

func passLabel(passed bool) string {
	if passed {
		return "PASS"
	}
	return "FAIL"
}
