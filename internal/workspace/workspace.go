package workspace

import (
	"context"
	"embed"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/soub4i/forge/internal/config"
	"github.com/soub4i/forge/internal/gates"
	"github.com/soub4i/forge/internal/models"
	"github.com/soub4i/forge/internal/reports"
	"github.com/soub4i/forge/internal/util"
)

//go:embed assets
var assets embed.FS

func ensureGitignore(dest string) bool {
	file := filepath.Join(dest, ".gitignore")
	if !util.FileExists(file) {
		_ = util.WriteText(file, ".forge/\nnode_modules/\ncoverage/\nbench/results.json\ndist/\nbuild/\ntarget/\nvendor/\n.venv/\n__pycache__/\n*.pyc\n*.log\n")
		return true
	}
	current := util.ReadText(file)
	for _, line := range strings.Split(current, "\n") {
		if strings.TrimSpace(line) == ".forge/" {
			return false
		}
	}
	if current != "" && !strings.HasSuffix(current, "\n") {
		current += "\n"
	}
	_ = util.WriteText(file, current+".forge/\n")
	return true
}

func copySchema(dest string) (string, error) {
	data, err := assets.ReadFile("assets/forge.schema.json")
	if err != nil {
		return "", err
	}
	target := filepath.Join(dest, ".forge", "forge.schema.json")
	if err := util.WriteText(target, string(data)); err != nil {
		return "", err
	}
	return target, nil
}

func writeAgentInstructions(dest string, cfg config.Config, detected gates.Detected) bool {
	file := filepath.Join(dest, "AGENTS.md")
	if util.FileExists(file) {
		return false
	}
	var commands []string
	for _, name := range []string{"install", "test", "coverage", "lint", "typecheck", "build", "benchmark"} {
		if spec := cfg.Command(name); spec != nil && spec.Command != "" {
			commands = append(commands, fmt.Sprintf("- %s: `%s`", name, spec.Command))
		}
	}
	commandText := strings.Join(commands, "\n")
	if commandText == "" {
		commandText = "- No commands detected. Add the project's standard build/test entry points or configure them in `forge.config.json`."
	}
	profile := detected.Profile()
	if profile == "unknown" {
		profile = "not detected"
	}
	content := fmt.Sprintf(`# Agent instructions

This repository is managed by Forge, an autonomous goal-driven harness.

## Goal

%s

## Project profile

%s

## Commands

%s

## Rules

- Use the repository's existing language, build system, package manager, and conventions.
- Prefer the project's native toolchain and a minimal dependency set.
- Every behavior change needs a test at the appropriate level: unit, integration, or end-to-end.
- Tests and benchmarks must be deterministic and offline.
- Do not weaken tests, benchmarks, or acceptance criteria to make a gate pass.
- Never commit secrets or machine-specific paths.
`, cfg.Goal, profile, commandText)
	_ = util.WriteText(file, content)
	return true
}

// InitOptions configures workspace creation.
type InitOptions struct {
	Dir       string
	Goal      string
	Force     bool
	Available []string
}

// InitResult describes a created workspace.
type InitResult struct {
	Dir             string
	Goal            string
	ConfigFile      string
	Detected        gates.Detected
	Models          map[string]config.ModelEntry
	Acceptance      config.Acceptance
	AvailableModels []string
}

func commandPtr(value string) *config.Command {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &config.Command{Command: value}
}

// InitWorkspace writes Forge control files into an existing or empty project.
func InitWorkspace(ctx context.Context, options InitOptions) (*InitResult, error) {
	dest, err := filepath.Abs(options.Dir)
	if err != nil {
		return nil, err
	}
	if err := util.EnsureDir(dest); err != nil {
		return nil, err
	}
	configFile := filepath.Join(dest, "forge.config.json")
	if util.FileExists(configFile) && !options.Force {
		return nil, fmt.Errorf("a Forge config already exists at %s. Use --force to overwrite it", configFile)
	}
	goal := strings.TrimSpace(options.Goal)
	if goal == "" && util.FileExists(configFile) {
		var existing config.Config
		if util.ReadJSON(configFile, &existing) == nil {
			goal = strings.TrimSpace(existing.Goal)
		}
	}
	if goal == "" {
		return nil, fmt.Errorf("forge init requires --goal <text>")
	}
	detected := gates.DetectProject(dest)
	commandStrings := gates.DetectedCommandsToJSON(detected)
	available := options.Available
	if available == nil {
		available = models.ListAvailable(ctx)
	}
	picked := models.PickInitial(config.Default(), available)
	hasCoverage := commandStrings["coverage"] != ""

	acceptance := config.Acceptance{
		Tests:     true,
		Coverage:  config.Coverage{Min: 0},
		Lint:      commandStrings["lint"] != "",
		Typecheck: commandStrings["typecheck"] != "",
		Build:     commandStrings["build"] != "",
		Benchmarks: config.Benchmarks{
			Required:    false,
			ResultsFile: "bench/results.json",
			Thresholds:  nil,
		},
		Security: config.Security{
			Enabled: true, MaxSeverity: "medium", RequireNoSecret: true,
			Scanners: []config.Scanner{}, Ignore: []string{},
		},
		Review: config.Review{Enabled: true, MaxBlockingFindings: 0},
	}
	if hasCoverage {
		acceptance.Coverage.Min = 70
	}

	cfg := config.Default()
	cfg.Goal = goal
	cfg.Acceptance = acceptance
	cfg.Commands = config.Commands{
		Install:   commandPtr(commandStrings["install"]),
		Test:      commandPtr(commandStrings["test"]),
		Coverage:  commandPtr(commandStrings["coverage"]),
		Lint:      commandPtr(commandStrings["lint"]),
		Typecheck: commandPtr(commandStrings["typecheck"]),
		Build:     commandPtr(commandStrings["build"]),
		Benchmark: commandPtr(commandStrings["benchmark"]),
	}
	cfg.Models = picked
	config.Normalize(&cfg)
	if err := config.WriteFile(configFile, cfg); err != nil {
		return nil, err
	}
	if _, err := reports.EnsureGoalDoc(dest, cfg); err != nil {
		return nil, err
	}
	ensureGitignore(dest)
	writeAgentInstructions(dest, cfg, detected)
	if _, err := copySchema(dest); err != nil {
		return nil, err
	}
	return &InitResult{
		Dir: dest, Goal: goal, ConfigFile: configFile, Detected: detected,
		Models: picked, Acceptance: acceptance, AvailableModels: available,
	}, nil
}
