package gates

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/soub4i/forge/internal/benchmark"
	"github.com/soub4i/forge/internal/config"
	"github.com/soub4i/forge/internal/security"
	"github.com/soub4i/forge/internal/util"
)

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDetectNodeScripts(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{
		"name": "demo",
		"scripts": {
			"test": "node --test",
			"test:coverage": "node --test --experimental-test-coverage",
			"lint": "node --check src/index.js",
			"build": "node build.js",
			"bench": "node bench/run.js"
		}
	}`)
	detected := DetectProject(dir)
	if detected.Language != "node" {
		t.Fatalf("language = %q", detected.Language)
	}
	if detected.Commands["test"].Command != "npm run test" {
		t.Fatalf("test command = %q", detected.Commands["test"].Command)
	}
	if detected.Commands["coverage"].Command != "npm run test:coverage" {
		t.Fatalf("coverage command = %q", detected.Commands["coverage"].Command)
	}
	if detected.Commands["benchmark"].Command != "npm run bench" {
		t.Fatalf("benchmark command = %q", detected.Commands["benchmark"].Command)
	}
}

func TestDetectGoProject(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module demo\n\ngo 1.23\n")
	detected := DetectProject(dir)
	if detected.Language != "go" {
		t.Fatalf("language = %q", detected.Language)
	}
	if detected.Commands["test"].Command != "go test ./..." {
		t.Fatalf("test command = %q", detected.Commands["test"].Command)
	}
}

func TestDetectMakefileTargets(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Makefile", "test:\n\tgo test ./...\n\nbench:\n\tgo test -bench . ./...\n")
	detected := DetectProject(dir)
	if detected.Commands["test"] == nil || detected.Commands["test"].Command != "make test" {
		t.Fatalf("test command = %+v", detected.Commands["test"])
	}
	if detected.Commands["benchmark"] == nil || detected.Commands["benchmark"].Command != "make bench" {
		t.Fatalf("benchmark command = %+v", detected.Commands["benchmark"])
	}
}

func TestResolveCommandsConfigWins(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"scripts":{"test":"node --test"}}`)
	cfg := config.Default()
	cfg.Commands.Test = &config.Command{Command: "npm run test:ci", Optional: true}
	resolved := ResolveCommands(cfg, dir)
	if resolved.Commands["test"].Command != "npm run test:ci" {
		t.Fatalf("test command = %q", resolved.Commands["test"].Command)
	}
	if resolved.Sources["test"] != "config" {
		t.Fatalf("source = %q", resolved.Sources["test"])
	}
	if !resolved.Commands["test"].Optional {
		t.Fatal("optional flag was not preserved")
	}
}

func TestResolveCommandsMissing(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	resolved := ResolveCommands(cfg, dir)
	if resolved.Commands["test"] != nil {
		t.Fatal("expected no test command in an empty directory")
	}
	if resolved.Sources["test"] != "missing" {
		t.Fatalf("source = %q", resolved.Sources["test"])
	}
}

func TestGateSignatureStable(t *testing.T) {
	det := Deterministic{
		Commands: []GateCommand{
			{Name: "test", Status: "fail", OK: false, ExitCode: 1},
			{Name: "lint", Status: "fail", OK: false, ExitCode: 2},
		},
		Coverage: CoverageResult{Skipped: true},
	}
	first := GateSignature(&det, nil, nil)
	second := GateSignature(&det, nil, nil)
	if first != second {
		t.Fatalf("signature is not stable: %q vs %q", first, second)
	}
	if first == "ok" {
		t.Fatal("failing gates must not have an ok signature")
	}
}

func TestReviewGateInferredBlocking(t *testing.T) {
	cfg := config.Default()
	report := &ReviewReport{
		Verdict: "changes_requested",
		Findings: []ReviewFinding{
			{Severity: "high", Title: "bug"},
			{Severity: "low", Title: "nit"},
		},
	}
	gate := ReviewGateFor(cfg, report)
	if gate.OK {
		t.Fatal("high finding should block")
	}
	if len(gate.Blocking) != 1 || gate.Blocking[0].Title != "bug" {
		t.Fatalf("blocking = %+v", gate.Blocking)
	}
}

func TestReviewGateApprove(t *testing.T) {
	cfg := config.Default()
	report := &ReviewReport{Verdict: "approve", Findings: []ReviewFinding{}}
	gate := ReviewGateFor(cfg, report)
	if !gate.OK {
		t.Fatalf("expected approval: %+v", gate)
	}
}

func TestReviewGateMissingReport(t *testing.T) {
	cfg := config.Default()
	gate := ReviewGateFor(cfg, nil)
	if gate.OK || gate.Verdict != "missing" || len(gate.Blocking) != 1 {
		t.Fatalf("gate = %+v", gate)
	}
	if !strings.Contains(FormatReviewGate(gate), "did not produce a valid") {
		t.Fatalf("format = %s", FormatReviewGate(gate))
	}
}

func TestReviewGateExplicitBlockingWins(t *testing.T) {
	cfg := config.Default()
	report := &ReviewReport{
		Verdict: "changes_requested",
		Findings: []ReviewFinding{
			{Severity: "low", Title: "explicit blocker", Blocking: true, File: "a.js", Line: 4, Recommendation: "fix it"},
			{Severity: "critical", Title: "inferred blocker"},
		},
	}
	gate := ReviewGateFor(cfg, report)
	if gate.OK || len(gate.Blocking) != 1 || gate.Blocking[0].Title != "explicit blocker" {
		t.Fatalf("gate = %+v", gate)
	}
	formatted := FormatReviewGate(gate)
	for _, want := range []string{"CHANGES REQUESTED", "explicit blocker", "a.js:4", "fix it"} {
		if !strings.Contains(formatted, want) {
			t.Errorf("format is missing %q:\n%s", want, formatted)
		}
	}
}

func TestFormatSecurityGate(t *testing.T) {
	gate := security.Gate{
		OK: false, MaxAllowed: "medium",
		Findings: []security.Finding{{Severity: "high", Title: "injection", File: "src/a.js", Line: 2}},
		Blocking: []security.Finding{{Severity: "high", Title: "injection", File: "src/a.js", Line: 2}},
	}
	formatted := FormatSecurityGate(gate)
	for _, want := range []string{"Security review", "verdict: FAIL", "max allowed severity: medium", "blocking: 1", "injection"} {
		if !strings.Contains(formatted, want) {
			t.Errorf("format is missing %q:\n%s", want, formatted)
		}
	}
	clean := FormatSecurityGate(security.Gate{OK: true, MaxAllowed: "medium"})
	if !strings.Contains(clean, "verdict: PASS") {
		t.Fatalf("clean format = %s", clean)
	}
}

func TestEvaluateDeterministicPassingCommands(t *testing.T) {
	dir := t.TempDir()
	cfg := baseGateConfig()
	cfg.Commands.Test = &config.Command{Command: "true"}
	runDir := t.TempDir()
	det := EvaluateDeterministic(context.Background(), cfg, dir, runDir, 1, nil)
	if !det.OK || !det.CodeOK {
		t.Fatalf("det = %+v", det)
	}
	if len(det.Commands) != 5 {
		t.Fatalf("commands = %+v", det.Commands)
	}
	if det.Commands[3].Name != "test" || !det.Commands[3].OK {
		t.Fatalf("test command = %+v", det.Commands[3])
	}
	if !util.FileExists(filepath.Join(runDir, "commands", "01-test.log")) {
		t.Fatal("command log was not written")
	}
	if summary := FormatGateSummary(det); !strings.Contains(summary, "test: pass") {
		t.Fatalf("summary = %q", summary)
	}
}

func TestEvaluateDeterministicFailingAndOptionalCommands(t *testing.T) {
	cfg := baseGateConfig()
	cfg.Commands.Test = &config.Command{Command: "echo boom >&2; exit 3"}
	det := EvaluateDeterministic(context.Background(), cfg, t.TempDir(), t.TempDir(), 2, nil)
	if det.OK || det.CodeOK {
		t.Fatalf("det = %+v", det)
	}
	testCommand := det.Commands[3]
	if testCommand.ExitCode != 3 || testCommand.Status != "fail" {
		t.Fatalf("test command = %+v", testCommand)
	}
	report := FormatGateReport(det, true, 2000)
	for _, want := range []string{"- test: FAIL", "boom"} {
		if !strings.Contains(report, want) {
			t.Errorf("report is missing %q:\n%s", want, report)
		}
	}

	optional := baseGateConfig()
	optional.Commands.Test = &config.Command{Command: "exit 3", Optional: true}
	optionalDet := EvaluateDeterministic(context.Background(), optional, t.TempDir(), t.TempDir(), 1, nil)
	if !optionalDet.OK {
		t.Fatalf("optional failure must not block: %+v", optionalDet)
	}
	if optionalDet.Commands[3].Status != "warn" {
		t.Fatalf("optional status = %q", optionalDet.Commands[3].Status)
	}
}

func TestEvaluateDeterministicCoverage(t *testing.T) {
	cfg := baseGateConfig()
	cfg.Commands.Test = &config.Command{Command: "true"}
	cfg.Acceptance.Coverage = config.Coverage{Min: 80}
	cfg.Commands.Coverage = &config.Command{Command: `echo "coverage: 85.2%"`}
	det := EvaluateDeterministic(context.Background(), cfg, t.TempDir(), t.TempDir(), 1, nil)
	if !det.OK || det.Coverage.Skipped || !det.Coverage.OK {
		t.Fatalf("det = %+v", det)
	}
	if det.Coverage.Percent == nil || *det.Coverage.Percent != 85.2 {
		t.Fatalf("percent = %v", det.Coverage.Percent)
	}

	cfg.Acceptance.Coverage.Min = 90
	failing := EvaluateDeterministic(context.Background(), cfg, t.TempDir(), t.TempDir(), 1, nil)
	if failing.OK || failing.Coverage.OK {
		t.Fatalf("failing coverage = %+v", failing)
	}
	if failing.Commands[4].Status != "fail" {
		t.Fatalf("coverage command = %+v", failing.Commands[4])
	}

	missing := baseGateConfig()
	missing.Commands.Test = &config.Command{Command: "true"}
	missing.Acceptance.Coverage = config.Coverage{Min: 10}
	missingDet := EvaluateDeterministic(context.Background(), missing, t.TempDir(), t.TempDir(), 1, nil)
	if missingDet.OK || missingDet.Coverage.OK || missingDet.Coverage.Skipped {
		t.Fatalf("missing coverage command = %+v", missingDet)
	}
}

func TestEvaluateDeterministicBenchmark(t *testing.T) {
	cfg := baseGateConfig()
	cfg.Commands.Test = &config.Command{Command: "true"}
	cfg.Acceptance.Benchmarks = config.Benchmarks{
		Required: true, ResultsFile: "bench/results.json",
		Thresholds: []config.Threshold{{Metric: "ops", Min: floatPointer(100)}},
	}
	cfg.Commands.Benchmark = &config.Command{Command: `mkdir -p bench && printf '{"metrics":{"ops":50}}' > bench/results.json`}
	det := EvaluateDeterministic(context.Background(), cfg, t.TempDir(), t.TempDir(), 1, nil)
	if det.OK || det.Benchmark.OK {
		t.Fatalf("det = %+v", det)
	}
	if len(det.Benchmark.Failures) != 1 || det.Benchmark.Failures[0].Reason != "below_min" {
		t.Fatalf("failures = %+v", det.Benchmark.Failures)
	}
	if !strings.Contains(FormatGateSummary(det), "benchmark: fail") {
		t.Fatalf("summary = %q", FormatGateSummary(det))
	}

	cfg.Commands.Benchmark = &config.Command{Command: `mkdir -p bench && printf '{"metrics":{"ops":150}}' > bench/results.json`}
	passing := EvaluateDeterministic(context.Background(), cfg, t.TempDir(), t.TempDir(), 1, nil)
	if !passing.OK || !passing.Benchmark.OK {
		t.Fatalf("passing benchmark = %+v", passing)
	}
}

func TestEvaluateDeterministicMissingBenchmark(t *testing.T) {
	cfg := baseGateConfig()
	cfg.Commands.Test = &config.Command{Command: "true"}
	cfg.Acceptance.Benchmarks = config.Benchmarks{
		Required: true, Thresholds: []config.Threshold{{Metric: "ops", Min: floatPointer(1)}},
	}
	det := EvaluateDeterministic(context.Background(), cfg, t.TempDir(), t.TempDir(), 1, nil)
	if det.OK || det.Benchmark.OK || len(det.Benchmark.Failures) != 1 || det.Benchmark.Failures[0].Reason != "missing_command" {
		t.Fatalf("det = %+v", det)
	}
}

func TestEvaluateDeterministicSecrets(t *testing.T) {
	cfg := baseGateConfig()
	cfg.Commands.Test = &config.Command{Command: "true"}
	cfg.Acceptance.Security = config.Security{Enabled: true, MaxSeverity: "medium", RequireNoSecret: true}
	dir := t.TempDir()
	writeFile(t, dir, "app.js", `const key = "AKIAIOSFODNN7EXAMPLE";`) // forge:allow-secret
	det := EvaluateDeterministic(context.Background(), cfg, dir, t.TempDir(), 1, nil)
	if det.OK || det.Security.OK || det.Security.Skipped {
		t.Fatalf("det = %+v", det)
	}
	if len(det.Security.Secrets.Findings) != 1 {
		t.Fatalf("secrets = %+v", det.Security.Secrets)
	}
	if !strings.Contains(FormatGateSummary(det), "security: fail") {
		t.Fatalf("summary = %q", FormatGateSummary(det))
	}

	cleanDir := t.TempDir()
	writeFile(t, cleanDir, "app.js", "console.log(1)\n")
	clean := EvaluateDeterministic(context.Background(), cfg, cleanDir, t.TempDir(), 1, nil)
	if !clean.OK || !clean.Security.OK || clean.Security.Skipped {
		t.Fatalf("clean security = %+v", clean)
	}
}

func TestRunInstallAndBootstrapChecks(t *testing.T) {
	cfg := baseGateConfig()
	skip := RunInstall(context.Background(), cfg, t.TempDir(), t.TempDir(), nil)
	if skip.Status != "skip" || !skip.OK {
		t.Fatalf("skip install = %+v", skip)
	}
	cfg.Commands.Install = &config.Command{Command: "true"}
	installed := RunInstall(context.Background(), cfg, t.TempDir(), t.TempDir(), nil)
	if !installed.OK || installed.Status != "pass" {
		t.Fatalf("install = %+v", installed)
	}
	if !NeedsTestBootstrap(config.Default(), Resolved{Commands: map[string]*CommandSpec{}}) {
		t.Fatal("missing test command should need bootstrap")
	}
	withTest := config.Default()
	withTest.Commands.Test = &config.Command{Command: "true"}
	if NeedsTestBootstrap(withTest, Resolved{Commands: map[string]*CommandSpec{"test": {Command: "true"}}}) {
		t.Fatal("configured test command should not need bootstrap")
	}
	bootstrap := config.Default()
	bootstrap.Acceptance.Benchmarks = config.Benchmarks{Required: true}
	if !NeedsBenchmarkBootstrap(bootstrap, Resolved{Commands: map[string]*CommandSpec{}}) {
		t.Fatal("missing benchmark command should need bootstrap")
	}
}

func TestGateSignatureIncludesSecurityAndReview(t *testing.T) {
	det := Deterministic{
		Commands:  []GateCommand{{Name: "test", Status: "fail", OK: false, ExitCode: 1}},
		Coverage:  CoverageResult{Skipped: true},
		Benchmark: benchmark.Result{},
		Security:  SecurityResult{Skipped: true},
	}
	sec := &security.Gate{Blocking: []security.Finding{{Title: "injection"}}}
	rev := &ReviewGate{Blocking: []ReviewFinding{{Title: "missing test"}}}
	signature := GateSignature(&det, sec, rev)
	for _, want := range []string{"test:1", "sec:injection", "rev:missing test"} {
		if !strings.Contains(signature, want) {
			t.Errorf("signature is missing %q: %q", want, signature)
		}
	}
	if got := GateSignature(nil, nil, nil); got != "ok" {
		t.Fatalf("empty signature = %q", got)
	}
}

func baseGateConfig() config.Config {
	cfg := config.Default()
	cfg.Goal = "gate test"
	cfg.Acceptance = config.Acceptance{
		Tests:      true,
		Coverage:   config.Coverage{Min: 0},
		Benchmarks: config.Benchmarks{Required: false, ResultsFile: "bench/results.json"},
		Security:   config.Security{Enabled: false},
		Review:     config.Review{Enabled: false},
	}
	return cfg
}

func floatPointer(value float64) *float64 { return &value }
