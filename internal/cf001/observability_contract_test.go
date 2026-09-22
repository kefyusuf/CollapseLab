package cf001

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestPrometheusScrapeResolutionCanObserveCanonicalStall(t *testing.T) {
	cfg := validConfig()

	path := filepath.Join("..", "..", "observability", "prometheus", "prometheus.yml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read prometheus config: %v", err)
	}

	match := regexp.MustCompile(`(?m)^\s*scrape_interval:\s*([^\s#]+)`).FindStringSubmatch(string(content))
	if len(match) != 2 {
		t.Fatal("prometheus scrape_interval is missing")
	}

	scrapeInterval, err := time.ParseDuration(strings.TrimSpace(match[1]))
	if err != nil {
		t.Fatalf("parse scrape_interval: %v", err)
	}

	maximum := cfg.Trial.StallDuration.Duration / 2
	if scrapeInterval > maximum {
		t.Fatalf(
			"scrape interval %s cannot reliably observe %s stall; want <= %s",
			scrapeInterval,
			cfg.Trial.StallDuration.Duration,
			maximum,
		)
	}
}
