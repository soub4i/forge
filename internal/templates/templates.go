package templates

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/soub4i/forge/internal/config"
	"github.com/soub4i/forge/internal/gates"
	"github.com/soub4i/forge/internal/models"
	"github.com/soub4i/forge/internal/reports"
	"github.com/soub4i/forge/internal/util"
)

//go:embed all:assets
var assets embed.FS

// Templates lists the available starter templates.
var Templates = []string{"cli", "log-aggregator", "none"}

// PackageNameFor derives a safe package name from a directory.
func PackageNameFor(dir, fallback string) string {
	base := strings.ToLower(filepath.Base(dir))
	var b strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	cleaned := strings.Trim(b.String(), "-._")
	if cleaned == "" {
		return fallback
	}
	return cleaned
}

// DefaultGoal returns a template-specific goal statement.
func DefaultGoal(template, name string) string {
	switch template {
	case "cli":
		return fmt.Sprintf("Build %s, a fast, dependency-free developer CLI that counts lines, words, and UTF-8 bytes from stdin or files, supports JSON output, and is covered by unit tests and a throughput benchmark.", name)
	case "log-aggregator":
		return fmt.Sprintf("Build %s, a zero-dependency log aggregator CLI that streams NDJSON logs from stdin or files, filters by level and time window, aggregates counts and latency percentiles, handles malformed lines safely, and is covered by unit tests and a throughput benchmark.", name)
	}
	return "Describe the developer tool or service you want Forge to build."
}

func floatPtr(value float64) *float64 { return &value }

// DefaultThresholds returns the template's benchmark thresholds.
func DefaultThresholds(template string) []config.Threshold {
	switch template {
	case "cli":
		return []config.Threshold{{Metric: "lines_per_sec", Min: floatPtr(100000), Unit: "lines/sec"}}
	case "log-aggregator":
		return []config.Threshold{{Metric: "events_per_sec", Min: floatPtr(50000), Unit: "events/sec"}}
	}
	return nil
}

func renderTemplate(text, name, goal, date, version string) string {
	replacer := strings.NewReplacer(
		"{{name}}", name,
		"{{goal}}", goal,
		"{{date}}", date,
		"{{version}}", version,
	)
	return replacer.Replace(text)
}

type templateFile struct {
	full string
	rel  string
}

func walkTemplate(root string) ([]templateFile, error) {
	var files []templateFile
	err := fs.WalkDir(assets, root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "node_modules" {
				return fs.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		files = append(files, templateFile{full: path, rel: filepath.ToSlash(rel)})
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })
	return files, err
}

type copyResult struct {
	Copied  []string
	Skipped []string
}

func copyTemplate(name, dest string, force bool, context struct{ name, goal, date, version string }) (copyResult, error) {
	result := copyResult{Copied: []string{}, Skipped: []string{}}
	if name == "none" {
		return result, nil
	}
	root := "assets/" + name
	if _, err := fs.Stat(assets, root); err != nil {
		return result, fmt.Errorf("unknown template: %s", name)
	}
	files, err := walkTemplate(root)
	if err != nil {
		return result, err
	}
	for _, file := range files {
		target := filepath.Join(dest, filepath.FromSlash(file.rel))
		if util.FileExists(target) && !force {
			result.Skipped = append(result.Skipped, file.rel)
			continue
		}
		data, err := assets.ReadFile(file.full)
		if err != nil {
			return result, err
		}
		content := renderTemplate(string(data), context.name, context.goal, context.date, context.version)
		if err := util.WriteText(target, content); err != nil {
			return result, err
		}
		if strings.HasSuffix(file.rel, "cli.js") || strings.HasPrefix(file.rel, "bin/") {
			_ = os.Chmod(target, 0o755)
		}
		result.Copied = append(result.Copied, file.rel)
	}
	return result, nil
}

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
	Template  string
	Goal      string
	Force     bool
	Available []string
}

// InitResult describes a created workspace.
type InitResult struct {
	Dir             string
	Template        string
	Goal            string
	ConfigFile      string
	Copied          []string
	Skipped         []string
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

// InitWorkspace creates a starter project, config, goal doc, and schema.
func InitWorkspace(ctx context.Context, options InitOptions) (*InitResult, error) {
	template := options.Template
	if template == "" {
		template = "cli"
	}
	valid := false
	for _, name := range Templates {
		if name == template {
			valid = true
			break
		}
	}
	if !valid {
		return nil, fmt.Errorf("unknown template %q. Choose one of: %s", template, strings.Join(Templates, ", "))
	}
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
	name := PackageNameFor(dest, "tool")
	goal := options.Goal
	if strings.TrimSpace(goal) == "" {
		goal = DefaultGoal(template, name)
	}
	context := struct{ name, goal, date, version string }{
		name: name, goal: goal, date: util.NowISO()[:10], version: "0.1.0",
	}
	copied, err := copyTemplate(template, dest, options.Force, context)
	if err != nil {
		return nil, err
	}
	detected := gates.DetectProject(dest)
	commandStrings := gates.DetectedCommandsToJSON(detected)
	available := options.Available
	if available == nil {
		available = models.ListAvailable(ctx)
	}
	picked := models.PickInitial(config.Default(), available)
	thresholds := DefaultThresholds(template)
	hasCoverage := commandStrings["coverage"] != ""

	acceptance := config.Acceptance{
		Tests:     true,
		Coverage:  config.Coverage{Min: 0},
		Lint:      commandStrings["lint"] != "",
		Typecheck: commandStrings["typecheck"] != "",
		Build:     commandStrings["build"] != "",
		Benchmarks: config.Benchmarks{
			Required:    len(thresholds) > 0,
			ResultsFile: "bench/results.json",
			Thresholds:  thresholds,
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
		Dir: dest, Template: template, Goal: goal, ConfigFile: configFile,
		Copied: copied.Copied, Skipped: copied.Skipped, Detected: detected,
		Models: picked, Acceptance: acceptance, AvailableModels: available,
	}, nil
}
