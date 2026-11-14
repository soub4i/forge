package benchmark

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/soub4i/forge/internal/config"
)

func TestParseDataNestedMetrics(t *testing.T) {
	value := map[string]any{
		"metrics": map[string]any{
			"lines_per_sec": json.Number("2724546"),
			"mb_per_sec":    160.45,
		},
		"runs": 5,
	}
	metrics := ParseData(value)
	if metrics["lines_per_sec"] != 2724546 {
		t.Fatalf("lines_per_sec = %v", metrics["lines_per_sec"])
	}
	if metrics["mb_per_sec"] != 160.45 {
		t.Fatalf("mb_per_sec = %v", metrics["mb_per_sec"])
	}
}

func TestParseDataArrayShape(t *testing.T) {
	value := []any{
		map[string]any{"name": "events_per_sec", "value": 75000.0},
		map[string]any{"metric": "p99_ms", "value": 12.5},
	}
	metrics := ParseData(value)
	if metrics["events_per_sec"] != 75000 {
		t.Fatalf("events_per_sec = %v", metrics["events_per_sec"])
	}
	if metrics["p99_ms"] != 12.5 {
		t.Fatalf("p99_ms = %v", metrics["p99_ms"])
	}
}

func TestEvaluateThresholds(t *testing.T) {
	min := 100.0
	max := 200.0
	cfg := config.Default()
	cfg.Acceptance.Benchmarks.Thresholds = []config.Threshold{
		{Metric: "throughput", Min: &min},
		{Metric: "latency", Max: &max},
		{Metric: "missing"},
	}
	result := Evaluate(cfg, t.TempDir(), `{"throughput": 150, "latency": 50}`)
	if result.OK {
		t.Fatal("missing metric should fail the benchmark")
	}
	if len(result.Failures) != 1 || result.Failures[0].Metric != "missing" {
		t.Fatalf("failures = %+v", result.Failures)
	}

	result = Evaluate(cfg, t.TempDir(), `{"throughput": 150, "latency": 50, "missing": 1}`)
	if !result.OK {
		t.Fatalf("expected pass, got failures %+v", result.Failures)
	}

	result = Evaluate(cfg, t.TempDir(), `{"throughput": 10, "latency": 500, "missing": 1}`)
	if result.OK || len(result.Failures) != 2 {
		t.Fatalf("expected two failures, got %+v", result.Failures)
	}
}

func TestReadResultsFromFile(t *testing.T) {
	dir := t.TempDir()
	if err := writeFile(dir+"/bench/results.json", `{"metrics":{"throughput":42}}`); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	metrics, source, value := ReadResults(dir, cfg, "")
	if metrics["throughput"] != 42 {
		t.Fatalf("throughput = %v", metrics["throughput"])
	}
	if source != "bench/results.json" {
		t.Fatalf("source = %q", source)
	}
	if value == nil {
		t.Fatal("expected parsed value")
	}
}

func TestFormat(t *testing.T) {
	result := Result{Metrics: map[string]float64{"throughput": 10}, Source: "stdout"}
	got := Format(result)
	if got != "throughput=10 (source: stdout)" {
		t.Fatalf("Format = %q", got)
	}
}

func writeFile(file, content string) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	return os.WriteFile(file, []byte(content), 0o644)
}
