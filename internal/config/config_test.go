package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/soub4i/forge/internal/util"
)

func TestDefaultNormalization(t *testing.T) {
	cfg := Default()
	Normalize(&cfg)
	if cfg.Version != Version {
		t.Fatalf("version = %d, want %d", cfg.Version, Version)
	}
	if len(cfg.Models) != len(DefaultModels) {
		t.Fatalf("models = %d, want %d", len(cfg.Models), len(DefaultModels))
	}
	architect := cfg.Models["architect"]
	if architect.Model != "opencode/big-pickle" {
		t.Fatalf("architect model = %q", architect.Model)
	}
	if architect.Tier != "reasoning" {
		t.Fatalf("architect tier = %q", architect.Tier)
	}
	for _, model := range architect.Fallbacks {
		if model == architect.Model {
			t.Fatalf("fallbacks must not repeat the primary model: %v", architect.Fallbacks)
		}
	}
	if cfg.Acceptance.Security.MaxSeverity != "medium" {
		t.Fatalf("max severity = %q", cfg.Acceptance.Security.MaxSeverity)
	}
}

func TestCoverageAndBenchmarkShapes(t *testing.T) {
	var cfg Config
	raw := `{
		"acceptance": {
			"coverage": 82.5,
			"benchmarks": [{"metric": "lines_per_sec", "min": 1000, "unit": "lines/sec"}]
		}
	}`
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatal(err)
	}
	Normalize(&cfg)
	if cfg.Acceptance.Coverage.Min != 82.5 {
		t.Fatalf("coverage min = %v", cfg.Acceptance.Coverage.Min)
	}
	if !cfg.Acceptance.Benchmarks.Required {
		t.Fatal("benchmarks should be required when thresholds are present")
	}
	if len(cfg.Acceptance.Benchmarks.Thresholds) != 1 {
		t.Fatalf("thresholds = %d", len(cfg.Acceptance.Benchmarks.Thresholds))
	}
	if cfg.Acceptance.Benchmarks.Thresholds[0].Min == nil || *cfg.Acceptance.Benchmarks.Thresholds[0].Min != 1000 {
		t.Fatal("threshold minimum was not parsed")
	}
}

func TestSeverityNormalization(t *testing.T) {
	cases := map[string]string{
		"CRITICAL": "critical", "blocker": "critical", "error": "high",
		"moderate": "medium", "note": "info", "nonsense": "medium",
	}
	for input, want := range cases {
		if got := NormalizeSeverity(input, "medium"); got != want {
			t.Errorf("NormalizeSeverity(%q) = %q, want %q", input, got, want)
		}
	}
	if SeverityRank("critical") <= SeverityRank("high") {
		t.Fatal("critical must rank above high")
	}
}

func TestFingerprintFileIgnoresGoal(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "forge.config.json")
	content := `{
		"goal": "first goal",
		"acceptance": {"coverage": {"min": 70}},
		"budget": {"maxIterations": 3}
	}`
	if err := util.WriteText(file, content); err != nil {
		t.Fatal(err)
	}
	first, err := FingerprintFile(file)
	if err != nil {
		t.Fatal(err)
	}
	updated := `{
		"goal": "a completely different goal",
		"acceptance": {"coverage": {"min": 70}},
		"budget": {"maxIterations": 3}
	}`
	if err := util.WriteText(file, updated); err != nil {
		t.Fatal(err)
	}
	second, err := FingerprintFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("changing the goal must not change the integrity fingerprint")
	}
	tampered := `{
		"goal": "first goal",
		"acceptance": {"coverage": {"min": 10}},
		"budget": {"maxIterations": 3}
	}`
	if err := util.WriteText(file, tampered); err != nil {
		t.Fatal(err)
	}
	third, err := FingerprintFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Fatal("changing acceptance criteria must change the fingerprint")
	}
}

func TestFindConfigAncestor(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "forge.config.json")
	if err := os.WriteFile(file, []byte(`{"goal":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "src", "deep")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	found, err := FindConfig(nested, "")
	if err != nil {
		t.Fatal(err)
	}
	if found != file {
		t.Fatalf("found %q, want %q", found, file)
	}
}

func TestValidateRejectsBadModels(t *testing.T) {
	cfg := Default()
	cfg.Models["implementer"] = ModelEntry{Model: "not-a-model"}
	if err := Validate(&cfg, true); err == nil {
		t.Fatal("expected validation error for missing provider prefix")
	}
}

func TestLoadPrecedenceAndOverrides(t *testing.T) {
	globalHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", globalHome)
	t.Setenv("FORGE_MAX_ITERATIONS", "")
	t.Setenv("FORGE_BUDGET_USD", "")
	t.Setenv("FORGE_MAX_MINUTES", "")
	t.Setenv("FORGE_MODEL_ARCHITECT", "opencode/env-architect")
	globalDir := filepath.Join(globalHome, "forge")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, "config.json"), []byte(`{
		"budget": {"maxIterations": 5, "maxUsd": 9},
		"loop": {"repairAttempts": 3}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, "forge.config.json"), []byte(`{
		"goal": "project goal",
		"acceptance": {"coverage": 55},
		"budget": {"maxIterations": 2}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	maxIterations := 1
	budget := 3.5
	loaded, err := Load(projectDir, LoadOptions{
		RequireGoal:    true,
		MaxIterations:  &maxIterations,
		BudgetUSD:      &budget,
		ModelOverrides: map[string]string{"designer": "opencode/override-designer"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Config.Goal != "project goal" {
		t.Fatalf("goal = %q", loaded.Config.Goal)
	}
	if loaded.Config.Budget.MaxIterations != 1 || loaded.Config.Budget.MaxUSD != 3.5 {
		t.Fatalf("budget = %+v", loaded.Config.Budget)
	}
	if loaded.Config.Loop.RepairAttempts != 3 {
		t.Fatalf("global loop settings were lost: %+v", loaded.Config.Loop)
	}
	if loaded.Config.Acceptance.Coverage.Min != 55 {
		t.Fatalf("coverage = %v", loaded.Config.Acceptance.Coverage.Min)
	}
	if loaded.Config.Models["designer"].Model != "opencode/override-designer" {
		t.Fatalf("designer = %q", loaded.Config.Models["designer"].Model)
	}
	if loaded.Config.Models["architect"].Model != "opencode/env-architect" {
		t.Fatalf("env override = %q", loaded.Config.Models["architect"].Model)
	}
	if loaded.File != filepath.Join(projectDir, "forge.config.json") {
		t.Fatalf("file = %q", loaded.File)
	}
	if loaded.Fingerprint == "" {
		t.Fatal("fingerprint was not set")
	}
}

func TestLoadErrors(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("FORGE_MAX_ITERATIONS", "")
	t.Setenv("FORGE_BUDGET_USD", "")
	t.Setenv("FORGE_MAX_MINUTES", "")
	dir := t.TempDir()
	if _, err := Load(dir, LoadOptions{RequireGoal: true}); err == nil {
		t.Fatal("missing goal should fail when required")
	}
	if err := os.WriteFile(filepath.Join(dir, "forge.config.json"), []byte("{bad json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir, LoadOptions{}); err == nil {
		t.Fatal("invalid JSON should fail")
	}
	if _, err := FindConfig(dir, "missing.json"); err == nil {
		t.Fatal("missing explicit config should fail")
	}
}

func TestCoverageUnmarshalShapes(t *testing.T) {
	cases := []struct {
		raw    string
		min    float64
		file   string
		hasNil bool
	}{
		{raw: `75`, min: 75},
		{raw: `"80.5"`, min: 80.5},
		{raw: `{"threshold": 60}`, min: 60},
		{raw: `{"percent": 42, "file": "coverage.json"}`, min: 42, file: "coverage.json"},
		{raw: `false`, min: 0},
		{raw: `null`, min: 0},
	}
	for _, test := range cases {
		var wrapper struct {
			Coverage Coverage `json:"coverage"`
		}
		if err := json.Unmarshal([]byte(`{"coverage":`+test.raw+`}`), &wrapper); err != nil {
			t.Fatalf("%s: %v", test.raw, err)
		}
		coverage := wrapper.Coverage
		if coverage.Min != test.min {
			t.Errorf("%s: min = %v, want %v", test.raw, coverage.Min, test.min)
		}
		if test.file == "" && coverage.File != nil {
			t.Errorf("%s: file = %v, want nil", test.raw, *coverage.File)
		}
		if test.file != "" && (coverage.File == nil || *coverage.File != test.file) {
			t.Errorf("%s: file = %v", test.raw, coverage.File)
		}
	}
}

func TestBenchmarksUnmarshalShapes(t *testing.T) {
	var object struct {
		Benchmarks Benchmarks `json:"benchmarks"`
	}
	object.Benchmarks = Benchmarks{ResultsFile: "bench/results.json"}
	if err := json.Unmarshal([]byte(`{"benchmarks":{"required":true,"resultsFile":"out.json","thresholds":[{"metric":"ops","min":10}]}}`), &object); err != nil {
		t.Fatal(err)
	}
	if !object.Benchmarks.Required || object.Benchmarks.ResultsFile != "out.json" || len(object.Benchmarks.Thresholds) != 1 {
		t.Fatalf("benchmarks = %+v", object.Benchmarks)
	}

	var array Benchmarks
	if err := json.Unmarshal([]byte(`[{"metric":"lines_per_sec","min":1000}, {"metric":"latency","max":10,"unit":"ms"}]`), &array); err != nil {
		t.Fatal(err)
	}
	if !array.Required || len(array.Thresholds) != 2 || array.ResultsFile != "bench/results.json" {
		t.Fatalf("array benchmarks = %+v", array)
	}

	var objectWithoutRequired Benchmarks
	if err := json.Unmarshal([]byte(`{"thresholds":[{"metric":"ops"}]}`), &objectWithoutRequired); err != nil {
		t.Fatal(err)
	}
	if !objectWithoutRequired.Required {
		t.Fatal("thresholds without required should still require benchmarks")
	}

	var disabled Benchmarks
	if err := json.Unmarshal([]byte(`false`), &disabled); err != nil {
		t.Fatal(err)
	}
	if disabled.Required || len(disabled.Thresholds) != 0 {
		t.Fatalf("disabled benchmarks = %+v", disabled)
	}

	var invalid Benchmarks
	invalid.ResultsFile = "keep.json"
	if err := json.Unmarshal([]byte(`"yes"`), &invalid); err != nil {
		t.Fatal(err)
	}
	if invalid.ResultsFile != "keep.json" {
		t.Fatalf("scalar benchmarks should keep defaults: %+v", invalid)
	}
}

func TestCommandAndModelUnmarshal(t *testing.T) {
	var cfg Config
	if err := json.Unmarshal([]byte(`{
		"commands": {
			"test": "npm test",
			"build": {"command": "make build", "optional": true, "timeoutMs": 100}
		},
		"models": {
			"architect": "opencode/simple",
			"designer": {"model": "opencode/rich", "tier": "reasoning", "fallbacks": ["opencode/backup"]}
		}
	}`), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Commands.Test == nil || cfg.Commands.Test.Command != "npm test" {
		t.Fatalf("test command = %+v", cfg.Commands.Test)
	}
	if cfg.Commands.Build == nil || !cfg.Commands.Build.Optional || cfg.Commands.Build.TimeoutMs != 100 {
		t.Fatalf("build command = %+v", cfg.Commands.Build)
	}
	if cfg.Models["architect"].Model != "opencode/simple" {
		t.Fatalf("architect = %+v", cfg.Models["architect"])
	}
	if cfg.Models["designer"].Tier != "reasoning" || len(cfg.Models["designer"].Fallbacks) != 1 {
		t.Fatalf("designer = %+v", cfg.Models["designer"])
	}
}

func TestWriteFileAndPricingLookup(t *testing.T) {
	dir := t.TempDir()
	cfg := Default()
	cfg.Goal = "  write me  "
	cfg.Pricing["opencode/big-pickle"] = Pricing{Input: 1, Output: 2}
	file := filepath.Join(dir, "forge.config.json")
	if err := WriteFile(file, cfg); err != nil {
		t.Fatal(err)
	}
	var written map[string]any
	if err := util.ReadJSON(file, &written); err != nil {
		t.Fatal(err)
	}
	if written["$schema"] != "./.forge/forge.schema.json" {
		t.Fatalf("schema = %v", written["$schema"])
	}
	if written["goal"] != "write me" {
		t.Fatalf("goal = %v", written["goal"])
	}

	cfg.Pricing["opencode/big-pickle#high"] = Pricing{Input: 5, Output: 6}
	if got := cfg.PricingFor("opencode/big-pickle#high"); got.Input != 5 {
		t.Fatalf("exact pricing = %+v", got)
	}
	if got := cfg.PricingFor("opencode/big-pickle#low"); got.Input != 1 {
		t.Fatalf("base pricing = %+v", got)
	}
	cfg.Models["architect"] = ModelEntry{Model: "opencode/custom", Pricing: &Pricing{Input: 7}}
	if got := cfg.PricingFor("opencode/custom#x"); got.Input != 7 {
		t.Fatalf("model pricing = %+v", got)
	}
	if got := cfg.PricingFor("unknown/model"); got != (Pricing{}) {
		t.Fatalf("unknown pricing = %+v", got)
	}
}

func TestValidateIssues(t *testing.T) {
	cfg := Default()
	cfg.Acceptance.Coverage.Min = 140
	cfg.Acceptance.Security.MaxSeverity = "nope"
	cfg.Budget.MaxUSD = -1
	cfg.Commands.Test = &Command{}
	err := Validate(&cfg, false)
	if err == nil {
		t.Fatal("expected validation errors")
	}
	configErr, ok := err.(*ConfigError)
	if !ok {
		t.Fatalf("error type = %T", err)
	}
	joined := strings.Join(configErr.Issues, "\n")
	for _, want := range []string{"between 0 and 100", "Unknown security severity", "budget values", "commands.test"} {
		if !strings.Contains(joined, want) {
			t.Errorf("issues are missing %q:\n%s", want, joined)
		}
	}
}

func TestModelCandidatesAndStageNames(t *testing.T) {
	cfg := Default()
	cfg.Models["tester"] = ModelEntry{
		Model: "opencode/primary", Fallbacks: []string{"opencode/primary", "opencode/secondary", ""},
	}
	candidates := cfg.ModelCandidates("tester", "")
	if len(candidates) != 2 || candidates[0] != "opencode/primary" || candidates[1] != "opencode/secondary" {
		t.Fatalf("candidates = %v", candidates)
	}
	override := cfg.ModelCandidates("tester", "opencode/override")
	if override[0] != "opencode/override" {
		t.Fatalf("override candidates = %v", override)
	}
	if got := cfg.ModelCandidates("missing", ""); got != nil {
		t.Fatalf("unknown stage candidates = %v", got)
	}
	names := StageNames()
	if len(names) != len(DefaultModels) || names[0] != "architect" {
		t.Fatalf("stage names = %v", names)
	}
}
