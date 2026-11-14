package orchestrator

import (
	stdctx "context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/soub4i/forge/internal/benchmark"
	"github.com/soub4i/forge/internal/config"
	snapshotpkg "github.com/soub4i/forge/internal/context"
	"github.com/soub4i/forge/internal/gates"
	gitpkg "github.com/soub4i/forge/internal/git"
	"github.com/soub4i/forge/internal/models"
	"github.com/soub4i/forge/internal/prompts"
	"github.com/soub4i/forge/internal/reports"
	"github.com/soub4i/forge/internal/runner"
	"github.com/soub4i/forge/internal/security"
	"github.com/soub4i/forge/internal/util"
)

// StageResult is the full outcome of one model stage.
type StageResult struct {
	Stage      string
	Iteration  int
	Model      string
	OK         bool
	DurationMs int64
	Tokens     util.Tokens
	CostUSD    float64
	SessionID  string
	Errors     []string
	Text       string
	PromptFile string
	TextFile   string
	EventsFile string
	Ledger     models.LedgerEntry
}

// IterationState captures one iteration of the loop.
type IterationState struct {
	Number       int
	StartedAt    string
	EndedAt      string
	Stages       []StageResult
	Gates        *gates.Deterministic
	SecurityGate *security.Gate
	ReviewGate   *gates.ReviewGate
	FeedbackFile string
}

// Result is the outcome of a harness run.
type Result struct {
	Success     bool
	ExitReason  string
	Error       string
	Report      *reports.Report
	ReportFiles reports.ReportFiles
	Run         *reports.Run
}

// Options configures RunHarness.
type Options struct {
	Cwd        string
	Config     config.Config
	ConfigFile string
	Verbose    bool
}

type stageContext struct {
	Feedback      *string
	HasFeedback   bool
	Resolved      *gates.Resolved
	GateReport    string
	SecurityGate  *security.Gate
	ReviewGate    *gates.ReviewGate
	ScannerResult []security.Finding
	Benchmark     *benchmark.Result
	History       []string
	OverrideModel string
}

type integrityResult struct {
	OK      bool
	Message string
}

type releaseOutcome struct {
	OK      bool
	Skipped bool
	Missing []string
	Reason  string
	Stage   *StageResult
}

func checkConfigIntegrity(configFile, expected string) integrityResult {
	if configFile == "" {
		return integrityResult{OK: true}
	}
	actual, err := config.FingerprintFile(configFile)
	if err != nil {
		return integrityResult{OK: false, Message: "Harness config became unreadable or invalid: " + configFile}
	}
	if actual != expected {
		return integrityResult{
			OK: false,
			Message: "The harness config changed during the run. Models must not edit forge.config.json acceptance, commands, models, or budget. " +
				"Restore the original values; if you intended to change the goal, start a new run.",
		}
	}
	return integrityResult{OK: true}
}

func stageLine(stage, model string, iteration int) string {
	return fmt.Sprintf("→ %-18s iter %d  %s", config.StageLabels[stage], iteration, model)
}

func stageResultLine(result StageResult) string {
	mark := "✓"
	if !result.OK {
		mark = "✗"
	}
	line := fmt.Sprintf("  %s %s  %s in / %s out  %s",
		mark, util.FormatDuration(result.DurationMs),
		util.FormatNumber(result.Tokens.Input), util.FormatNumber(result.Tokens.Output),
		util.FormatUSD(result.CostUSD))
	if len(result.Errors) > 0 {
		line += "  " + result.Errors[0]
	}
	return line
}

func historySummary(iteration int, det *gates.Deterministic, sec *security.Gate, rev *gates.ReviewGate) string {
	var failed []string
	if det != nil {
		for _, command := range det.Commands {
			if !command.OK && command.Status != "skip" {
				failed = append(failed, command.Name)
			}
		}
	}
	codeStatus := "unknown"
	if det != nil {
		codeStatus = "pass"
		if !det.CodeOK {
			codeStatus = "fail"
			if len(failed) > 0 {
				codeStatus += " (" + strings.Join(failed, ", ") + ")"
			}
		}
	}
	parts := []string{"Iteration " + fmt.Sprintf("%d", iteration) + ": code gates " + codeStatus}
	if det != nil && !det.Coverage.Skipped {
		percent := "?"
		if det.Coverage.Percent != nil {
			percent = fmt.Sprintf("%g", *det.Coverage.Percent)
		}
		parts = append(parts, "coverage "+percent+"%")
	}
	if det != nil && !det.Benchmark.Skipped {
		status := "pass"
		if !det.Benchmark.OK {
			status = "fail"
		}
		parts = append(parts, "benchmark "+status)
	}
	if sec != nil {
		status := "pass"
		if !sec.OK {
			status = "fail"
		}
		parts = append(parts, "security "+status)
	}
	if rev != nil {
		parts = append(parts, "review "+rev.Verdict)
	}
	return strings.Join(parts, "; ")
}

func compileFeedback(det *gates.Deterministic, sec *security.Gate, rev *gates.ReviewGate, integrity *integrityResult) string {
	blocks := []string{"# Failure feedback for the next iteration"}
	if det != nil && !det.OK {
		blocks = append(blocks, gates.FormatGateReport(*det, true, 4000))
	}
	if sec != nil && !sec.OK {
		blocks = append(blocks, gates.FormatSecurityGate(*sec))
	}
	if rev != nil && !rev.OK {
		blocks = append(blocks, gates.FormatReviewGate(*rev))
	}
	if integrity != nil && !integrity.OK {
		blocks = append(blocks, "### Harness config integrity\n\n"+integrity.Message)
	}
	var actions []string
	if det != nil {
		for _, command := range det.Commands {
			if command.OK || command.Status == "skip" {
				continue
			}
			commandName := command.Command
			if commandName == "" {
				commandName = "(missing command)"
			}
			action := fmt.Sprintf("Fix the `%s` gate: `%s` must pass.", command.Name, commandName)
			if command.LogFile != "" {
				action += fmt.Sprintf(" See `%s`.", command.LogFile)
			}
			actions = append(actions, action)
		}
		if !det.Coverage.OK && !det.Coverage.Skipped {
			percent := "unknown"
			if det.Coverage.Percent != nil {
				percent = fmt.Sprintf("%g", *det.Coverage.Percent)
			}
			actions = append(actions, fmt.Sprintf("Raise coverage from %s%% to at least %g%% with meaningful tests.", percent, det.Coverage.Required))
		}
		for _, failure := range det.Benchmark.Failures {
			actions = append(actions, "Fix benchmark: "+failure.Message)
		}
		if !det.Security.Skipped && !det.Security.OK {
			for _, finding := range append(append([]security.Finding{}, det.Security.Secrets.Findings...), det.Security.Scanners.Findings...) {
				actions = append(actions, fmt.Sprintf("Remediate [%s] %s: %s", finding.Severity, finding.Title, finding.Recommendation))
			}
		}
	}
	if sec != nil {
		for _, finding := range sec.Blocking {
			actions = append(actions, fmt.Sprintf("Fix blocking security finding: [%s] %s. %s", finding.Severity, finding.Title, finding.Recommendation))
		}
	}
	if rev != nil {
		for _, finding := range rev.Blocking {
			actions = append(actions, fmt.Sprintf("Address blocking review finding: [%s] %s. %s", finding.Severity, finding.Title, finding.Recommendation))
		}
	}
	if integrity != nil && !integrity.OK {
		actions = append(actions, integrity.Message)
	}
	if len(actions) == 0 {
		actions = append(actions, "Re-run the failing gates and verify the fix.")
	}
	blocks = append(blocks, "## Required next actions\n\n- "+strings.Join(actions, "\n- "))
	return util.Truncate(strings.Join(blocks, "\n\n"), 32000)
}

func writeFeedback(run *reports.Run, iteration int, text string) (string, error) {
	file := filepath.Join(run.Dir, "iterations", fmt.Sprintf("%02d-feedback.md", iteration))
	return file, util.WriteText(file, text)
}

func modelOverrideForStage(cfg config.Config, stage string, escalated bool) string {
	if !escalated {
		return ""
	}
	if stage != "implementer" && stage != "fixer" {
		return ""
	}
	return cfg.Models["architect"].Model
}

// RunHarness drives the goal-driven loop until gates and review pass.
func RunHarness(ctx stdctx.Context, options Options) (*Result, error) {
	cwd := options.Cwd
	cfg := options.Config
	configFile := options.ConfigFile

	available := models.ListAvailable(ctx)
	if available == nil {
		util.Warn("Could not list OpenCode models; using configured model ids.")
	}

	run := reports.CreateRun(cwd, cfg, configFile)
	ledger := models.NewLedger(cfg.Budget)
	integrityHash := ""
	if configFile != "" {
		hash, err := config.FingerprintFile(configFile)
		if err != nil {
			util.Warn("Could not fingerprint config file: " + err.Error())
		} else {
			integrityHash = hash
		}
	}
	initialGit := snapshotpkg.GitFactsFor(cwd, "")
	baseCommit := ""
	if initialGit.Repo {
		baseCommit = initialGit.Head
	}
	maxIterations := cfg.Budget.MaxIterations
	iterations := []IterationState{}
	iteration := 0
	feedback := ""
	lastSignature := ""
	stuck := 0
	designChange := false
	success := false
	exitReason := ""
	errorMessage := ""
	var finalDet *gates.Deterministic
	var finalSecurityGate *security.Gate
	var finalReviewGate *gates.ReviewGate
	var finalBenchmark *benchmark.Result
	var releaseResult *releaseOutcome

	if cfg.Git.Checkpoint {
		if !gitpkg.IsRepo(cwd) {
			util.Warn("git.checkpoint is enabled but this workspace is not a git repository. Checkpoints are disabled.")
		} else {
			gitpkg.EnsureForgeExcluded(cwd)
		}
	}

	modelsMap := map[string]string{}
	for stage, entry := range cfg.Models {
		modelsMap[stage] = entry.Model
	}
	_ = util.WriteJSON(run.RunJSON, map[string]any{
		"id": run.ID, "goal": cfg.Goal, "workspace": cwd, "configFile": configFile,
		"startedAt": run.StartedAt, "models": modelsMap, "budget": cfg.Budget, "acceptance": cfg.Acceptance,
	})
	_, _ = reports.EnsureGoalDoc(cwd, cfg)

	util.Log("")
	util.Log("FORGE  run " + run.ID)
	util.Log("Goal:      " + cfg.Goal)
	util.Log("Workspace: " + cwd)
	configLabel := configFile
	if configLabel == "" {
		configLabel = "(defaults)"
	}
	util.Log("Config:    " + configLabel)
	var modelPairs []string
	for stage, entry := range cfg.Models {
		parts := strings.Split(entry.Model, "/")
		modelPairs = append(modelPairs, stage+"="+parts[len(parts)-1])
	}
	util.Log("Models:    " + strings.Join(modelPairs, "  "))
	iterationLabel := "unlimited iterations"
	if maxIterations > 0 {
		iterationLabel = fmt.Sprintf("%d iterations", maxIterations)
	}
	timeLabel := "no time limit"
	if cfg.Budget.MaxMinutes > 0 {
		timeLabel = fmt.Sprintf("%g min", cfg.Budget.MaxMinutes)
	}
	costLabel := "no cost limit"
	if cfg.Budget.MaxUSD > 0 {
		costLabel = util.FormatUSD(cfg.Budget.MaxUSD)
	}
	util.Log(fmt.Sprintf("Budget:    %s  |  %s  |  %s", iterationLabel, timeLabel, costLabel))
	util.Log("")

	resolvedAtStart := gates.ResolveCommands(cfg, cwd)
	if cfg.Acceptance.Tests && resolvedAtStart.Commands["test"] == nil {
		util.Log("note: no test command detected yet; the tester stage will create one.")
	}
	if cfg.Acceptance.Benchmarks.Required && resolvedAtStart.Commands["benchmark"] == nil {
		util.Log("note: no benchmark command detected yet; the benchmarker stage will create one.")
	}

	install := gates.RunInstall(ctx, cfg, cwd, run.Dir, &resolvedAtStart)
	if install.Status != "skip" {
		if !install.OK {
			exitReason = "install_failed"
			errorMessage = "Install command failed: " + install.Command
			util.Error(errorMessage)
			if install.LogFile != "" {
				util.Log("  log: " + install.LogFile)
			}
		} else {
			util.Log(fmt.Sprintf("  install: ok (%s)", util.FormatDuration(install.DurationMs)))
		}
	}

	history := func() []string {
		out := make([]string, 0, len(iterations))
		for _, item := range iterations {
			out = append(out, historySummary(item.Number, item.Gates, item.SecurityGate, item.ReviewGate))
		}
		return out
	}

	executeStage := func(stage string, sc stageContext) (StageResult, error) {
		resolvedModel := models.Resolve(cfg, stage, available, sc.OverrideModel)
		if len(resolvedModel.Unavailable) > 0 {
			util.Warn(fmt.Sprintf("%s: configured model(s) unavailable: %s; using %s",
				stage, strings.Join(resolvedModel.Unavailable, ", "), resolvedModel.Selected))
		}
		attempts := []string{resolvedModel.Selected}
		for _, model := range resolvedModel.Candidates {
			if model != resolvedModel.Selected {
				attempts = append(attempts, model)
			}
		}
		limit := 1 + cfg.Loop.ModelRetries
		if len(attempts) > limit {
			attempts = attempts[:limit]
		}

		resolved := sc.Resolved
		if resolved == nil {
			r := gates.ResolveCommands(cfg, cwd)
			resolved = &r
		}
		snapshot := snapshotpkg.BuildSnapshot(cwd, cfg, baseCommit)
		stageFeedback := feedback
		if sc.HasFeedback {
			stageFeedback = *sc.Feedback
		}
		promptContext := prompts.Context{
			Cwd: cwd, Config: cfg, Iteration: iteration, MaxIterations: maxIterations,
			Resolved: *resolved, Snapshot: snapshot, Feedback: stageFeedback,
			GateReport: sc.GateReport, SecurityGate: sc.SecurityGate, ReviewGate: sc.ReviewGate,
			ScannerResults: sc.ScannerResult, Benchmark: sc.Benchmark, History: sc.History,
		}
		prompt := prompts.Build(stage, promptContext)
		eventsFile := filepath.Join(run.Dir, "events", fmt.Sprintf("%02d-%s.jsonl", iteration, stage))

		var lastResult StageResult
		var lastErr error
		for _, model := range attempts {
			util.Log(stageLine(stage, model, iteration))
			result := runner.RunStage(ctx, runner.Options{
				Model: model, Prompt: prompt, Cwd: cwd,
				Title:   fmt.Sprintf("forge %s %s %d", run.ID, stage, iteration),
				Timeout: durationMinutes(cfg.Loop.StageTimeoutMinutes),
				LogFile: eventsFile,
				Env: map[string]string{
					"FORGE_RUN_ID": run.ID, "FORGE_ITERATION": fmt.Sprintf("%d", iteration), "FORGE_STAGE": stage,
				},
			})
			entry := ledger.Add(cfg, stage, iteration, model, result.CostUSD, result.Tokens, result.DurationMs)
			promptFile, textFile, _, err := reports.WriteStageArtifacts(run, iteration, stage, prompt, reports.StageArtifactInput{
				Model: model, OK: result.OK, DurationMs: result.DurationMs, SessionID: result.SessionID,
				Tokens: result.Tokens, CostUSD: result.CostUSD, Errors: result.Errors, Text: result.Text,
			})
			if err != nil {
				util.Warn("could not write stage artifacts: " + err.Error())
			}
			lastResult = StageResult{
				Stage: stage, Iteration: iteration, Model: model, OK: result.OK,
				DurationMs: result.DurationMs, Tokens: result.Tokens, CostUSD: result.CostUSD,
				SessionID: result.SessionID, Errors: result.Errors, Text: result.Text,
				PromptFile: promptFile, TextFile: textFile, EventsFile: eventsFile, Ledger: entry,
			}
			util.Log(stageResultLine(lastResult))
			if lastResult.OK {
				return lastResult, nil
			}
			lastErr = fmt.Errorf("stage %q failed", stage)
			if len(result.Errors) > 0 {
				util.Warn(stage + ": " + util.Truncate(strings.Join(result.Errors, " | "), 500))
			}
		}
		if lastErr == nil {
			lastErr = fmt.Errorf("stage %q failed", stage)
		}
		return lastResult, fmt.Errorf("stage %q failed after %d model attempt(s)", stage, len(attempts))
	}

	verifyReleaseArtifacts := func() []string {
		required := []string{"README.md", "CHANGELOG.md", "docs/RELEASE.md"}
		var missing []string
		for _, rel := range required {
			if !util.FileExists(filepath.Join(cwd, rel)) {
				missing = append(missing, rel)
			}
		}
		return missing
	}

	executeRelease := func() releaseOutcome {
		if !cfg.Release.Enabled {
			return releaseOutcome{OK: true, Skipped: true}
		}
		gateReport := ""
		if finalDet != nil {
			gateReport = gates.FormatGateReport(*finalDet, false, 0)
		}
		stageResult, err := executeStage("release", stageContext{
			GateReport: gateReport, SecurityGate: finalSecurityGate, ReviewGate: finalReviewGate,
			Benchmark: finalBenchmark, History: history(),
		})
		if err != nil {
			return releaseOutcome{OK: false, Reason: "release_failed", Stage: &stageResult}
		}
		missing := verifyReleaseArtifacts()
		if len(missing) > 0 {
			util.Warn("release artifacts missing: " + strings.Join(missing, ", ") + "; retrying release stage once.")
			retryFeedback := "The release stage did not create these required artifacts: " + strings.Join(missing, ", ") + ". Create them now."
			retryResult, err := executeStage("release", stageContext{
				Feedback: &retryFeedback, HasFeedback: true,
				GateReport: gateReport, SecurityGate: finalSecurityGate, ReviewGate: finalReviewGate,
				Benchmark: finalBenchmark,
			})
			stageResult = retryResult
			if err != nil {
				return releaseOutcome{OK: false, Reason: "release_failed", Stage: &stageResult}
			}
			missing = verifyReleaseArtifacts()
		}
		if len(missing) > 0 {
			return releaseOutcome{OK: false, Reason: "release_incomplete", Missing: missing, Stage: &stageResult}
		}
		if cfg.Release.Tag {
			var pkg map[string]any
			if util.ReadJSON(filepath.Join(cwd, "package.json"), &pkg) == nil {
				if version, ok := pkg["version"].(string); ok && version != "" {
					tagged := gitpkg.Tag(cwd, "v"+version, "Forge release v"+version)
					if !tagged.OK {
						util.Warn("could not create git tag: " + tagged.Reason)
					} else if !tagged.Skipped {
						util.Log("  git tag: " + tagged.Tag)
					}
				}
			}
		}
		return releaseOutcome{OK: true, Stage: &stageResult}
	}

loop:
	for exitReason == "" {
		if maxIterations > 0 && iteration >= maxIterations {
			exitReason = "max_iterations"
			break
		}
		budget := ledger.OverBudget()
		if budget.Over {
			exitReason = budget.Reason
			errorMessage = budget.Detail
			break
		}

		iteration++
		state := IterationState{
			Number: iteration, StartedAt: util.NowISO(),
			Stages: []StageResult{},
		}
		util.Log("")
		if maxIterations > 0 {
			util.Log(fmt.Sprintf("Iteration %d/%d", iteration, maxIterations))
		} else {
			util.Log(fmt.Sprintf("Iteration %d", iteration))
		}

		var det *gates.Deterministic
		var secGate *security.Gate
		var revGate *gates.ReviewGate
		escalated := false
		stageFailed := false

		runStage := func(stage string, sc stageContext) (StageResult, bool) {
			result, err := executeStage(stage, sc)
			if err != nil {
				errorMessage = err.Error()
				exitReason = "stage_failed"
				state.EndedAt = util.NowISO()
				state.Gates = det
				state.SecurityGate = secGate
				state.ReviewGate = revGate
				util.Error(err.Error())
				stageFailed = true
				return result, false
			}
			state.Stages = append(state.Stages, result)
			return result, true
		}

		architectureMissing := !util.FileExists(filepath.Join(cwd, "docs", "ARCHITECTURE.md"))
		designMissing := !util.FileExists(filepath.Join(cwd, "docs", "DESIGN.md"))
		if iteration == 1 || architectureMissing || designChange {
			if _, ok := runStage("architect", stageContext{History: []string{}}); !ok {
				break loop
			}
		}
		if iteration == 1 || designMissing || designChange {
			if _, ok := runStage("designer", stageContext{History: []string{}}); !ok {
				break loop
			}
		}
		designChange = false

		if stuck >= cfg.Loop.EscalateAfter && feedback != "" {
			triage, ok := runStage("triage", stageContext{Feedback: &feedback, HasFeedback: true, History: history()})
			if !ok {
				break loop
			}
			feedback = feedback + "\n\n## Triage brief\n\n" + triage.Text
			escalated = true
			util.Log("  escalation active: implementer will use " + cfg.Models["architect"].Model)
		}

		if _, ok := runStage("implementer", stageContext{
			Feedback: &feedback, HasFeedback: true, History: history(),
			OverrideModel: modelOverrideForStage(cfg, "implementer", escalated),
		}); !ok {
			break loop
		}
		if _, ok := runStage("tester", stageContext{
			Feedback: &feedback, HasFeedback: true, History: history(),
		}); !ok {
			break loop
		}

		resolved := gates.ResolveCommands(cfg, cwd)
		if cfg.Acceptance.Benchmarks.Required && (iteration == 1 || gates.NeedsBenchmarkBootstrap(cfg, resolved)) {
			if _, ok := runStage("benchmarker", stageContext{Resolved: &resolved, History: history()}); !ok {
				break loop
			}
			resolved = gates.ResolveCommands(cfg, cwd)
		}

		detValue := gates.EvaluateDeterministic(ctx, cfg, cwd, run.Dir, iteration, &resolved)
		det = &detValue
		integrity := checkConfigIntegrity(configFile, integrityHash)

		if !det.OK {
			for repair := 0; repair < cfg.Loop.RepairAttempts && !det.OK; repair++ {
				if _, ok := runStage("fixer", stageContext{
					Feedback: &feedback, HasFeedback: true,
					GateReport: gates.FormatGateReport(*det, true, 4000), History: history(),
					OverrideModel: modelOverrideForStage(cfg, "fixer", escalated),
				}); !ok {
					break
				}
				if stageFailed {
					break
				}
				resolved = gates.ResolveCommands(cfg, cwd)
				detValue = gates.EvaluateDeterministic(ctx, cfg, cwd, run.Dir, iteration, &resolved)
				det = &detValue
			}
			integrity = checkConfigIntegrity(configFile, integrityHash)
		}
		if stageFailed {
			break loop
		}

		if !det.OK || !integrity.OK {
			state.Gates = det
			state.EndedAt = util.NowISO()
			text := compileFeedback(det, nil, nil, &integrity)
			feedbackFile, err := writeFeedback(run, iteration, text)
			if err != nil {
				util.Warn("could not write feedback: " + err.Error())
			}
			if rel, err := filepath.Rel(cwd, feedbackFile); err == nil {
				state.FeedbackFile = rel
			}
			iterations = append(iterations, state)
			feedback = text
			signature := gates.GateSignature(det, nil, nil)
			if !integrity.OK {
				signature += "|config"
			}
			if signature == lastSignature {
				stuck++
			} else {
				stuck = 0
			}
			lastSignature = signature
			util.Log("  goal not met: " + gates.FormatGateSummary(*det))
			if !integrity.OK {
				util.Error(integrity.Message)
			}
			checkpointIfEnabled(cfg, cwd, fmt.Sprintf("forge: iteration %d (gates fail)", iteration))
			continue
		}

		finalDet = det
		benchmarkCopy := det.Benchmark
		finalBenchmark = &benchmarkCopy

		var scannerFindings []security.Finding
		if cfg.Acceptance.Security.Enabled {
			scannerFindings = append(scannerFindings, det.Security.Secrets.Findings...)
			scannerFindings = append(scannerFindings, det.Security.Scanners.Findings...)
			reports.ClearReport(cwd, ".forge/security.json")
			securityStage, ok := runStage("security", stageContext{
				Resolved: &resolved, GateReport: gates.FormatGateReport(*det, false, 0),
				ScannerResult: scannerFindings, Benchmark: &det.Benchmark, History: history(),
			})
			if !ok {
				break loop
			}
			securityReport := reports.LoadSecurityReport(cwd, securityStage.Text)
			secGateValue := security.EvaluateGate(cfg, scannerFindings, securityReport)
			secGate = &secGateValue
			state.SecurityGate = secGate
			finalSecurityGate = secGate

			if !secGate.OK {
				state.Gates = det
				state.EndedAt = util.NowISO()
				text := compileFeedback(det, secGate, nil, nil)
				feedbackFile, err := writeFeedback(run, iteration, text)
				if err != nil {
					util.Warn("could not write feedback: " + err.Error())
				}
				if rel, err := filepath.Rel(cwd, feedbackFile); err == nil {
					state.FeedbackFile = rel
				}
				iterations = append(iterations, state)
				feedback = text
				signature := gates.GateSignature(det, secGate, nil)
				if signature == lastSignature {
					stuck++
				} else {
					stuck = 0
				}
				lastSignature = signature
				util.Log(fmt.Sprintf("  security gate failed: %d blocking finding(s)", len(secGate.Blocking)))
				checkpointIfEnabled(cfg, cwd, fmt.Sprintf("forge: iteration %d (security fail)", iteration))
				continue
			}
		} else {
			secGate = &security.Gate{
				OK: true, Findings: []security.Finding{}, Blocking: []security.Finding{},
				MaxAllowed: cfg.Acceptance.Security.MaxSeverity,
			}
			state.SecurityGate = secGate
			finalSecurityGate = secGate
		}

		if cfg.Acceptance.Review.Enabled {
			reports.ClearReport(cwd, ".forge/review.json")
			reviewer, ok := runStage("reviewer", stageContext{
				Resolved: &resolved, GateReport: gates.FormatGateReport(*det, false, 0),
				SecurityGate: secGate, Benchmark: &det.Benchmark, History: history(),
			})
			if !ok {
				break loop
			}
			reviewReport := reports.LoadReviewReport(cwd, reviewer.Text)
			revGateValue := gates.ReviewGateFor(cfg, reviewReport)
			revGate = &revGateValue
			state.ReviewGate = revGate
			finalReviewGate = revGate
			if !revGate.OK {
				state.Gates = det
				state.EndedAt = util.NowISO()
				text := compileFeedback(det, secGate, revGate, nil)
				feedbackFile, err := writeFeedback(run, iteration, text)
				if err != nil {
					util.Warn("could not write feedback: " + err.Error())
				}
				if rel, err := filepath.Rel(cwd, feedbackFile); err == nil {
					state.FeedbackFile = rel
				}
				iterations = append(iterations, state)
				feedback = text
				designChange = revGate.DesignChangeNeeded
				signature := gates.GateSignature(det, secGate, revGate)
				if signature == lastSignature {
					stuck++
				} else {
					stuck = 0
				}
				lastSignature = signature
				util.Log(fmt.Sprintf("  review requested changes: %d blocking finding(s)", len(revGate.Blocking)))
				checkpointIfEnabled(cfg, cwd, fmt.Sprintf("forge: iteration %d (review changes)", iteration))
				continue
			}
		}

		finalIntegrity := checkConfigIntegrity(configFile, integrityHash)
		if !finalIntegrity.OK {
			state.Gates = det
			state.EndedAt = util.NowISO()
			text := compileFeedback(det, secGate, revGate, &finalIntegrity)
			feedbackFile, err := writeFeedback(run, iteration, text)
			if err != nil {
				util.Warn("could not write feedback: " + err.Error())
			}
			if rel, err := filepath.Rel(cwd, feedbackFile); err == nil {
				state.FeedbackFile = rel
			}
			iterations = append(iterations, state)
			feedback = text
			stuck++
			util.Error(finalIntegrity.Message)
			continue
		}

		state.Gates = det
		state.EndedAt = util.NowISO()
		iterations = append(iterations, state)
		success = true
		checkpointIfEnabled(cfg, cwd, fmt.Sprintf("forge: iteration %d (goal met)", iteration))
		util.Log("")
		util.Log(fmt.Sprintf("  all gates passed on iteration %d.", iteration))
		break
	}

	if success {
		outcome := executeRelease()
		releaseResult = &outcome
		if !outcome.OK {
			success = false
			exitReason = outcome.Reason
			if exitReason == "" {
				exitReason = "release_failed"
			}
			errorMessage = "Release stage did not create: " + strings.Join(outcome.Missing, ", ")
			util.Error(errorMessage)
		}
	}

	if !success && exitReason == "" {
		exitReason = "unknown"
	}
	if !success && exitReason == "max_iterations" && errorMessage == "" {
		errorMessage = fmt.Sprintf("Reached the maximum of %d iterations without meeting the goal.", maxIterations)
	}

	report := reports.BuildReport(reports.BuildInput{
		Run: run, Config: cfg, Iterations: toReportIterations(iterations), Ledger: ledger,
		Success: success, ExitReason: exitReason, ConfigFile: configFile,
		SecurityGate: finalSecurityGate, ReviewGate: finalReviewGate, FinalGates: finalDet,
	})
	report.Error = errorMessage
	if releaseResult != nil {
		view := &reports.ReleaseView{OK: releaseResult.OK, Skipped: releaseResult.Skipped, Missing: releaseResult.Missing}
		if releaseResult.Stage != nil {
			view.Stage = &reports.ReleaseStageView{
				Model: releaseResult.Stage.Model, OK: releaseResult.Stage.OK,
				DurationMs: releaseResult.Stage.DurationMs, Tokens: releaseResult.Stage.Tokens,
				CostUSD: releaseResult.Stage.CostUSD,
			}
		}
		report.Release = view
	}

	reportFiles, err := reports.WriteRunReport(run, report)
	if err != nil {
		return nil, err
	}
	reports.RecordRun(cwd, report, reportFiles)
	reports.PrintRunSummary(report, reportFiles)
	if !success && errorMessage != "" {
		util.Error("Reason: " + errorMessage)
	}

	return &Result{
		Success: success, ExitReason: exitReason, Error: errorMessage,
		Report: report, ReportFiles: reportFiles, Run: run,
	}, nil
}

func checkpointIfEnabled(cfg config.Config, cwd, message string) {
	if !cfg.Git.Checkpoint {
		return
	}
	saved := gitpkg.Checkpoint(cwd, message)
	if !saved.OK && !saved.Skipped {
		util.Warn("checkpoint failed: " + saved.Reason)
	}
}

func durationMinutes(minutes int) time.Duration {
	return time.Duration(minutes) * time.Minute
}

func toReportIterations(iterations []IterationState) []reports.ReportIteration {
	out := make([]reports.ReportIteration, 0, len(iterations))
	for _, iteration := range iterations {
		stages := make([]reports.ReportStage, 0, len(iteration.Stages))
		for _, stage := range iteration.Stages {
			stages = append(stages, reports.ReportStage{
				Stage: stage.Stage, Model: stage.Model, OK: stage.OK, DurationMs: stage.DurationMs,
				Tokens: stage.Tokens, CostUSD: stage.CostUSD, SessionID: stage.SessionID,
				Errors: stage.Errors, PromptFile: stage.PromptFile, TextFile: stage.TextFile, EventsFile: stage.EventsFile,
			})
		}
		view := reports.ReportIteration{
			Number: iteration.Number, StartedAt: iteration.StartedAt, EndedAt: iteration.EndedAt,
			Stages: stages, Gates: reports.SanitizeDeterministic(iteration.Gates),
			FeedbackFile: iteration.FeedbackFile,
		}
		if iteration.SecurityGate != nil {
			view.SecurityGate = &reports.SecurityGateView{
				OK: iteration.SecurityGate.OK, MaxAllowed: iteration.SecurityGate.MaxAllowed,
				Findings: iteration.SecurityGate.Findings, Blocking: iteration.SecurityGate.Blocking,
			}
		}
		if iteration.ReviewGate != nil {
			view.ReviewGate = &reports.ReviewGateView{
				OK: iteration.ReviewGate.OK, Verdict: iteration.ReviewGate.Verdict,
				Blocking: iteration.ReviewGate.Blocking, Findings: iteration.ReviewGate.Findings,
				DesignChangeNeeded: iteration.ReviewGate.DesignChangeNeeded, Summary: iteration.ReviewGate.Summary,
			}
		}
		out = append(out, view)
	}
	return out
}
