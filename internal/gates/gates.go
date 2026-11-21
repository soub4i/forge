package gates

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/soub4i/forge/internal/benchmark"
	"github.com/soub4i/forge/internal/config"
	"github.com/soub4i/forge/internal/coverage"
	"github.com/soub4i/forge/internal/security"
	"github.com/soub4i/forge/internal/util"
)

// GateCommand is the outcome of one machine-checked command.
type GateCommand struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	OK         bool   `json:"ok"`
	Optional   bool   `json:"optional"`
	Missing    bool   `json:"missing"`
	Command    string `json:"command,omitempty"`
	ExitCode   int    `json:"exitCode,omitempty"`
	Signal     string `json:"signal,omitempty"`
	TimedOut   bool   `json:"timedOut,omitempty"`
	DurationMs int64  `json:"durationMs"`
	Stdout     string `json:"stdout,omitempty"`
	Stderr     string `json:"stderr,omitempty"`
	LogFile    string `json:"logFile,omitempty"`
	Message    string `json:"message,omitempty"`
}

// CoverageResult is the parsed coverage outcome.
type CoverageResult struct {
	Required float64  `json:"required"`
	Percent  *float64 `json:"percent"`
	Source   string   `json:"source,omitempty"`
	OK       bool     `json:"ok"`
	Skipped  bool     `json:"skipped"`
	Message  string   `json:"message,omitempty"`
}

// SecurityResult combines secret scanning and external scanners.
type SecurityResult struct {
	OK       bool                   `json:"ok"`
	Skipped  bool                   `json:"skipped"`
	Secrets  security.SecretResult  `json:"secrets"`
	Scanners security.ScannerResult `json:"scanners"`
}

// Deterministic is the complete machine-gate evaluation for one iteration.
type Deterministic struct {
	OK         bool             `json:"ok"`
	CodeOK     bool             `json:"codeOk"`
	Cwd        string           `json:"cwd,omitempty"`
	Coverage   CoverageResult   `json:"coverage"`
	Benchmark  benchmark.Result `json:"benchmark"`
	Security   SecurityResult   `json:"security"`
	Commands   []GateCommand    `json:"commands"`
	Resolved   Resolved         `json:"-"`
	DurationMs int64            `json:"durationMs"`
}

var gateOrder = []string{"lint", "typecheck", "build", "test", "coverage"}

// GateEvent reports one machine-gate transition to progress observers.
type GateEvent struct {
	Name     string
	Status   string // start, pass, fail, warn, skip
	Index    int
	Total    int
	Duration time.Duration
}

// GateObserver receives gate progress events.
type GateObserver func(GateEvent)

func notifyGate(observers []GateObserver, event GateEvent) {
	for _, observer := range observers {
		if observer != nil {
			observer(event)
		}
	}
}

func gateEnabled(cfg config.Config, name string) bool {
	switch name {
	case "test":
		return cfg.Acceptance.Tests
	case "coverage":
		return cfg.Acceptance.Coverage.Min > 0
	case "lint":
		return cfg.Acceptance.Lint
	case "typecheck":
		return cfg.Acceptance.Typecheck
	case "build":
		return cfg.Acceptance.Build
	}
	return false
}

func runNamedCommand(ctx context.Context, name string, spec *CommandSpec, cwd, runDir string, iteration, timeoutMs int) GateCommand {
	if spec == nil {
		return GateCommand{
			Name: name, Status: "fail", OK: false, Missing: true,
			Message: fmt.Sprintf("No %s command is configured or detectable.", name),
		}
	}
	if timeoutMs <= 0 {
		timeoutMs = 20 * 60 * 1000
	}
	if spec.TimeoutMs > 0 {
		timeoutMs = spec.TimeoutMs
	}
	result := util.RunCommand(ctx, spec.Command, cwd, time.Duration(timeoutMs)*time.Millisecond)
	logFile := filepath.Join(runDir, "commands", fmt.Sprintf("%02d-%s.log", iteration, name))
	logBody := fmt.Sprintf("$ %s\nexit: %d\nduration: %s\ntimed out: %v\n\n--- stdout ---\n%s\n\n--- stderr ---\n%s",
		spec.Command, result.Code, util.FormatDuration(result.Duration.Milliseconds()), result.TimedOut, result.Stdout, result.Stderr)
	_ = util.WriteText(logFile, logBody)

	status := "fail"
	if result.OK {
		status = "pass"
	} else if spec.Optional {
		status = "warn"
	}
	message := "passed"
	if !result.OK {
		if result.TimedOut {
			message = "timed out after " + util.FormatDuration(result.Duration.Milliseconds())
		} else {
			message = fmt.Sprintf("exited with code %d", result.Code)
		}
	}
	return GateCommand{
		Name: name, Status: status, OK: result.OK, Optional: spec.Optional,
		Command: spec.Command, ExitCode: result.Code, TimedOut: result.TimedOut,
		DurationMs: result.Duration.Milliseconds(), Stdout: result.Stdout, Stderr: result.Stderr,
		LogFile: logFile, Message: message,
	}
}

// RunInstall runs the install command, if one is configured or detected.
func RunInstall(ctx context.Context, cfg config.Config, cwd, runDir string, resolved *Resolved) GateCommand {
	if resolved == nil {
		r := ResolveCommands(cfg, cwd)
		resolved = &r
	}
	spec := resolved.Commands["install"]
	if spec == nil {
		return GateCommand{Name: "install", Status: "skip", OK: true, Message: "not configured"}
	}
	timeout := cfg.Loop.StageTimeoutMinutes * 60 * 1000
	return runNamedCommand(ctx, "install", spec, cwd, runDir, 0, timeout)
}

// EvaluateDeterministic runs every machine-checkable gate. Security scanners
// are skipped when code gates fail, because fixing broken code comes first.
// Optional observers receive progress events for each enabled check.
func EvaluateDeterministic(ctx context.Context, cfg config.Config, cwd, runDir string, iteration int, resolved *Resolved, observers ...GateObserver) Deterministic {
	started := time.Now()
	if resolved == nil {
		r := ResolveCommands(cfg, cwd)
		resolved = &r
	}
	timeoutMs := cfg.Loop.StageTimeoutMinutes * 60 * 1000
	commands := make([]GateCommand, 0, len(gateOrder))

	total := 0
	for _, name := range gateOrder {
		if gateEnabled(cfg, name) {
			total++
		}
	}
	if cfg.Acceptance.Benchmarks.Required {
		total++
	}
	index := 0

	for _, name := range gateOrder {
		if !gateEnabled(cfg, name) {
			commands = append(commands, GateCommand{Name: name, Status: "skip", OK: true, Message: "disabled"})
			continue
		}
		if name == "coverage" && cfg.Acceptance.Coverage.File != nil && *cfg.Acceptance.Coverage.File != "" {
			_ = os.Remove(filepath.Join(cwd, *cfg.Acceptance.Coverage.File))
		}
		index++
		notifyGate(observers, GateEvent{Name: name, Status: "start", Index: index, Total: total})
		command := runNamedCommand(ctx, name, resolved.Commands[name], cwd, runDir, iteration, timeoutMs)
		notifyGate(observers, GateEvent{
			Name: name, Status: command.Status, Index: index, Total: total,
			Duration: time.Duration(command.DurationMs) * time.Millisecond,
		})
		commands = append(commands, command)
	}

	coverageResult := CoverageResult{Required: cfg.Acceptance.Coverage.Min, OK: true, Skipped: true}
	if cfg.Acceptance.Coverage.Min > 0 {
		index := -1
		for i := range commands {
			if commands[i].Name == "coverage" {
				index = i
				break
			}
		}
		if index >= 0 && commands[index].OK {
			percent, source := coverage.Extract(cwd, cfg, commands[index].Stdout)
			ok := percent != nil && *percent >= cfg.Acceptance.Coverage.Min
			message := "coverage percentage could not be parsed from the command output"
			if percent != nil {
				message = fmt.Sprintf("%g%% (minimum %g%%)", *percent, cfg.Acceptance.Coverage.Min)
			}
			coverageResult = CoverageResult{
				Required: cfg.Acceptance.Coverage.Min, Percent: percent, Source: source,
				OK: ok, Skipped: false, Message: message,
			}
			if !ok {
				commands[index].Status = "fail"
				commands[index].OK = false
				commands[index].Message = message
			}
		} else {
			coverageResult = CoverageResult{
				Required: cfg.Acceptance.Coverage.Min, OK: false, Skipped: false,
				Message: "coverage command did not pass",
			}
		}
	}

	codeOK := true
	for _, command := range commands {
		if !command.OK && command.Status != "skip" && !command.Optional {
			codeOK = false
		}
	}

	benchmarkResult := benchmark.Result{OK: true, Skipped: true, Metrics: map[string]float64{}}
	if codeOK && cfg.Acceptance.Benchmarks.Required {
		spec := resolved.Commands["benchmark"]
		if spec == nil {
			benchmarkResult = benchmark.Result{
				OK: false, Skipped: false, Required: true, Metrics: map[string]float64{},
				Failures: []benchmark.Failure{{
					Metric: "(benchmark)", Reason: "missing_command",
					Message: "No benchmark command is configured or detectable.",
				}},
			}
		} else {
			resultsFile := cfg.Acceptance.Benchmarks.ResultsFile
			if resultsFile == "" {
				resultsFile = "bench/results.json"
			}
			_ = os.Remove(filepath.Join(cwd, resultsFile))
			index++
			notifyGate(observers, GateEvent{Name: "benchmark", Status: "start", Index: index, Total: total})
			command := runNamedCommand(ctx, "benchmark", spec, cwd, runDir, iteration, timeoutMs)
			notifyGate(observers, GateEvent{
				Name: "benchmark", Status: command.Status, Index: index, Total: total,
				Duration: time.Duration(command.DurationMs) * time.Millisecond,
			})
			evaluated := benchmark.Evaluate(cfg, cwd, command.Stdout)
			evaluated.Required = true
			evaluated.Skipped = false
			evaluated.Command = spec.Command
			evaluated.CommandOK = command.OK
			evaluated.DurationMs = command.DurationMs
			evaluated.ExitCode = command.ExitCode
			evaluated.LogFile = command.LogFile
			evaluated.Stdout = command.Stdout
			evaluated.Stderr = command.Stderr
			evaluated.OK = command.OK && evaluated.OK
			if !command.OK {
				message := fmt.Sprintf("benchmark command exited with code %d", command.ExitCode)
				if command.TimedOut {
					message = "benchmark command timed out"
				}
				evaluated.Failures = append(evaluated.Failures, benchmark.Failure{
					Metric: "(benchmark command)", Reason: "command_failed", Message: message,
				})
				evaluated.OK = false
			}
			benchmarkResult = evaluated
		}
	}

	securityResult := SecurityResult{
		OK: true, Skipped: true,
		Secrets:  security.SecretResult{OK: true, Findings: []security.Finding{}},
		Scanners: security.ScannerResult{OK: true, Findings: []security.Finding{}, Scanners: []string{}, Warnings: []string{}},
	}
	if codeOK && cfg.Acceptance.Security.Enabled {
		secrets := security.SecretResult{OK: true, Findings: []security.Finding{}}
		if cfg.Acceptance.Security.RequireNoSecret {
			secrets = security.ScanSecrets(cwd, cfg)
		}
		scanners := security.RunScanners(ctx, cfg, cwd, runDir)
		securityResult = SecurityResult{
			OK: secrets.OK && scanners.OK, Skipped: false,
			Secrets: secrets, Scanners: scanners,
		}
	}

	return Deterministic{
		OK:         codeOK && benchmarkResult.OK && securityResult.OK,
		CodeOK:     codeOK,
		Cwd:        cwd,
		Coverage:   coverageResult,
		Benchmark:  benchmarkResult,
		Security:   securityResult,
		Commands:   commands,
		Resolved:   *resolved,
		DurationMs: time.Since(started).Milliseconds(),
	}
}

// NeedsBenchmarkBootstrap reports whether the benchmarker stage must run.
func NeedsBenchmarkBootstrap(cfg config.Config, resolved Resolved) bool {
	return cfg.Acceptance.Benchmarks.Required && resolved.Commands["benchmark"] == nil
}

// NeedsTestBootstrap reports whether the tester must create a test command.
func NeedsTestBootstrap(cfg config.Config, resolved Resolved) bool {
	return cfg.Acceptance.Tests && resolved.Commands["test"] == nil
}

// ReviewFinding is one independent-review finding.
type ReviewFinding struct {
	Severity       string `json:"severity"`
	Blocking       bool   `json:"blocking"`
	Title          string `json:"title"`
	File           string `json:"file,omitempty"`
	Line           int    `json:"line,omitempty"`
	Recommendation string `json:"recommendation,omitempty"`
}

// ReviewReport is the parsed `.forge/review.json` contract.
type ReviewReport struct {
	Missing            bool            `json:"missing"`
	Verdict            string          `json:"verdict"`
	Summary            string          `json:"summary"`
	DesignChangeNeeded bool            `json:"design_change_needed"`
	Findings           []ReviewFinding `json:"findings"`
}

// ReviewGate is the enforced review outcome.
type ReviewGate struct {
	OK                 bool            `json:"ok"`
	Verdict            string          `json:"verdict"`
	Blocking           []ReviewFinding `json:"blocking"`
	Findings           []ReviewFinding `json:"findings"`
	DesignChangeNeeded bool            `json:"designChangeNeeded"`
	Summary            string          `json:"summary,omitempty"`
	Message            string          `json:"message,omitempty"`
	Report             *ReviewReport   `json:"-"`
}

// ReviewGate enforces the independent review verdict and blocking findings.
func ReviewGateFor(cfg config.Config, report *ReviewReport) ReviewGate {
	if report == nil || report.Missing {
		return ReviewGate{
			OK: false, Verdict: "missing",
			Blocking: []ReviewFinding{{
				Severity:       "high",
				Title:          "Independent review did not produce a valid .forge/review.json report",
				File:           ".forge/review.json",
				Recommendation: "Re-run the review stage and ensure it writes the required JSON report.",
			}},
			Findings: []ReviewFinding{},
			Report:   report,
			Message:  "verdict=missing, blocking findings=1",
		}
	}
	findings := report.Findings
	if findings == nil {
		findings = []ReviewFinding{}
	}
	var explicit, inferred []ReviewFinding
	for _, finding := range findings {
		if finding.Blocking {
			explicit = append(explicit, finding)
		} else if config.SeverityRank(finding.Severity) >= config.SeverityRank("high") {
			inferred = append(inferred, finding)
		}
	}
	blocking := inferred
	if len(explicit) > 0 {
		blocking = explicit
	}
	verdict := strings.ToLower(report.Verdict)
	ok := verdict == "approve" && len(blocking) <= cfg.Acceptance.Review.MaxBlockingFindings
	message := fmt.Sprintf("verdict=%s, blocking findings=%d", valueOr(verdict, "missing"), len(blocking))
	return ReviewGate{
		OK: ok, Verdict: valueOr(verdict, "missing"), Blocking: blocking, Findings: findings,
		DesignChangeNeeded: report.DesignChangeNeeded, Summary: report.Summary, Message: message, Report: report,
	}
}

// FormatRunSummary renders a compact one-line summary of a gate evaluation.
func FormatRunSummary(det Deterministic) string {
	passed, total := 0, 0
	var failed []string
	for _, command := range det.Commands {
		if command.Status == "skip" {
			continue
		}
		total++
		if command.OK {
			passed++
		} else {
			failed = append(failed, command.Name)
		}
	}
	if !det.Benchmark.Skipped {
		total++
		if det.Benchmark.OK {
			passed++
		} else {
			failed = append(failed, "benchmark")
		}
	}
	summary := fmt.Sprintf("%d/%d checks passed", passed, total)
	if det.Coverage.Percent != nil && !det.Coverage.Skipped {
		summary += fmt.Sprintf(" · coverage %g%%", *det.Coverage.Percent)
	}
	if len(failed) > 0 {
		summary += " · failing: " + strings.Join(failed, ", ")
	}
	return summary
}

// FormatGateSummary renders a compact one-line gate summary.
func FormatGateSummary(det Deterministic) string {
	var rows []string
	for _, command := range det.Commands {
		if command.Status == "skip" {
			continue
		}
		rows = append(rows, command.Name+": "+command.Status)
	}
	if !det.Coverage.Skipped {
		status := "pass"
		if !det.Coverage.OK {
			status = "fail"
		}
		rows = append(rows, fmt.Sprintf("coverage: %s (%s)", status, det.Coverage.Message))
	}
	if !det.Benchmark.Skipped {
		status := "pass"
		if !det.Benchmark.OK {
			status = "fail"
		}
		rows = append(rows, "benchmark: "+status)
	}
	if !det.Security.Skipped {
		status := "pass"
		if !det.Security.OK {
			status = "fail"
		}
		rows = append(rows, fmt.Sprintf("security: %s (%d secrets, %d scanner findings)",
			status, len(det.Security.Secrets.Findings), len(det.Security.Scanners.Findings)))
	}
	return strings.Join(rows, "  |  ")
}

// FormatGateReport renders the detailed report fed back to the next stage.
func FormatGateReport(det Deterministic, includeOutput bool, maxOutput int) string {
	if maxOutput <= 0 {
		maxOutput = 4000
	}
	lines := []string{"### Machine-checked gates"}
	for _, command := range det.Commands {
		if command.Status == "skip" {
			continue
		}
		line := "- " + command.Name + ": " + strings.ToUpper(command.Status)
		if command.Message != "" {
			line += " — " + command.Message
		}
		lines = append(lines, line)
		if includeOutput && !command.OK && (command.Stdout != "" || command.Stderr != "") {
			output := strings.TrimSpace(util.Tail(command.Stdout+"\n"+command.Stderr, maxOutput))
			if output != "" {
				lines = append(lines, "```text", output, "```")
			}
		}
	}
	if !det.Coverage.Skipped {
		status := "PASS"
		if !det.Coverage.OK {
			status = "FAIL"
		}
		lines = append(lines, fmt.Sprintf("- coverage: %s — %s", status, det.Coverage.Message))
	}
	if !det.Benchmark.Skipped {
		status := "PASS"
		if !det.Benchmark.OK {
			status = "FAIL"
		}
		lines = append(lines, fmt.Sprintf("- benchmark: %s — %s", status, benchmark.Format(det.Benchmark)))
		for _, failure := range det.Benchmark.Failures {
			lines = append(lines, "  - "+failure.Message)
		}
	}
	if !det.Security.Skipped {
		secretStatus := "PASS"
		if !det.Security.Secrets.OK {
			secretStatus = "FAIL"
		}
		lines = append(lines, "- secret scan: "+secretStatus)
		for _, finding := range det.Security.Secrets.Findings {
			lines = append(lines, fmt.Sprintf("  - [%s] %s — %s", finding.Severity, finding.Title, finding.Recommendation))
		}
		scannerStatus := "PASS"
		if !det.Security.Scanners.OK {
			scannerStatus = "FAIL"
		}
		lines = append(lines, "- dependency/code scanners: "+scannerStatus)
		for _, warning := range det.Security.Scanners.Warnings {
			lines = append(lines, "  - warning: "+warning)
		}
	}
	return strings.Join(lines, "\n")
}

// FormatSecurityGate renders the security review outcome.
func FormatSecurityGate(gate security.Gate) string {
	lines := []string{
		"### Security review",
		fmt.Sprintf("- verdict: %s", passFail(gate.OK)),
		fmt.Sprintf("- max allowed severity: %s", gate.MaxAllowed),
		fmt.Sprintf("- findings: %d", len(gate.Findings)),
		fmt.Sprintf("- blocking: %d", len(gate.Blocking)),
	}
	details := security.FormatFindings(gate.Blocking, 30)
	if len(gate.Blocking) == 0 {
		details = security.FormatFindings(gate.Findings, 30)
	}
	if details != "" && details != "No security findings." {
		lines = append(lines, "", details)
	}
	return strings.Join(lines, "\n")
}

// FormatReviewGate renders the independent review outcome.
func FormatReviewGate(gate ReviewGate) string {
	lines := []string{
		"### Independent review",
		"- verdict: " + gate.Verdict,
		"- result: " + map[bool]string{true: "APPROVED", false: "CHANGES REQUESTED"}[gate.OK],
		fmt.Sprintf("- blocking findings: %d", len(gate.Blocking)),
		"- design change needed: " + map[bool]string{true: "yes", false: "no"}[gate.DesignChangeNeeded],
	}
	if gate.Summary != "" {
		lines = append(lines, "", gate.Summary)
	}
	if len(gate.Blocking) > 0 {
		lines = append(lines, "", "Blocking findings:")
		for _, finding := range gate.Blocking {
			location := ""
			if finding.File != "" {
				location = " (" + finding.File
				if finding.Line > 0 {
					location += fmt.Sprintf(":%d", finding.Line)
				}
				location += ")"
			}
			lines = append(lines, fmt.Sprintf("- [%s] %s%s", config.NormalizeSeverity(finding.Severity, "medium"), finding.Title, location))
			if finding.Recommendation != "" {
				lines = append(lines, "  "+finding.Recommendation)
			}
		}
	}
	return strings.Join(lines, "\n")
}

// GateSignature is a stable failure fingerprint used for stuck detection.
func GateSignature(det *Deterministic, sec *security.Gate, rev *ReviewGate) string {
	var parts []string
	if det != nil {
		for _, command := range det.Commands {
			if !command.OK && command.Status != "skip" {
				parts = append(parts, fmt.Sprintf("%s:%d", command.Name, command.ExitCode))
			}
		}
		if !det.Coverage.OK && !det.Coverage.Skipped {
			parts = append(parts, "coverage:"+formatPercent(det.Coverage.Percent))
		}
		for _, failure := range det.Benchmark.Failures {
			parts = append(parts, fmt.Sprintf("bench:%s:%s", failure.Metric, failure.Reason))
		}
		for _, finding := range det.Security.Secrets.Findings {
			parts = append(parts, "secret:"+finding.Title)
		}
		for _, finding := range det.Security.Scanners.Findings {
			parts = append(parts, "scan:"+finding.Title)
		}
	}
	if sec != nil {
		for _, finding := range sec.Blocking {
			parts = append(parts, "sec:"+finding.Title)
		}
	}
	if rev != nil {
		for _, finding := range rev.Blocking {
			parts = append(parts, "rev:"+finding.Title)
		}
	}
	if len(parts) == 0 {
		return "ok"
	}
	sort.Strings(parts)
	return strings.Join(parts, "|")
}

func formatPercent(value *float64) string {
	if value == nil {
		return "unknown"
	}
	return fmt.Sprintf("%g", *value)
}

func passFail(ok bool) string {
	if ok {
		return "PASS"
	}
	return "FAIL"
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
