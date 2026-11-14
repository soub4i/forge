package reports

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/soub4i/forge/internal/benchmark"
	"github.com/soub4i/forge/internal/config"
	"github.com/soub4i/forge/internal/gates"
	"github.com/soub4i/forge/internal/models"
	"github.com/soub4i/forge/internal/util"
)

func writeReportFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadReviewReportFromFile(t *testing.T) {
	dir := t.TempDir()
	writeReportFile(t, dir, ".forge/review.json", `{
		"verdict": "approve",
		"summary": "looks good",
		"design_change_needed": false,
		"findings": [
			{"severity": "low", "blocking": false, "title": "nit", "file": "src/a.js", "line": 3, "recommendation": "rename"}
		]
	}`)
	report := LoadReviewReport(dir, "")
	if report.Missing {
		t.Fatal("report should not be missing")
	}
	if report.Verdict != "approve" || report.Summary != "looks good" {
		t.Fatalf("report = %+v", report)
	}
	if len(report.Findings) != 1 || report.Findings[0].Severity != "low" {
		t.Fatalf("findings = %+v", report.Findings)
	}
}

func TestLoadReviewReportFromStageText(t *testing.T) {
	dir := t.TempDir()
	text := "Here is the result:\n```json\n{\"verdict\":\"changes_requested\",\"findings\":[]}\n```"
	report := LoadReviewReport(dir, text)
	if report.Missing || report.Verdict != "changes_requested" {
		t.Fatalf("report = %+v", report)
	}
}

func TestLoadReviewReportMissing(t *testing.T) {
	report := LoadReviewReport(t.TempDir(), "")
	if !report.Missing {
		t.Fatal("expected a missing report")
	}
	if report.Verdict != "missing" {
		t.Fatalf("verdict = %q", report.Verdict)
	}
}

func TestLoadSecurityReport(t *testing.T) {
	dir := t.TempDir()
	writeReportFile(t, dir, ".forge/security.json", `{
		"verdict": "pass",
		"summary": "no issues",
		"findings": []
	}`)
	report := LoadSecurityReport(dir, "")
	if report.Missing || report.Verdict != "pass" {
		t.Fatalf("report = %+v", report)
	}
}

func TestLoadSecurityReportRejectsInvalidShape(t *testing.T) {
	dir := t.TempDir()
	writeReportFile(t, dir, ".forge/security.json", `{"verdict":"pass"}`)
	report := LoadSecurityReport(dir, "")
	if !report.Missing {
		t.Fatal("a report without a findings array must be rejected")
	}
}

func TestEnsureGoalDoc(t *testing.T) {
	dir := t.TempDir()
	min := 70.0
	cfg := config.Default()
	cfg.Goal = "Build a thing"
	cfg.Acceptance.Coverage.Min = min
	cfg.Acceptance.Benchmarks.Thresholds = []config.Threshold{{Metric: "throughput", Min: &min, Unit: "ops/sec"}}
	file, err := EnsureGoalDoc(dir, cfg)
	if err != nil {
		t.Fatal(err)
	}
	content := util.ReadText(file)
	for _, want := range []string{"Build a thing", "Coverage is at least 70%", "throughput", "Independent review"} {
		if !strings.Contains(content, want) {
			t.Errorf("goal doc is missing %q:\n%s", want, content)
		}
	}
}

func TestBuildReportAndRender(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Goal = "Ship the tool"
	run := CreateRun(dir, cfg, "")
	ledger := models.NewLedger(cfg.Budget)
	ledger.Add(cfg, "architect", 1, "opencode/big-pickle", 0, util.Tokens{Input: 100, Output: 50}, 1234)
	det := gates.Deterministic{
		OK: true, CodeOK: true, Cwd: dir,
		Commands:  []gates.GateCommand{{Name: "test", Status: "pass", OK: true, Message: "passed"}},
		Coverage:  gates.CoverageResult{Skipped: true, OK: true},
		Benchmark: benchmarkZero(),
		Security:  securityZero(),
	}
	report := BuildReport(BuildInput{
		Run: run, Config: cfg, Ledger: ledger, Success: true,
		FinalGates: &det,
		Iterations: []ReportIteration{{
			Number: 1, StartedAt: run.StartedAt, EndedAt: run.StartedAt,
			Stages: []ReportStage{{Stage: "architect", Model: "opencode/big-pickle", OK: true, Tokens: util.Tokens{Input: 100, Output: 50}}},
			Gates:  SanitizeDeterministic(&det),
		}},
	})
	if report.Status != "success" {
		t.Fatalf("status = %q", report.Status)
	}
	markdown := RenderReportMarkdown(report)
	for _, want := range []string{"# Forge run " + run.ID, "Ship the tool", "| 1 | architect |", "## Final gates"} {
		if !strings.Contains(markdown, want) {
			t.Errorf("report is missing %q:\n%s", want, markdown)
		}
	}
	files, err := WriteRunReport(run, report)
	if err != nil {
		t.Fatal(err)
	}
	if !util.FileExists(files.JSONFile) || !util.FileExists(files.MarkdownFile) {
		t.Fatal("report files were not written")
	}
	RecordRun(dir, report, files)
	state := ReadState(dir)
	if len(state.Runs) != 1 || state.Runs[0].ID != run.ID {
		t.Fatalf("state = %+v", state)
	}
}

func benchmarkZero() benchmark.Result {
	return benchmark.Result{OK: true, Skipped: true, Metrics: map[string]float64{}}
}

func securityZero() gates.SecurityResult {
	return gates.SecurityResult{OK: true, Skipped: true}
}

func TestSanitizeDeterministicRelativeLog(t *testing.T) {
	dir := t.TempDir()
	det := &gates.Deterministic{
		OK: false, CodeOK: false, Cwd: dir,
		Commands: []gates.GateCommand{{
			Name: "test", Status: "fail", LogFile: filepath.Join(dir, ".forge", "commands", "01-test.log"),
		}},
		Coverage:  gates.CoverageResult{Skipped: true},
		Benchmark: benchmarkZero(),
		Security:  securityZero(),
	}
	view := SanitizeDeterministic(det)
	if view == nil || len(view.Commands) != 1 {
		t.Fatalf("view = %+v", view)
	}
	if filepath.IsAbs(view.Commands[0].LogFile) {
		t.Fatalf("log file should be relative: %q", view.Commands[0].LogFile)
	}
}

func TestFormatLastRun(t *testing.T) {
	state := State{Runs: []StateRun{{ID: "run-1", Status: "success", Goal: "x", Iterations: 2, Report: ".forge/runs/run-1/report.md"}}}
	text := FormatLastRun(state)
	if !strings.Contains(text, "Last run: run-1") || !strings.Contains(text, "Report:") {
		t.Fatalf("text = %q", text)
	}
	if FormatLastRun(State{}) != "No Forge runs recorded in this workspace." {
		t.Fatal("empty state should say no runs")
	}
}
