package coverage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/soub4i/forge/internal/config"
)

func writeCoverageFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExtractCoverageFileFormats(t *testing.T) {
	cases := []struct {
		name    string
		rel     string
		content string
		want    float64
	}{
		{
			name:    "cobertura",
			rel:     "reports/coverage.xml",
			content: `<?xml version="1.0"?><coverage line-rate="0.875" branch-rate="0.5"><packages/></coverage>`,
			want:    87.5,
		},
		{
			name:    "cobertura zero",
			rel:     "coverage/coverage.cobertura.xml",
			content: `<?xml version="1.0"?><coverage line-rate="0" branch-rate="0"><packages/></coverage>`,
			want:    0,
		},
		{
			name:    "jacoco",
			rel:     "target/site/jacoco/jacoco.xml",
			content: `<?xml version="1.0"?><report name="demo"><counter type="INSTRUCTION" missed="10" covered="90"/><counter type="LINE" missed="25" covered="75"/></report>`,
			want:    75,
		},
		{
			name:    "lcov",
			rel:     "coverage/lcov.info",
			content: "TN:\nSF:src/a.js\nLF:10\nLH:5\nend_of_record\nSF:src/b.js\nLF:10\nLH:5\nend_of_record\n",
			want:    50,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeCoverageFile(t, dir, tc.rel, tc.content)
			percent, source := Extract(dir, config.Default(), "")
			if percent == nil {
				t.Fatalf("coverage was not parsed from %s", tc.rel)
			}
			if diff := *percent - tc.want; diff > 0.001 || diff < -0.001 {
				t.Fatalf("coverage = %v, want %v", *percent, tc.want)
			}
			if source != tc.rel {
				t.Fatalf("source = %q, want %q", source, tc.rel)
			}
		})
	}
}

func TestExtractGoProfile(t *testing.T) {
	dir := t.TempDir()
	writeCoverageFile(t, dir, "coverage.out", `mode: set
example.com/demo/a.go:1.1,2.1 2 1
example.com/demo/a.go:3.1,4.1 2 0
example.com/demo/b.go:1.1,2.1 1 3
`)
	percent, source := Extract(dir, config.Default(), "")
	if percent == nil || source != "coverage.out" {
		t.Fatalf("percent = %v source = %q", percent, source)
	}
	want := 60.0
	if diff := *percent - want; diff > 0.001 || diff < -0.001 {
		t.Fatalf("coverage = %v, want %v", *percent, want)
	}
}

func TestExtractCoverageTextFormats(t *testing.T) {
	cases := map[string]float64{
		"PHPUnit":   72.5,
		"SimpleCov": 87.5,
		"Elixir":    91.2,
		"tarpaulin": 50,
	}
	outputs := map[string]string{
		"PHPUnit":   "Code Coverage Report:\n Summary:\n  Lines:   72.50% (29/40)\n",
		"SimpleCov": "Line Coverage: 87.5% (35 / 40)\n",
		"Elixir":    "Coverage report:\nTotal            91.2%\n",
		"tarpaulin": "|| 50.00% covered\n",
	}
	for name, stdout := range outputs {
		t.Run(name, func(t *testing.T) {
			percent, source := Extract(t.TempDir(), config.Default(), stdout)
			if percent == nil || source != "stdout" {
				t.Fatalf("percent = %v source = %q", percent, source)
			}
			if diff := *percent - cases[name]; diff > 0.001 || diff < -0.001 {
				t.Fatalf("coverage = %v, want %v", *percent, cases[name])
			}
		})
	}
}

func TestExtractConfiguredCoverageFileWins(t *testing.T) {
	dir := t.TempDir()
	writeCoverageFile(t, dir, "coverage.xml", `<?xml version="1.0"?><coverage line-rate="0.50"/>`)
	writeCoverageFile(t, dir, "custom/report.xml", `<?xml version="1.0"?><coverage line-rate="0.91"/>`)
	cfg := config.Default()
	file := "custom/report.xml"
	cfg.Acceptance.Coverage.File = &file
	percent, source := Extract(dir, cfg, "")
	if percent == nil || source != file {
		t.Fatalf("percent = %v source = %q", percent, source)
	}
	if diff := *percent - 91.0; diff > 0.001 || diff < -0.001 {
		t.Fatalf("coverage = %v, want 91", *percent)
	}
}

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
