package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/soub4i/forge/internal/config"
	"github.com/soub4i/forge/internal/gates"
	gitpkg "github.com/soub4i/forge/internal/git"
	"github.com/soub4i/forge/internal/models"
	"github.com/soub4i/forge/internal/orchestrator"
	"github.com/soub4i/forge/internal/reports"
	"github.com/soub4i/forge/internal/security"
	"github.com/soub4i/forge/internal/templates"
	"github.com/soub4i/forge/internal/util"
)

// Version is the Forge harness version.
const Version = "1.2.0"

// Flags is a small permissive flag parser matching the JS harness.
type Flags struct {
	Args   []string
	values map[string][]string
	bools  map[string]bool
}

func parseFlags(args []string) Flags {
	flags := Flags{Args: []string{}, values: map[string][]string{}, bools: map[string]bool{}}
	list := append([]string{}, args...)
	for len(list) > 0 {
		arg := list[0]
		list = list[1:]
		if arg == "--" {
			flags.Args = append(flags.Args, list...)
			break
		}
		if strings.HasPrefix(arg, "--") {
			body := strings.TrimPrefix(arg, "--")
			if index := strings.Index(body, "="); index >= 0 {
				key := body[:index]
				value := body[index+1:]
				flags.values[key] = append(flags.values[key], value)
				continue
			}
			key := body
			if len(list) > 0 && !strings.HasPrefix(list[0], "-") {
				flags.values[key] = append(flags.values[key], list[0])
				list = list[1:]
			} else {
				flags.bools[key] = true
			}
			continue
		}
		if strings.HasPrefix(arg, "-") && len(arg) > 1 {
			flags.bools[strings.TrimPrefix(arg, "-")] = true
			continue
		}
		flags.Args = append(flags.Args, arg)
	}
	return flags
}

// String returns the last value for a flag.
func (f Flags) String(name string) (string, bool) {
	values := f.values[name]
	if len(values) == 0 {
		return "", false
	}
	return values[len(values)-1], true
}

// Bool reports whether a flag was set.
func (f Flags) Bool(name string) bool {
	return f.bools[name] || len(f.values[name]) > 0
}

// List returns every value for a repeatable flag.
func (f Flags) List(name string) []string {
	return f.values[name]
}

func parseNumber(flags Flags, name string) (*float64, error) {
	value, ok := flags.String(name)
	if !ok {
		return nil, nil
	}
	number, err := strconv.ParseFloat(value, 64)
	if err != nil || number < 0 {
		return nil, fmt.Errorf("--%s must be a non-negative number", name)
	}
	return &number, nil
}

func parseModelOverrides(flags Flags) (map[string]string, error) {
	overrides := map[string]string{}
	for _, entry := range flags.List("model") {
		index := strings.Index(entry, "=")
		if index < 0 {
			return nil, fmt.Errorf("--model must be stage=provider/model (got %s)", entry)
		}
		stage := entry[:index]
		model := entry[index+1:]
		if _, ok := config.DefaultModels[stage]; !ok {
			return nil, fmt.Errorf("unknown model stage %q. Valid stages: %s", stage, strings.Join(config.StageNames(), ", "))
		}
		if !strings.Contains(model, "/") {
			return nil, fmt.Errorf("model must look like provider/model (got %s)", model)
		}
		overrides[stage] = model
	}
	return overrides, nil
}

func resolveWorkspace(args []string) (string, error) {
	input := "."
	if len(args) > 0 && args[0] != "" {
		input = args[0]
	}
	resolved, err := filepath.Abs(input)
	if err != nil {
		return "", err
	}
	if !util.DirExists(resolved) {
		return "", fmt.Errorf("workspace does not exist: %s", resolved)
	}
	return resolved, nil
}

func loadOptions(flags Flags, requireGoal bool) (config.LoadOptions, error) {
	options := config.LoadOptions{RequireGoal: requireGoal}
	if value, ok := flags.String("config"); ok {
		options.ConfigFile = value
	}
	maxIterations, err := parseNumber(flags, "max-iterations")
	if err != nil {
		return options, err
	}
	if maxIterations != nil {
		value := int(*maxIterations)
		options.MaxIterations = &value
	}
	budget, err := parseNumber(flags, "budget")
	if err != nil {
		return options, err
	}
	options.BudgetUSD = budget
	maxMinutes, err := parseNumber(flags, "max-minutes")
	if err != nil {
		return options, err
	}
	options.MaxMinutes = maxMinutes
	overrides, err := parseModelOverrides(flags)
	if err != nil {
		return options, err
	}
	options.ModelOverrides = overrides
	return options, nil
}

func commandInit(flags Flags) (int, error) {
	dir := "."
	if len(flags.Args) > 0 && flags.Args[0] != "" {
		dir = flags.Args[0]
	}
	template, _ := flags.String("template")
	goal, _ := flags.String("goal")
	result, err := templates.InitWorkspace(context.Background(), templates.InitOptions{
		Dir: dir, Template: template, Goal: goal, Force: flags.Bool("force"),
	})
	if err != nil {
		return 1, err
	}
	util.Log("")
	util.Log("Created Forge workspace: " + result.Dir)
	util.Log("Goal: " + result.Goal)
	util.Log("Detected: " + result.Detected.Profile())
	if len(result.Detected.Manifests) > 0 {
		manifests := result.Detected.Manifests
		if len(manifests) > 8 {
			manifests = manifests[:8]
		}
		util.Log("Manifests: " + strings.Join(manifests, ", "))
	}
	var commandParts []string
	for _, name := range []string{"install", "test", "coverage", "lint", "typecheck", "build", "benchmark"} {
		if spec := result.Detected.Commands[name]; spec != nil {
			commandParts = append(commandParts, fmt.Sprintf("%s=`%s`", name, spec.Command))
		}
	}
	if len(commandParts) > 0 {
		util.Log("Commands: " + strings.Join(commandParts, "  "))
	} else {
		util.Log("Commands: none detected. Configure `commands` in forge.config.json or add a Makefile, justfile, Taskfile, or test/build script.")
	}
	util.Log("")
	util.Log("Model routing:")
	util.Log(models.FormatTable(models.Rows(config.Config{Models: result.Models, Pricing: map[string]config.Pricing{}})))
	util.Log("")
	relative, err := filepath.Rel(mustGetwd(), result.Dir)
	target := result.Dir
	if err == nil {
		if relative == "" {
			target = "."
		} else if !strings.HasPrefix(relative, "..") && !filepath.IsAbs(relative) {
			target = relative
		}
	}
	util.Log("Next steps:")
	util.Log("  cd " + target)
	util.Log("  forge doctor")
	util.Log("  forge run")
	util.Log("")
	util.Log("Tip: `go install ./cmd/forge` puts the `forge` binary on your PATH.")
	return 0, nil
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

func printPlan(workspace string, cfg config.Config, configFile string) {
	resolved := gates.ResolveCommands(cfg, workspace)
	util.Log("")
	util.Log("FORGE plan")
	util.Log("Goal:      " + cfg.Goal)
	util.Log("Workspace: " + workspace)
	label := configFile
	if label == "" {
		label = "(defaults)"
	}
	util.Log("Config:    " + label)
	util.Log("Project:   " + resolved.Detected.Profile())
	util.Log("")
	util.Log("Models:")
	util.Log(models.FormatTable(models.Rows(cfg)))
	util.Log("")
	util.Log("Commands:")
	for _, name := range []string{"install", "test", "coverage", "lint", "typecheck", "build", "benchmark"} {
		spec := resolved.Commands[name]
		if spec == nil {
			util.Log(fmt.Sprintf("  %-10s (missing)", name))
			continue
		}
		suffix := ""
		if resolved.Sources[name] != "config" {
			suffix = "  (" + resolved.Sources[name] + ")"
		}
		util.Log(fmt.Sprintf("  %-10s %s%s", name, spec.Command, suffix))
	}
	util.Log("")
	util.Log("Acceptance:")
	acceptance, _ := json.MarshalIndent(cfg.Acceptance, "", "  ")
	util.Log(string(acceptance))
	util.Log("")
	util.Log("Loop:")
	util.Log("  stages: architect -> designer -> implementer -> tester -> benchmarker -> security -> reviewer -> release")
	iterations := "unlimited"
	if cfg.Budget.MaxIterations > 0 {
		iterations = fmt.Sprintf("%d", cfg.Budget.MaxIterations)
	}
	minutes := "unlimited"
	if cfg.Budget.MaxMinutes > 0 {
		minutes = fmt.Sprintf("%g", cfg.Budget.MaxMinutes)
	}
	cost := "unlimited"
	if cfg.Budget.MaxUSD > 0 {
		cost = util.FormatUSD(cfg.Budget.MaxUSD)
	}
	util.Log(fmt.Sprintf("  max iterations: %s  |  max time: %s min  |  max cost: %s", iterations, minutes, cost))
	util.Log("")
	util.Log("Dry run: no model calls and no files written.")
}

func commandRun(flags Flags) (int, error) {
	if flags.Bool("help") {
		util.Log(Help)
		return 0, nil
	}
	workspace, err := resolveWorkspace(flags.Args)
	if err != nil {
		return 1, err
	}
	options, err := loadOptions(flags, true)
	if err != nil {
		return 1, err
	}
	loaded, err := config.Load(workspace, options)
	if err != nil {
		return 1, err
	}
	if flags.Bool("checkpoint") {
		loaded.Config.Git.Checkpoint = true
	}
	if flags.Bool("no-checkpoint") {
		loaded.Config.Git.Checkpoint = false
	}
	if flags.Bool("dry-run") {
		printPlan(workspace, loaded.Config, loaded.File)
		return 0, nil
	}
	result, err := orchestrator.RunHarness(context.Background(), orchestrator.Options{
		Cwd: workspace, Config: loaded.Config, ConfigFile: loaded.File,
		Verbose: flags.Bool("verbose"),
	})
	if err != nil {
		return 1, err
	}
	if result.Success {
		return 0, nil
	}
	return 1, nil
}

func commandGates(flags Flags) (int, error) {
	workspace, err := resolveWorkspace(flags.Args)
	if err != nil {
		return 1, err
	}
	options, err := loadOptions(flags, false)
	if err != nil {
		return 1, err
	}
	loaded, err := config.Load(workspace, options)
	if err != nil {
		return 1, err
	}
	runDir := filepath.Join(reports.ForgeDir(workspace), "gates", util.RunID(time.Now()))
	_ = util.EnsureDir(filepath.Join(runDir, "commands"))
	_ = util.EnsureDir(filepath.Join(runDir, "security"))
	util.Log("Running machine gates in " + workspace)
	det := gates.EvaluateDeterministic(context.Background(), loaded.Config, workspace, runDir, 0, nil)
	util.Log("")
	for _, command := range det.Commands {
		if command.Status == "skip" {
			continue
		}
		line := fmt.Sprintf("%-10s %s", command.Name, strings.ToUpper(command.Status))
		if command.Message != "" {
			line += "  " + command.Message
		}
		util.Log(line)
	}
	if !det.Coverage.Skipped {
		status := "FAIL"
		if det.Coverage.OK {
			status = "PASS"
		}
		util.Log("coverage   " + status + "  " + det.Coverage.Message)
	}
	if !det.Benchmark.Skipped {
		status := "FAIL"
		if det.Benchmark.OK {
			status = "PASS"
		}
		util.Log("benchmark  " + status + "  " + benchmarkMetrics(det.Benchmark.Metrics))
		for _, failure := range det.Benchmark.Failures {
			util.Log("  " + failure.Message)
		}
	}
	if !det.Security.Skipped {
		secretStatus := "FAIL"
		if det.Security.Secrets.OK {
			secretStatus = "PASS"
		}
		scannerStatus := "FAIL"
		if det.Security.Scanners.OK {
			scannerStatus = "PASS"
		}
		util.Log(fmt.Sprintf("secrets    %s  %d finding(s)", secretStatus, len(det.Security.Secrets.Findings)))
		util.Log(fmt.Sprintf("scanners   %s  %d finding(s)", scannerStatus, len(det.Security.Scanners.Findings)))
		findings := append(append([]security.Finding{}, det.Security.Secrets.Findings...), det.Security.Scanners.Findings...)
		for index, finding := range findings {
			if index >= 5 {
				break
			}
			location := "unknown location"
			if finding.File != "" {
				location = finding.File
				if finding.Line > 0 {
					location += fmt.Sprintf(":%d", finding.Line)
				}
			}
			util.Log(fmt.Sprintf("  [%s] %s — %s", finding.Severity, finding.Title, location))
		}
		for _, warning := range det.Security.Scanners.Warnings {
			util.Warn(warning)
		}
	}
	util.Log("")
	if det.OK {
		util.Log("All machine gates passed.")
	} else {
		util.Log("Machine gates failed.")
	}
	util.Log("Logs: " + runDir)
	if det.OK {
		return 0, nil
	}
	return 1, nil
}

func benchmarkMetrics(metrics map[string]float64) string {
	var parts []string
	for key, value := range metrics {
		parts = append(parts, fmt.Sprintf("%s=%g", key, value))
	}
	return strings.Join(parts, ", ")
}

func commandStatus(flags Flags) (int, error) {
	workspace, err := resolveWorkspace(flags.Args)
	if err != nil {
		return 1, err
	}
	state := reports.ReadState(workspace)
	if flags.Bool("json") {
		data, _ := json.MarshalIndent(state, "", "  ")
		util.Log(string(data))
		return 0, nil
	}
	util.Log(reports.FormatLastRun(state))
	if len(state.Runs) > 1 {
		util.Log("")
		util.Log("Recent runs:")
		for index, run := range state.Runs {
			if index >= 5 {
				break
			}
			goal := run.Goal
			if len(goal) > 60 {
				goal = goal[:60]
			}
			util.Log(fmt.Sprintf("  %s  %-7s  %d iter  %s  %s", run.ID, run.Status, run.Iterations, util.FormatUSD(run.CostUSD), goal))
		}
	}
	if flags.Bool("report") && len(state.Runs) > 0 && state.Runs[0].Report != "" {
		reportFile := filepath.Join(workspace, state.Runs[0].Report)
		if util.FileExists(reportFile) {
			util.Log("")
			util.Log(util.ReadText(reportFile))
		}
	}
	return 0, nil
}

func commandModels(flags Flags) (int, error) {
	workspace, err := resolveWorkspace(flags.Args)
	if err != nil {
		return 1, err
	}
	options, err := loadOptions(flags, false)
	if err != nil {
		return 1, err
	}
	loaded, err := config.Load(workspace, options)
	if err != nil {
		return 1, err
	}
	rows := models.Rows(loaded.Config)
	available := models.ListAvailable(context.Background())
	if flags.Bool("json") {
		data, _ := json.MarshalIndent(map[string]any{"models": rows, "available": available}, "", "  ")
		util.Log(string(data))
		return 0, nil
	}
	util.Log(models.FormatTable(rows))
	util.Log("")
	util.Log("Prices are USD per 1M tokens from `pricing` in forge.config.json; 0 means free or unpriced.")
	util.Log("Tiers: reasoning = large/high-capability, balanced = everyday coding, fast = cheap/fast.")
	if available != nil {
		for _, row := range rows {
			resolved := models.Resolve(loaded.Config, row.Stage, available, "")
			if len(resolved.Unavailable) > 0 {
				util.Warn(fmt.Sprintf("%s: unavailable %s; fallback %s", row.Stage, strings.Join(resolved.Unavailable, ", "), resolved.Selected))
			}
		}
	}
	return 0, nil
}

func commandDoctor(flags Flags) (int, error) {
	workspace, err := resolveWorkspace(flags.Args)
	if err != nil {
		return 1, err
	}
	var issues, warnings []string
	info := map[string]any{"workspace": workspace, "go": runtime.Version()}

	opencode := util.RunCommand(context.Background(), "opencode --version", "", 20*time.Second)
	if opencode.OK {
		info["opencode"] = strings.TrimSpace(opencode.Stdout)
	} else {
		info["opencode"] = nil
		issues = append(issues, "`opencode` is not available on PATH.")
	}

	options, err := loadOptions(flags, false)
	if err != nil {
		return 1, err
	}
	loaded, loadErr := config.Load(workspace, options)
	if loadErr != nil {
		issues = append(issues, loadErr.Error())
	} else {
		info["configFile"] = loaded.File
		if loaded.File == "" {
			warnings = append(warnings, "No forge.config.json found; defaults will be used.")
		}
		if loaded.Config.Goal == "" {
			issues = append(issues, "No goal is configured.")
		}
		available := models.ListAvailable(context.Background())
		info["availableModels"] = len(available)
		if available == nil {
			warnings = append(warnings, "Could not list OpenCode models.")
		} else {
			for stage, entry := range loaded.Config.Models {
				if !models.IsAvailable(entry.Model, available) {
					resolved := models.Resolve(loaded.Config, stage, available, "")
					warnings = append(warnings, fmt.Sprintf("%s: %s is unavailable; fallback is %s.", stage, entry.Model, resolved.Selected))
				}
			}
		}
		resolved := gates.ResolveCommands(loaded.Config, workspace)
		info["commands"] = gates.CommandsToJSON(resolved)
		info["detected"] = resolved.Detected
		if resolved.Detected.Language == "unknown" && len(resolved.Detected.Commands) == 0 {
			warnings = append(warnings, "No project manifest or task runner detected. Configure commands explicitly for this project.")
		}
		if loaded.Config.Acceptance.Tests && resolved.Commands["test"] == nil {
			issues = append(issues, "Tests are required but no test command is configured or detectable.")
		}
		if loaded.Config.Acceptance.Coverage.Min > 0 && resolved.Commands["coverage"] == nil {
			issues = append(issues, "Coverage is required but no coverage command is configured or detectable.")
		}
		if loaded.Config.Acceptance.Lint && resolved.Commands["lint"] == nil {
			warnings = append(warnings, "Lint is enabled but no lint command is configured or detectable.")
		}
		if loaded.Config.Acceptance.Build && resolved.Commands["build"] == nil {
			warnings = append(warnings, "Build is enabled but no build command is configured or detectable.")
		}
		if loaded.Config.Acceptance.Benchmarks.Required && resolved.Commands["benchmark"] == nil {
			issues = append(issues, "Benchmarks are required but no benchmark command is configured or detectable.")
		}
		if loaded.Config.Acceptance.Security.Enabled {
			scanners := security.AutoScanners(workspace)
			names := []string{}
			for _, scanner := range scanners {
				names = append(names, scanner.Name)
			}
			info["securityScanners"] = names
			if len(scanners) == 0 {
				warnings = append(warnings, "No external security scanner detected; the built-in secret scan will still run.")
			}
		}
		modelMap := map[string]string{}
		for stage, entry := range loaded.Config.Models {
			modelMap[stage] = entry.Model
		}
		info["models"] = modelMap
	}

	isRepo := gitpkg.IsRepo(workspace)
	info["git"] = isRepo
	if !isRepo {
		warnings = append(warnings, "Workspace is not a git repository; enable git for checkpoints and diffs.")
	}

	if flags.Bool("json") {
		data, _ := json.MarshalIndent(map[string]any{"info": info, "issues": issues, "warnings": warnings}, "", "  ")
		util.Log(string(data))
	} else {
		util.Log("Forge doctor")
		util.Log("  workspace: " + workspace)
		util.Log("  go:        " + info["go"].(string))
		opencodeLabel := "(missing)"
		if value, ok := info["opencode"].(string); ok {
			opencodeLabel = value
		}
		util.Log("  opencode:  " + opencodeLabel)
		configLabel := "(defaults)"
		if value, ok := info["configFile"].(string); ok && value != "" {
			configLabel = value
		}
		util.Log("  config:    " + configLabel)
		goalLabel := "(missing)"
		if loaded != nil && loaded.Config.Goal != "" {
			goalLabel = "set"
		}
		util.Log("  goal:      " + goalLabel)
		util.Log(fmt.Sprintf("  models:    %v available", info["availableModels"]))
		gitLabel := "no"
		if isRepo {
			gitLabel = "yes"
		}
		util.Log("  git:       " + gitLabel)
		util.Log("")
		for _, warning := range warnings {
			util.Warn(warning)
		}
		for _, issue := range issues {
			util.Error(issue)
		}
		util.Log("")
		if len(issues) == 0 {
			util.Log("Doctor found no blocking issues.")
		} else {
			util.Log(fmt.Sprintf("Doctor found %d blocking issue(s).", len(issues)))
		}
	}
	if len(issues) == 0 {
		return 0, nil
	}
	return 1, nil
}

func dispatch(args []string) (int, error) {
	command := ""
	if len(args) > 0 {
		command = args[0]
	}
	rest := []string{}
	if len(args) > 1 {
		rest = args[1:]
	}
	flags := parseFlags(rest)
	switch command {
	case "", "help", "--help", "-h":
		util.Log(Help)
		return 0, nil
	case "--version", "-v", "version":
		util.Log("forge " + Version)
		return 0, nil
	case "init":
		return commandInit(flags)
	case "run":
		return commandRun(flags)
	case "gates":
		return commandGates(flags)
	case "status":
		return commandStatus(flags)
	case "doctor":
		return commandDoctor(flags)
	case "models":
		return commandModels(flags)
	default:
		util.Error("Unknown command: " + command)
		util.Log("")
		util.Log(Help)
		return 1, nil
	}
}

// Main runs the CLI and returns a process exit code.
func Main(args []string) int {
	code, err := dispatch(args)
	if err != nil {
		var configErr *config.ConfigError
		if errors.As(err, &configErr) {
			util.Error(configErr.Message)
			return 1
		}
		util.Error(err.Error())
		return 1
	}
	return code
}

// Help is the CLI help text.
const Help = `Forge — a personal, multi-model coding harness

Usage:
  forge <command> [workspace] [flags]

Commands:
  init [dir]        Create a workspace, goal, config, and schema
  run [dir]         Run the goal-driven loop until gates + review pass
  gates [dir]       Run only the machine gates (tests, build, bench, security)
  status [dir]      Show the latest run and recent history
  doctor [dir]      Check OpenCode, models, commands, config, and git
  models [dir]      Show model routing and prices
  version           Show the Forge version
  help              Show this help

Init flags:
  --goal <text>             Goal statement (required for a new workspace)
  --template <name>         none (default and only built-in; accepted for compatibility)
  --force                   Overwrite generated control files and an existing config

Run flags:
  --max-iterations <n>      0 = unlimited (default from config)
  --budget <usd>            Cost budget; 0 = unlimited
  --max-minutes <n>         Wall-clock budget; 0 = unlimited
  --model <stage=model>     Override a stage model (repeatable)
  --config <file>           Use a specific config file
  --checkpoint              Commit after each iteration (requires git)
  --dry-run                 Print the plan without calling models
  --verbose                 More console detail

Examples:
  cd my-project
  forge init . --goal "Add structured logging without changing the public API"
  forge doctor
  forge run
  forge run --max-iterations 4 --model implementer=opencode/glm-5.3-flash-free
  forge gates
  forge models
`
