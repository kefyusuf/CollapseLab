package cf001

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestClosedWorkloadContract(t *testing.T) {
	source := readWorkload(t, "closed.js")

	assertJSMatch(t, source, `executor\s*:\s*["']constant-vus["']`)
	assertJSMatch(t, source, `vus\s*:\s*5\b`)
	assertJSMatch(t, source, `duration\s*:\s*["']30s["']`)
	assertJSContains(t, source, "http://sut-lab:8080/work")
	assertJSMatch(t, source, `["']X-CollapseLab-Scenario["']\s*:\s*["']closed["']`)
	assertJSMatch(t, source, `new\s+Trend\s*\(\s*["']work_latency["']`)
	assertJSContains(t, source, "handleSummary")
	assertJSMatch(t, source, `summaryTrendStats\s*:\s*\[[^\]]*["']p\(99\)["']`)

	if regexp.MustCompile(`\bsleep\s*\(`).MatchString(source) {
		t.Fatal("closed workload must not contain sleep-based pacing")
	}
}

func TestOpenWorkloadContract(t *testing.T) {
	source := readWorkload(t, "open.js")

	assertJSMatch(t, source, `executor\s*:\s*["']constant-arrival-rate["']`)
	assertJSMatch(t, source, `rate\s*:\s*100\b`)
	assertJSMatch(t, source, `timeUnit\s*:\s*["']1s["']`)
	assertJSMatch(t, source, `duration\s*:\s*["']30s["']`)
	assertJSMatch(t, source, `preAllocatedVUs\s*:\s*100\b`)
	assertJSMatch(t, source, `maxVUs\s*:\s*100\b`)
	assertJSContains(t, source, "http://sut-lab:8080/work")
	assertJSMatch(t, source, `["']X-CollapseLab-Scenario["']\s*:\s*["']open["']`)
	assertJSMatch(t, source, `new\s+Trend\s*\(\s*["']work_latency["']`)
	assertJSContains(t, source, "handleSummary")
	assertJSMatch(t, source, `summaryTrendStats\s*:\s*\[[^\]]*["']p\(99\)["']`)
}

func TestWorkloadSummaryContractSurfacesDroppedIterations(t *testing.T) {
	source := readWorkload(t, "summary.js")

	assertJSContains(t, source, "schema_version")
	assertJSContains(t, source, "scenario")
	assertJSContains(t, source, "dropped_iterations")
	assertJSContains(t, source, "data.metrics")
}

func TestWorkloadsDoNotTargetHostPublishedPorts(t *testing.T) {
	for _, name := range []string{"closed.js", "open.js"} {
		source := readWorkload(t, name)
		if strings.Contains(source, "127.0.0.1") || strings.Contains(source, "localhost") {
			t.Fatalf("%s must use the internal lab data-plane identity, not a host-published address", name)
		}
	}
}

func readWorkload(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("..", "..", "workloads", "k6", "cf001", name)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read workload %s: %v", name, err)
	}
	return string(content)
}

func assertJSContains(t *testing.T, source, needle string) {
	t.Helper()
	if !strings.Contains(source, needle) {
		t.Fatalf("expected workload source to contain %q", needle)
	}
}

func assertJSMatch(t *testing.T, source, expression string) {
	t.Helper()
	if !regexp.MustCompile(expression).MatchString(source) {
		t.Fatalf("expected workload source to match %q", expression)
	}
}
