package reports

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/soub4i/forge/internal/benchmark"
	"github.com/soub4i/forge/internal/config"
	"github.com/soub4i/forge/internal/gates"
	"github.com/soub4i/forge/internal/models"
	"github.com/soub4i/forge/internal/security"
	"github.com/soub4i/forge/internal/util"
)

// Run identifies one harness run and its artifact directory.
type Run struct {
	ID         string
	Dir        string
	Cwd        string
	ConfigFile string
	StartedAt  string
	Goal       string
	RunJSON    string
}

// StateRun is the compact run record kept in `.forge/state.json`.
type StateRun struct {
	ID         string      `json:"id"`
	StartedAt  string      `json:"startedAt"`
	EndedAt    string      `json:"endedAt"`
	Status     string      `json:"status"`
	ExitReason string      `json:"exitReason,omitempty"`
	Iterations int         `json:"iterations"`
	CostUSD    float64     `json:"costUsd"`
	Tokens     util.Tokens `json:"tokens"`
	Goal       string      `json:"goal"`
	Report     string      `json:"report"`
}

// State is the workspace-level run history.
type State struct {
	Version   int        `json:"version"`
	UpdatedAt string     `json:"updatedAt,omitempty"`
	Runs      []StateRun `json:"runs"`
	LastRunID string     `json:"lastRunId,omitempty"`
}

// ReportFiles are the written report paths.
type ReportFiles struct {
	JSONFile     string
	MarkdownFile string
}

// ForgeDir returns the workspace harness directory.
func ForgeDir(cwd string) string {
	return filepath.Join(cwd, ".forge")
}

// StatePath returns the workspace state file path.
func StatePath(cwd string) string {
	return filepath.Join(ForgeDir(cwd), "state.json")
}

// ReadState loads the workspace run history, tolerating a missing file.
func ReadState(cwd string) State {
	state := State{Version: 1, Runs: []StateRun{}}
	_ = util.ReadJSON(StatePath(cwd), &state)
	if state.Runs == nil {
		state.Runs = []StateRun{}
	}
	return state
}

// WriteState persists the workspace run history.
func WriteState(cwd string, state State) error {
	state.UpdatedAt = util.NowISO()
	return util.WriteJSON(StatePath(cwd), state)
}

// CreateRun allocates a run id and artifact directories.
func CreateRun(cwd string, cfg config.Config, configFile string) *Run {
	id := util.RunID(time.Now())
	dir := filepath.Join(ForgeDir(cwd), "runs", id)
	for _, sub := range []string{"stages", "prompts", "events", "commands", "security", "iterations"} {
		_ = util.EnsureDir(filepath.Join(dir, sub))
	}
	return &Run{
		ID: id, Dir: dir, Cwd: cwd, ConfigFile: configFile,
		StartedAt: util.NowISO(), Goal: cfg.Goal, RunJSON: filepath.Join(dir, "run.json"),
	}
}

// EnsureGoalDoc writes docs/GOAL.md from the acceptance criteria if absent.
func EnsureGoalDoc(cwd string, cfg config.Config) (string, error) {
	file := filepath.Join(cwd, "docs", "GOAL.md")
	if util.FileExists(file) {
		return file, nil
	}
	var checklist []string
	testLine := "- [ ] Unit tests pass"
	if spec := cfg.Command("test"); spec != nil && spec.Command != "" {
		testLine += fmt.Sprintf(" (`%s`)", spec.Command)
	}
	checklist = append(checklist, testLine)
	if cfg.Acceptance.Coverage.Min > 0 {
		checklist = append(checklist, fmt.Sprintf("- [ ] Coverage is at least %g%%", cfg.Acceptance.Coverage.Min))
	}
	if cfg.Acceptance.Lint {
		checklist = append(checklist, "- [ ] Lint passes")
	}
	if cfg.Acceptance.Typecheck {
		checklist = append(checklist, "- [ ] Type check passes")
	}
	if cfg.Acceptance.Build {
		checklist = append(checklist, "- [ ] Build passes")
	}
	for _, threshold := range cfg.Acceptance.Benchmarks.Thresholds {
		bound := ""
		if threshold.Min != nil {
			bound = fmt.Sprintf(">= %g", *threshold.Min)
		} else if threshold.Max != nil {
			bound = fmt.Sprintf("<= %g", *threshold.Max)
		}
		line := fmt.Sprintf("- [ ] Benchmark `%s` %s", threshold.Metric, bound)
		if threshold.Unit != "" {
			line += " " + threshold.Unit
		}
		checklist = append(checklist, line)
	}
	if cfg.Acceptance.Security.Enabled {
		line := fmt.Sprintf("- [ ] No security findings above `%s`", cfg.Acceptance.Security.MaxSeverity)
		if cfg.Acceptance.Security.RequireNoSecret {
			line += " and no committed secrets"
		}
		checklist = append(checklist, line)
	}
	if cfg.Acceptance.Review.Enabled {
		checklist = append(checklist, "- [ ] Independent review approves with no blocking findings")
	}
	if cfg.Release.Enabled {
		checklist = append(checklist, "- [ ] Release notes and changelog are generated")
	}
	content := fmt.Sprintf(`# Goal

%s

## Acceptance criteria

%s

## Definition of done

Forge runs the configured commands, security scanners, benchmarks, and an independent review after every iteration. The goal is met only when all machine checks pass and the reviewer approves. The machine-readable source of truth is `+"`forge.config.json`"+`.
`, cfg.Goal, strings.Join(checklist, "\n"))
	if err := util.WriteText(file, content); err != nil {
		return "", err
	}
	return file, nil
}

// ClearReport removes a stale stage report.
func ClearReport(cwd, rel string) {
	_ = os.Remove(filepath.Join(cwd, rel))
}

func readReportObject(cwd, rel string) (map[string]any, bool) {
	text := util.ReadText(filepath.Join(cwd, rel))
	if strings.TrimSpace(text) == "" {
		return nil, false
	}
	value := util.ExtractJSON(text)
	object, ok := value.(map[string]any)
	return object, ok
}

func reportObjectFromText(stageText string) (map[string]any, bool) {
	value := util.ExtractJSON(stageText)
	object, ok := value.(map[string]any)
	return object, ok
}

func decodeFindings[T any](object map[string]any, key string) ([]T, bool) {
	raw, ok := object[key]
	if !ok {
		return nil, false
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, false
	}
	var findings []T
	if err := json.Unmarshal(data, &findings); err != nil {
		return nil, false
	}
	if findings == nil {
		findings = []T{}
	}
	return findings, true
}

// LoadSecurityReport reads `.forge/security.json`, falling back to JSON in the
// stage text. It returns a missing report rather than nil on contract failure.
func LoadSecurityReport(cwd, stageText string) *security.Report {
	candidates := []map[string]any{}
	if object, ok := readReportObject(cwd, ".forge/security.json"); ok {
		candidates = append(candidates, object)
	}
	if object, ok := reportObjectFromText(stageText); ok {
		candidates = append(candidates, object)
	}
	for _, object := range candidates {
		verdict, ok := object["verdict"].(string)
		if !ok {
			continue
		}
		findings, ok := decodeFindings[security.Finding](object, "findings")
		if !ok {
			continue
		}
		summary, _ := object["summary"].(string)
		normalized := make([]security.Finding, 0, len(findings))
		for _, finding := range findings {
			normalized = append(normalized, security.Normalize(finding, "security-review"))
		}
		return &security.Report{
			Missing: false, Verdict: strings.ToLower(verdict),
			Summary: summary, Findings: normalized,
		}
	}
	return &security.Report{
		Missing: true, Verdict: "fail",
		Summary:  "Security stage did not write a valid .forge/security.json report.",
		Findings: []security.Finding{},
	}
}

// LoadReviewReport reads `.forge/review.json`, falling back to the stage text.
func LoadReviewReport(cwd, stageText string) *gates.ReviewReport {
	candidates := []map[string]any{}
	if object, ok := readReportObject(cwd, ".forge/review.json"); ok {
		candidates = append(candidates, object)
	}
	if object, ok := reportObjectFromText(stageText); ok {
		candidates = append(candidates, object)
	}
	for _, object := range candidates {
		verdict, ok := object["verdict"].(string)
		if !ok {
			continue
		}
		findings, ok := decodeFindings[gates.ReviewFinding](object, "findings")
		if !ok {
			continue
		}
		summary, _ := object["summary"].(string)
		designChange, _ := object["design_change_needed"].(bool)
		normalized := make([]gates.ReviewFinding, 0, len(findings))
		for _, finding := range findings {
			finding.Severity = config.NormalizeSeverity(finding.Severity, "medium")
			if finding.Title == "" {
				finding.Title = "Review finding"
			}
			if finding.Recommendation == "" {
				finding.Recommendation = "Address the review finding."
			}
			normalized = append(normalized, finding)
		}
		return &gates.ReviewReport{
			Missing: false, Verdict: strings.ToLower(verdict), Summary: summary,
			DesignChangeNeeded: designChange, Findings: normalized,
		}
	}
	return &gates.ReviewReport{
		Missing: true, Verdict: "missing",
		Summary:  "Reviewer did not write a valid .forge/review.json report.",
		Findings: []gates.ReviewFinding{},
	}
}

// StageArtifactInput is the data needed to write one stage artifact.
type StageArtifactInput struct {
	Model      string
	OK         bool
	DurationMs int64
	SessionID  string
	Tokens     util.Tokens
	CostUSD    float64
	Errors     []string
	Text       string
}

// WriteStageArtifacts writes the prompt and stage transcript files.
func WriteStageArtifacts(run *Run, iteration int, stage, prompt string, result StageArtifactInput) (string, string, string, error) {
	prefix := fmt.Sprintf("%02d-%s", iteration, stage)
	promptFile := filepath.Join(run.Dir, "prompts", prefix+".md")
	textFile := filepath.Join(run.Dir, "stages", prefix+".md")
	eventsFile := filepath.Join(run.Dir, "events", prefix+".jsonl")
	if err := util.WriteText(promptFile, prompt); err != nil {
		return "", "", "", err
	}
	errorsLine := ""
	if len(result.Errors) > 0 {
		errorsLine = "- errors:\n"
		for _, item := range result.Errors {
			errorsLine += "  - " + item + "\n"
		}
	}
	text := fmt.Sprintf(`# %s — iteration %d

- model: %s
- ok: %v
- duration: %s
- session: %s
- tokens: in=%d out=%d reasoning=%d
- cost: %s
%s
## Model output

%s
`, stage, iteration, result.Model, result.OK, util.FormatDuration(result.DurationMs),
		valueOr(result.SessionID, "(none)"), result.Tokens.Input, result.Tokens.Output,
		result.Tokens.Reasoning, util.FormatUSD(result.CostUSD), errorsLine, valueOr(result.Text, "(no text output)"))
	if err := util.WriteText(textFile, text); err != nil {
		return "", "", "", err
	}
	return promptFile, textFile, eventsFile, nil
}

// GateCommandView is a sanitized command result for reports.
type GateCommandView struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	OK         bool   `json:"ok"`
	Optional   bool   `json:"optional,omitempty"`
	Missing    bool   `json:"missing,omitempty"`
	Command    string `json:"command,omitempty"`
	ExitCode   int    `json:"exitCode,omitempty"`
	TimedOut   bool   `json:"timedOut,omitempty"`
	DurationMs int64  `json:"durationMs"`
	Message    string `json:"message,omitempty"`
	LogFile    string `json:"logFile,omitempty"`
}

// CoverageView is the sanitized coverage outcome.
type CoverageView struct {
	Required float64  `json:"required"`
	Percent  *float64 `json:"percent"`
	Source   string   `json:"source,omitempty"`
	OK       bool     `json:"ok"`
	Skipped  bool     `json:"skipped"`
	Message  string   `json:"message,omitempty"`
}

// BenchmarkView is the sanitized benchmark outcome.
type BenchmarkView struct {
	OK        bool                `json:"ok"`
	Skipped   bool                `json:"skipped"`
	Required  bool                `json:"required"`
	Metrics   map[string]float64  `json:"metrics"`
	Source    string              `json:"source,omitempty"`
	Failures  []benchmark.Failure `json:"failures"`
	Command   string              `json:"command,omitempty"`
	CommandOK bool                `json:"commandOk"`
}

// SecurityView is the sanitized security outcome.
type SecurityView struct {
	OK       bool                   `json:"ok"`
	Skipped  bool                   `json:"skipped"`
	Secrets  security.SecretResult  `json:"secrets"`
	Scanners security.ScannerResult `json:"scanners"`
}

// GatesView is the sanitized machine-gate report.
type GatesView struct {
	OK         bool              `json:"ok"`
	CodeOK     bool              `json:"codeOk"`
	DurationMs int64             `json:"durationMs"`
	Commands   []GateCommandView `json:"commands"`
	Coverage   CoverageView      `json:"coverage"`
	Benchmark  *BenchmarkView    `json:"benchmark"`
	Security   *SecurityView     `json:"security"`
}

// SanitizeDeterministic converts gate results into report-safe structures.
func SanitizeDeterministic(det *gates.Deterministic) *GatesView {
	if det == nil {
		return nil
	}
	view := &GatesView{
		OK: det.OK, CodeOK: det.CodeOK, DurationMs: det.DurationMs,
		Coverage: CoverageView{
			Required: det.Coverage.Required, Percent: det.Coverage.Percent,
			Source: det.Coverage.Source, OK: det.Coverage.OK,
			Skipped: det.Coverage.Skipped, Message: det.Coverage.Message,
		},
		Commands: []GateCommandView{},
	}
	for _, command := range det.Commands {
		logFile := command.LogFile
		if logFile != "" && det.Cwd != "" {
			if rel, err := filepath.Rel(det.Cwd, logFile); err == nil {
				logFile = rel
			}
		}
		view.Commands = append(view.Commands, GateCommandView{
			Name: command.Name, Status: command.Status, OK: command.OK, Optional: command.Optional,
			Missing: command.Missing, Command: command.Command, ExitCode: command.ExitCode,
			TimedOut: command.TimedOut, DurationMs: command.DurationMs, Message: command.Message, LogFile: logFile,
		})
	}
	if !det.Benchmark.Skipped {
		view.Benchmark = &BenchmarkView{
			OK: det.Benchmark.OK, Skipped: det.Benchmark.Skipped, Required: det.Benchmark.Required,
			Metrics: det.Benchmark.Metrics, Source: det.Benchmark.Source, Failures: det.Benchmark.Failures,
			Command: det.Benchmark.Command, CommandOK: det.Benchmark.CommandOK,
		}
	}
	if !det.Security.Skipped {
		view.Security = &SecurityView{
			OK: det.Security.OK, Skipped: det.Security.Skipped,
			Secrets: det.Security.Secrets, Scanners: det.Security.Scanners,
		}
	}
	return view
}

// ReportStage is one stage row in a run report.
type ReportStage struct {
	Stage      string      `json:"stage"`
	Model      string      `json:"model"`
	OK         bool        `json:"ok"`
	DurationMs int64       `json:"durationMs"`
	Tokens     util.Tokens `json:"tokens"`
	CostUSD    float64     `json:"costUsd"`
	SessionID  string      `json:"sessionID,omitempty"`
	Errors     []string    `json:"errors,omitempty"`
	PromptFile string      `json:"promptFile,omitempty"`
	TextFile   string      `json:"textFile,omitempty"`
	EventsFile string      `json:"eventsFile,omitempty"`
}

// SecurityGateView is the report view of the security gate.
type SecurityGateView struct {
	OK         bool               `json:"ok"`
	MaxAllowed string             `json:"maxAllowed"`
	Findings   []security.Finding `json:"findings"`
	Blocking   []security.Finding `json:"blocking"`
}

// ReviewGateView is the report view of the review gate.
type ReviewGateView struct {
	OK                 bool                  `json:"ok"`
	Verdict            string                `json:"verdict"`
	Blocking           []gates.ReviewFinding `json:"blocking"`
	Findings           []gates.ReviewFinding `json:"findings"`
	DesignChangeNeeded bool                  `json:"designChangeNeeded"`
	Summary            string                `json:"summary,omitempty"`
}

// ReportIteration is one iteration in a run report.
type ReportIteration struct {
	Number       int               `json:"number"`
	StartedAt    string            `json:"startedAt"`
	EndedAt      string            `json:"endedAt"`
	Stages       []ReportStage     `json:"stages"`
	Gates        *GatesView        `json:"gates"`
	SecurityGate *SecurityGateView `json:"securityGate"`
	ReviewGate   *ReviewGateView   `json:"reviewGate"`
	FeedbackFile string            `json:"feedbackFile,omitempty"`
}

// ReleaseStageView summarizes the release stage in the report.
type ReleaseStageView struct {
	Model      string      `json:"model"`
	OK         bool        `json:"ok"`
	DurationMs int64       `json:"durationMs"`
	Tokens     util.Tokens `json:"tokens"`
	CostUSD    float64     `json:"costUsd"`
}

// ReleaseView is the report view of release preparation.
type ReleaseView struct {
	OK      bool              `json:"ok"`
	Skipped bool              `json:"skipped"`
	Missing []string          `json:"missing"`
	Stage   *ReleaseStageView `json:"stage,omitempty"`
}

// Report is the machine-readable run report.
type Report struct {
	RunID        string               `json:"runId"`
	Goal         string               `json:"goal"`
	Workspace    string               `json:"workspace"`
	ConfigFile   string               `json:"configFile,omitempty"`
	StartedAt    string               `json:"startedAt"`
	EndedAt      string               `json:"endedAt"`
	Status       string               `json:"status"`
	ExitReason   string               `json:"exitReason,omitempty"`
	Error        string               `json:"error,omitempty"`
	Iterations   []ReportIteration    `json:"iterations"`
	Ledger       models.LedgerSummary `json:"ledger"`
	FinalGates   *GatesView           `json:"finalGates"`
	SecurityGate *SecurityGateView    `json:"securityGate"`
	ReviewGate   *ReviewGateView      `json:"reviewGate"`
	Models       map[string]string    `json:"models"`
	Budget       config.Budget        `json:"budget"`
	Release      *ReleaseView         `json:"release,omitempty"`
}

// BuildInput collects the data needed to build a run report.
type BuildInput struct {
	Run          *Run
	Config       config.Config
	Iterations   []ReportIteration
	Ledger       *models.Ledger
	Success      bool
	ExitReason   string
	ConfigFile   string
	SecurityGate *security.Gate
	ReviewGate   *gates.ReviewGate
	FinalGates   *gates.Deterministic
}

// BuildReport assembles the report structure.
func BuildReport(input BuildInput) *Report {
	status := "failed"
	if input.Success {
		status = "success"
	}
	modelMap := map[string]string{}
	for stage, entry := range input.Config.Models {
		modelMap[stage] = entry.Model
	}
	report := &Report{
		RunID: input.Run.ID, Goal: input.Config.Goal, Workspace: input.Run.Cwd,
		ConfigFile: input.ConfigFile, StartedAt: input.Run.StartedAt, EndedAt: util.NowISO(),
		Status: status, ExitReason: input.ExitReason,
		Iterations: input.Iterations, Ledger: input.Ledger.Summary(),
		FinalGates: SanitizeDeterministic(input.FinalGates),
		Models:     modelMap, Budget: input.Config.Budget,
	}
	if input.SecurityGate != nil {
		report.SecurityGate = &SecurityGateView{
			OK: input.SecurityGate.OK, MaxAllowed: input.SecurityGate.MaxAllowed,
			Findings: input.SecurityGate.Findings, Blocking: input.SecurityGate.Blocking,
		}
	}
	if input.ReviewGate != nil {
		report.ReviewGate = &ReviewGateView{
			OK: input.ReviewGate.OK, Verdict: input.ReviewGate.Verdict,
			Blocking: input.ReviewGate.Blocking, Findings: input.ReviewGate.Findings,
			DesignChangeNeeded: input.ReviewGate.DesignChangeNeeded, Summary: input.ReviewGate.Summary,
		}
	}
	return report
}

func statusMark(ok bool) string {
	if ok {
		return "PASS"
	}
	return "FAIL"
}

// RenderReportMarkdown renders the human-readable run report.
func RenderReportMarkdown(report *Report) string {
	lines := []string{
		"# Forge run " + report.RunID,
		"",
		"**Status:** " + strings.ToUpper(report.Status),
		"",
		"**Goal:** " + report.Goal,
		"",
		"**Workspace:** `" + report.Workspace + "`",
		"",
		"**Duration:** " + util.FormatDuration(report.Ledger.ElapsedMs) + "  ",
		"**Cost:** " + util.FormatUSD(report.Ledger.CostUSD) + "  ",
		fmt.Sprintf("**Tokens:** in %s / out %s / reasoning %s",
			util.FormatNumber(report.Ledger.Tokens.Input), util.FormatNumber(report.Ledger.Tokens.Output),
			util.FormatNumber(report.Ledger.Tokens.Reasoning)),
		"",
		"## Iterations",
		"",
		"| # | Stage | Model | Duration | Tokens in/out | Cost | Status |",
		"|---|-------|-------|----------|---------------|------|--------|",
	}
	for _, iteration := range report.Iterations {
		for _, stage := range iteration.Stages {
			lines = append(lines, fmt.Sprintf("| %d | %s | `%s` | %s | %s/%s | %s | %s |",
				iteration.Number, stage.Stage, stage.Model, util.FormatDuration(stage.DurationMs),
				util.FormatNumber(stage.Tokens.Input), util.FormatNumber(stage.Tokens.Output),
				util.FormatUSD(stage.CostUSD), statusMark(stage.OK)))
		}
	}
	lines = append(lines, "")
	if final := report.FinalGates; final != nil {
		lines = append(lines, "## Final gates", "")
		for _, command := range final.Commands {
			if command.Status == "skip" {
				continue
			}
			line := fmt.Sprintf("- %s: **%s**", command.Name, strings.ToUpper(command.Status))
			if command.Message != "" {
				line += " — " + command.Message
			}
			lines = append(lines, line)
		}
		if !final.Coverage.Skipped {
			lines = append(lines, fmt.Sprintf("- coverage: **%s** — %s", statusMark(final.Coverage.OK), final.Coverage.Message))
		}
		if final.Benchmark != nil && !final.Benchmark.Skipped {
			var metrics []string
			for key, value := range final.Benchmark.Metrics {
				metrics = append(metrics, fmt.Sprintf("%s=%g", key, value))
			}
			lines = append(lines, fmt.Sprintf("- benchmark: **%s** — %s", statusMark(final.Benchmark.OK), strings.Join(metrics, ", ")))
			for _, failure := range final.Benchmark.Failures {
				lines = append(lines, "  - "+failure.Message)
			}
		}
		if final.Security != nil && !final.Security.Skipped {
			lines = append(lines, fmt.Sprintf("- secret scan: **%s** (%d findings)",
				statusMark(final.Security.Secrets.OK), len(final.Security.Secrets.Findings)))
			lines = append(lines, fmt.Sprintf("- security scanners: **%s** (%d findings)",
				statusMark(final.Security.Scanners.OK), len(final.Security.Scanners.Findings)))
			for _, warning := range final.Security.Scanners.Warnings {
				lines = append(lines, "  - warning: "+warning)
			}
		}
		lines = append(lines, "")
	}
	if gate := report.SecurityGate; gate != nil {
		lines = append(lines, "## Security review", "")
		lines = append(lines, "- verdict: **"+statusMark(gate.OK)+"**")
		lines = append(lines, "- max allowed severity: "+gate.MaxAllowed)
		for _, finding := range gate.Findings {
			location := ""
			if finding.File != "" {
				location = " (" + finding.File
				if finding.Line > 0 {
					location += fmt.Sprintf(":%d", finding.Line)
				}
				location += ")"
			}
			lines = append(lines, fmt.Sprintf("- [%s] %s%s", finding.Severity, finding.Title, location))
		}
		if len(gate.Findings) == 0 {
			lines = append(lines, "- no findings")
		}
		lines = append(lines, "")
	}
	if gate := report.ReviewGate; gate != nil {
		lines = append(lines, "## Independent review", "")
		lines = append(lines, "- verdict: **"+gate.Verdict+"**")
		lines = append(lines, fmt.Sprintf("- blocking findings: %d", len(gate.Blocking)))
		lines = append(lines, "- design change needed: "+map[bool]string{true: "yes", false: "no"}[gate.DesignChangeNeeded])
		if gate.Summary != "" {
			lines = append(lines, "", gate.Summary)
		}
		for _, finding := range gate.Blocking {
			line := fmt.Sprintf("- [%s] %s", finding.Severity, finding.Title)
			if finding.File != "" {
				line += " (" + finding.File + ")"
			}
			lines = append(lines, line)
			if finding.Recommendation != "" {
				lines = append(lines, "  "+finding.Recommendation)
			}
		}
		lines = append(lines, "")
	}
	lines = append(lines,
		"## Artifacts", "",
		"- Run directory: `.forge/runs/"+report.RunID+"`",
		"- Prompts: `.forge/runs/"+report.RunID+"/prompts/`",
		"- Stage output: `.forge/runs/"+report.RunID+"/stages/`",
		"- Command logs: `.forge/runs/"+report.RunID+"/commands/`",
		"- Event streams: `.forge/runs/"+report.RunID+"/events/`",
		"",
	)
	return strings.Join(lines, "\n")
}

// WriteRunReport writes report.json and report.md.
func WriteRunReport(run *Run, report *Report) (ReportFiles, error) {
	jsonFile := filepath.Join(run.Dir, "report.json")
	markdownFile := filepath.Join(run.Dir, "report.md")
	if err := util.WriteJSON(jsonFile, report); err != nil {
		return ReportFiles{}, err
	}
	if err := util.WriteText(markdownFile, RenderReportMarkdown(report)); err != nil {
		return ReportFiles{}, err
	}
	return ReportFiles{JSONFile: jsonFile, MarkdownFile: markdownFile}, nil
}

// RecordRun prepends the run summary to `.forge/state.json`.
func RecordRun(cwd string, report *Report, files ReportFiles) StateRun {
	state := ReadState(cwd)
	relativeReport, err := filepath.Rel(cwd, files.MarkdownFile)
	if err != nil {
		relativeReport = files.MarkdownFile
	}
	summary := StateRun{
		ID: report.RunID, StartedAt: report.StartedAt, EndedAt: report.EndedAt,
		Status: report.Status, ExitReason: report.ExitReason, Iterations: len(report.Iterations),
		CostUSD: report.Ledger.CostUSD, Tokens: report.Ledger.Tokens, Goal: report.Goal,
		Report: relativeReport,
	}
	runs := []StateRun{summary}
	for _, run := range state.Runs {
		if run.ID != summary.ID {
			runs = append(runs, run)
		}
	}
	if len(runs) > 30 {
		runs = runs[:30]
	}
	state.Runs = runs
	state.LastRunID = summary.ID
	_ = WriteState(cwd, state)
	return summary
}

// PrintRunSummary prints the final console summary.
func PrintRunSummary(report *Report, files ReportFiles) {
	ok := report.Status == "success"
	title := "GOAL NOT MET"
	if ok {
		title = "GOAL MET"
	}
	mark := "✖"
	if ok {
		mark = "✔"
	}
	suffix := ""
	if !ok && report.ExitReason != "" {
		suffix = " (" + report.ExitReason + ")"
	}
	util.Log("")
	util.Log(mark + " " + title + "  " + suffix)
	util.Log(fmt.Sprintf("  iterations: %d   duration: %s   cost: %s   tokens: %s in / %s out",
		len(report.Iterations), util.FormatDuration(report.Ledger.ElapsedMs), util.FormatUSD(report.Ledger.CostUSD),
		util.FormatNumber(report.Ledger.Tokens.Input), util.FormatNumber(report.Ledger.Tokens.Output)))
	if report.FinalGates != nil {
		var gates []string
		for _, command := range report.FinalGates.Commands {
			if command.Status == "skip" {
				continue
			}
			gates = append(gates, command.Name+"="+command.Status)
		}
		if len(gates) > 0 {
			util.Log("  gates: " + strings.Join(gates, " "))
		}
	}
	if report.ReviewGate != nil {
		util.Log("  review: " + report.ReviewGate.Verdict)
	}
	if rel, err := filepath.Rel(report.Workspace, files.MarkdownFile); err == nil {
		util.Log("  report: " + rel)
	}
	if rel, err := filepath.Rel(report.Workspace, filepath.Dir(files.MarkdownFile)); err == nil {
		util.Log("  logs:   " + rel)
	}
	util.Log("")
}

// FormatLastRun renders the latest run summary for `forge status`.
func FormatLastRun(state State) string {
	if len(state.Runs) == 0 {
		return "No Forge runs recorded in this workspace."
	}
	run := state.Runs[0]
	status := run.Status
	if run.ExitReason != "" {
		status += " (" + run.ExitReason + ")"
	}
	return strings.Join([]string{
		"Last run: " + run.ID,
		"Status: " + status,
		"Goal: " + run.Goal,
		fmt.Sprintf("Iterations: %d  Cost: %s", run.Iterations, util.FormatUSD(run.CostUSD)),
		"Report: " + run.Report,
	}, "\n")
}

// TailFile returns the tail of a file, or an empty string.
func TailFile(file string, maxChars int) string {
	if !util.FileExists(file) {
		return ""
	}
	if maxChars <= 0 {
		maxChars = 4000
	}
	return util.Tail(util.ReadText(file), maxChars)
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
