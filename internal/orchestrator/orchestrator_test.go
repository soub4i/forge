package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/soub4i/forge/internal/benchmark"
	"github.com/soub4i/forge/internal/config"
	"github.com/soub4i/forge/internal/gates"
	"github.com/soub4i/forge/internal/reports"
	"github.com/soub4i/forge/internal/security"
	"github.com/soub4i/forge/internal/util"
)

func TestCheckConfigIntegrity(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "forge.config.json")
	if err := os.WriteFile(file, []byte(`{"acceptance":{"tests":true},"budget":{"maxIterations":2}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	hash, err := config.FingerprintFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if result := checkConfigIntegrity(file, hash); !result.OK {
		t.Fatalf("unchanged config should pass: %+v", result)
	}
	if err := os.WriteFile(file, []byte(`{"acceptance":{"tests":false},"budget":{"maxIterations":2}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if result := checkConfigIntegrity(file, hash); result.OK {
		t.Fatal("tampered acceptance must fail integrity")
	}
	if result := checkConfigIntegrity("", hash); !result.OK {
		t.Fatal("no config file should skip integrity")
	}
}

func TestCompileFeedback(t *testing.T) {
	det := gates.Deterministic{
		OK: false, CodeOK: false,
		Commands:  []gates.GateCommand{{Name: "test", Status: "fail", Command: "npm test", LogFile: "log.txt"}},
		Coverage:  gates.CoverageResult{Skipped: true},
		Benchmark: benchmarkZero(),
		Security:  securityZero(),
	}
	text := compileFeedback(&det, nil, nil, nil)
	for _, want := range []string{"Failure feedback", "Fix the `test` gate", "Required next actions"} {
		if !strings.Contains(text, want) {
			t.Errorf("feedback is missing %q:\n%s", want, text)
		}
	}
}

func TestHistorySummary(t *testing.T) {
	det := &gates.Deterministic{
		CodeOK: false,
		Commands: []gates.GateCommand{
			{Name: "test", Status: "fail", OK: false},
			{Name: "lint", Status: "pass", OK: true},
		},
		Coverage:  gates.CoverageResult{Skipped: true},
		Benchmark: benchmarkZero(),
	}
	got := historySummary(2, det, nil, nil)
	if !strings.Contains(got, "Iteration 2") || !strings.Contains(got, "fail (test)") {
		t.Fatalf("history = %q", got)
	}
}

func TestModelOverrideForStage(t *testing.T) {
	cfg := config.Default()
	cfg.Models["architect"] = config.ModelEntry{Model: "opencode/big-pickle"}
	if got := modelOverrideForStage(cfg, "implementer", false); got != "" {
		t.Fatalf("no escalation should not override: %q", got)
	}
	if got := modelOverrideForStage(cfg, "implementer", true); got != "opencode/big-pickle" {
		t.Fatalf("override = %q", got)
	}
	if got := modelOverrideForStage(cfg, "tester", true); got != "" {
		t.Fatalf("tester must not escalate: %q", got)
	}
}

func TestToReportIterations(t *testing.T) {
	iterations := []IterationState{{
		Number: 1, StartedAt: "start", EndedAt: "end",
		Stages:       []StageResult{{Stage: "architect", Model: "m", OK: true, Tokens: tokenZero()}},
		SecurityGate: &security.Gate{OK: true, MaxAllowed: "medium"},
	}}
	report := toReportIterations(iterations)
	if len(report) != 1 || len(report[0].Stages) != 1 {
		t.Fatalf("report = %+v", report)
	}
	if report[0].SecurityGate == nil || !report[0].SecurityGate.OK {
		t.Fatal("security gate was not copied")
	}
}

func benchmarkZero() benchmark.Result {
	return benchmark.Result{}
}

func securityZero() gates.SecurityResult {
	return gates.SecurityResult{OK: true, Skipped: true}
}

func tokenZero() util.Tokens {
	return util.Tokens{}
}

func fakeModelBinary(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
if [ "$1" = "models" ]; then
  exit 0
fi
echo '{"sessionID":"ses_fake","type":"text","part":{"text":"stage complete"}}'
echo '{"type":"step_finish","part":{"cost":0,"tokens":{"input":10,"output":5,"reasoning":1}}}'
exit 0
`
	file := filepath.Join(dir, "opencode")
	if err := os.WriteFile(file, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func harnessConfig() config.Config {
	cfg := config.Default()
	cfg.Goal = "Build a test tool"
	cfg.Acceptance = config.Acceptance{
		Tests:      true,
		Coverage:   config.Coverage{Min: 0},
		Benchmarks: config.Benchmarks{Required: false, ResultsFile: "bench/results.json"},
		Security:   config.Security{Enabled: false},
		Review:     config.Review{Enabled: false},
	}
	cfg.Commands.Test = &config.Command{Command: "true"}
	cfg.Budget = config.Budget{MaxIterations: 1}
	cfg.Loop = config.Loop{RepairAttempts: 0, EscalateAfter: 1, StageTimeoutMinutes: 1, ModelRetries: 1}
	cfg.Release.Enabled = false
	return cfg
}

func TestRunHarnessSuccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake model binary uses a shell script")
	}
	fakeModelBinary(t)
	dir := t.TempDir()
	result, err := RunHarness(context.Background(), Options{Cwd: dir, Config: harnessConfig()})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success || result.Report == nil {
		t.Fatalf("result = %+v", result)
	}
	if result.Report.Status != "success" || result.ExitReason != "" {
		t.Fatalf("report = %+v", result.Report)
	}
	if len(result.Report.Iterations) != 1 {
		t.Fatalf("iterations = %d", len(result.Report.Iterations))
	}
	var stages []string
	for _, stage := range result.Report.Iterations[0].Stages {
		stages = append(stages, stage.Stage)
	}
	want := "architect,designer,implementer,tester"
	if strings.Join(stages, ",") != want {
		t.Fatalf("stages = %v, want %s", stages, want)
	}
	if !util.FileExists(result.ReportFiles.JSONFile) || !util.FileExists(result.ReportFiles.MarkdownFile) {
		t.Fatal("report files were not written")
	}
	state := reports.ReadState(dir)
	if len(state.Runs) != 1 || state.Runs[0].Status != "success" {
		t.Fatalf("state = %+v", state)
	}
	if len(result.Report.Ledger.Entries) != 4 {
		t.Fatalf("ledger entries = %d", len(result.Report.Ledger.Entries))
	}
}

func TestRunHarnessGateFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake model binary uses a shell script")
	}
	fakeModelBinary(t)
	dir := t.TempDir()
	cfg := harnessConfig()
	cfg.Commands.Test = &config.Command{Command: "false"}
	result, err := RunHarness(context.Background(), Options{Cwd: dir, Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	if result.Success {
		t.Fatal("a failing gate must not succeed")
	}
	if result.ExitReason != "max_iterations" {
		t.Fatalf("exit reason = %q", result.ExitReason)
	}
	if result.Report == nil || len(result.Report.Iterations) != 1 {
		t.Fatalf("report = %+v", result.Report)
	}
	feedback := result.Report.Iterations[0].FeedbackFile
	if feedback == "" || !util.FileExists(filepath.Join(dir, feedback)) {
		t.Fatalf("feedback file = %q", feedback)
	}
	content := util.ReadText(filepath.Join(dir, feedback))
	if !strings.Contains(content, "Fix the `test` gate") {
		t.Fatalf("feedback = %s", content)
	}
}

func TestRunHarnessInstallFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake model binary uses a shell script")
	}
	fakeModelBinary(t)
	dir := t.TempDir()
	cfg := harnessConfig()
	cfg.Commands.Install = &config.Command{Command: "false"}
	result, err := RunHarness(context.Background(), Options{Cwd: dir, Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	if result.Success || result.ExitReason != "install_failed" {
		t.Fatalf("result = %+v", result)
	}
	if result.Report == nil || len(result.Report.Iterations) != 0 {
		t.Fatalf("report = %+v", result.Report)
	}
	if result.Report.Status != "failed" || !strings.Contains(result.Report.Error, "Install command failed") {
		t.Fatalf("report = %+v", result.Report)
	}
}

func TestRunHarnessStageFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake model binary uses a shell script")
	}
	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()
	result, err := RunHarness(context.Background(), Options{Cwd: dir, Config: harnessConfig()})
	if err != nil {
		t.Fatal(err)
	}
	if result.Success || result.ExitReason != "stage_failed" {
		t.Fatalf("result = %+v", result)
	}
	if result.Report == nil || result.Report.Status != "failed" {
		t.Fatalf("report = %+v", result.Report)
	}
}
