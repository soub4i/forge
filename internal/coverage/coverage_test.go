package coverage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/soub4i/forge/internal/config"
)

func TestExtractFromJSON(t *testing.T) {
	value := map[string]any{
		"total": map[string]any{
			"lines": map[string]any{"pct": 87.5},
		},
	}
	percent := ExtractFromJSON(value)
	if percent == nil || *percent != 87.5 {
		t.Fatalf("percent = %v", percent)
	}
}

func TestExtractFromJSONPythonShape(t *testing.T) {
	value := map[string]any{"totals": map[string]any{"percent_covered": 73.25}}
	percent := ExtractFromJSON(value)
	if percent == nil || *percent != 73.25 {
		t.Fatalf("percent = %v", percent)
	}
}

func TestExtractFromGoMultiPackageOutput(t *testing.T) {
	text := `	forge/cmd/forge		coverage: 0.0% of statements
ok  	forge/internal/config	0.043s	coverage: 87.6% of statements
total:							(statements)		76.1%`
	percent := ExtractFromText(text)
	if percent == nil || *percent != 76.1 {
		t.Fatalf("Go total was not preferred: %v", percent)
	}
}

func TestExtractFromTextPatterns(t *testing.T) {
	cases := map[string]float64{
		"coverage: 82.5% of statements":         82.5,
		"total: (statements) 71.2%":             71.2,
		"All files | 64.4 | 50.0 | 70.0 | 65.0": 64.4,
		"TOTAL 100 20 80.0%":                    80.0,
		"Lines : 91.3%":                         91.3,
		"91.3% lines covered":                   91.3,
	}
	for text, want := range cases {
		percent := ExtractFromText(text)
		if percent == nil {
			t.Fatalf("no percentage parsed from %q", text)
		}
		if *percent != want {
			t.Errorf("ExtractFromText(%q) = %v, want %v", text, *percent, want)
		}
	}
}

func TestExtractPrefersConfiguredFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "reports"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := "reports/coverage.json"
	if err := os.WriteFile(filepath.Join(dir, file), []byte(`{"total":{"lines":{"pct":66.6}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Acceptance.Coverage.File = &file
	percent, source := Extract(dir, cfg, "coverage: 1.0%")
	if percent == nil || *percent != 66.6 {
		t.Fatalf("percent = %v", percent)
	}
	if source != file {
		t.Fatalf("source = %q", source)
	}
}

func TestExtractFromJSONMoreShapes(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  float64
	}{
		{"statements", map[string]any{"total": map[string]any{"statements": map[string]any{"pct": json.Number("66.6")}}}, 66.6},
		{"python", map[string]any{"totals": map[string]any{"percent_covered": "72.5"}}, 72.5},
		{"camel", map[string]any{"percentCovered": 44.0}, 44.0},
		{"coverage", map[string]any{"coverage": 33.3}, 33.3},
		{"total percent", map[string]any{"total": map[string]any{"percent": 42.0}}, 42.0},
		{"lines pct", map[string]any{"lines": map[string]any{"pct": 12.5}}, 12.5},
		{"nested array", []any{map[string]any{"total": map[string]any{"lines": map[string]any{"pct": 51.0}}}}, 51.0},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			percent := ExtractFromJSON(test.value)
			if percent == nil || *percent != test.want {
				t.Fatalf("percent = %v, want %v", percent, test.want)
			}
		})
	}
	if got := ExtractFromJSON(map[string]any{"unrelated": 10}); got != nil {
		t.Fatalf("unrelated JSON = %v", got)
	}
	if got := ExtractFromJSON("55%"); got != nil {
		t.Fatalf("scalar JSON = %v", got)
	}
}

func TestExtractDefaultLocations(t *testing.T) {
	cfg := config.Default()

	stdoutDir := t.TempDir()
	percent, source := Extract(stdoutDir, cfg, "All files | 55.5 | 20 | 70 | 55.5")
	if percent == nil || *percent != 55.5 || source != "stdout" {
		t.Fatalf("stdout percent = %v, source = %q", percent, source)
	}

	summaryDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(summaryDir, "coverage"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(summaryDir, "coverage", "coverage-summary.json"), []byte(`{"total":{"lines":{"pct":64.2}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	percent, source = Extract(summaryDir, cfg, "coverage: 1.0%")
	if percent == nil || *percent != 64.2 || source != "coverage/coverage-summary.json" {
		t.Fatalf("summary percent = %v, source = %q", percent, source)
	}

	outDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(outDir, "coverage.out"), []byte("coverage: 77.7%\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	percent, source = Extract(outDir, cfg, "")
	if percent == nil || *percent != 77.7 || source != "coverage.out" {
		t.Fatalf("coverage.out percent = %v, source = %q", percent, source)
	}

	emptyDir := t.TempDir()
	if percent, source := Extract(emptyDir, cfg, "no numbers here"); percent != nil || source != "" {
		t.Fatalf("empty extraction = %v, %q", percent, source)
	}
}

func TestConfiguredFileInvalidFallsThrough(t *testing.T) {
	dir := t.TempDir()
	file := "bad.json"
	if err := os.WriteFile(filepath.Join(dir, file), []byte(`{"nothing":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Acceptance.Coverage.File = &file
	percent, source := Extract(dir, cfg, "coverage: 61.5%")
	if percent == nil || *percent != 61.5 || source != "stdout" {
		t.Fatalf("percent = %v, source = %q", percent, source)
	}
}
