package workspace

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/soub4i/forge/internal/config"
	"github.com/soub4i/forge/internal/util"
)

func TestInitWorkspaceDefaultsToControlFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "empty")
	result, err := InitWorkspace(context.Background(), InitOptions{
		Dir: dir, Goal: "Do something", Available: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Goal != "Do something" {
		t.Fatalf("goal = %q", result.Goal)
	}
	required := []string{
		".gitignore", "AGENTS.md", ".forge/forge.schema.json", "docs/GOAL.md",
	}
	for _, rel := range required {
		if !util.FileExists(filepath.Join(dir, rel)) {
			t.Errorf("missing generated file: %s", rel)
		}
	}
	for _, rel := range []string{"package.json", "src", "test", "bench"} {
		if util.FileExists(filepath.Join(dir, rel)) {
			t.Errorf("init generated project scaffolding: %s", rel)
		}
	}

	var cfg config.Config
	if err := util.ReadJSON(result.ConfigFile, &cfg); err != nil {
		t.Fatal(err)
	}
	config.Normalize(&cfg)
	if cfg.Goal != "Do something" {
		t.Fatalf("config goal = %q", cfg.Goal)
	}
	if cfg.Commands.Test != nil {
		t.Fatalf("test command = %+v", cfg.Commands.Test)
	}
	if !cfg.Acceptance.Tests {
		t.Fatal("tests should remain required by default")
	}
	if cfg.Acceptance.Coverage.Min != 0 || cfg.Acceptance.Benchmarks.Required {
		t.Fatalf("acceptance = %+v", cfg.Acceptance)
	}
	if len(cfg.Models) != len(config.DefaultModels) {
		t.Fatalf("models = %d", len(cfg.Models))
	}
	if cfg.Schema != "./.forge/forge.schema.json" {
		t.Fatalf("schema = %q", cfg.Schema)
	}
	gitignore := util.ReadText(filepath.Join(dir, ".gitignore"))
	for _, want := range []string{".forge/", ".venv/", "target/", "__pycache__/", "vendor/"} {
		if !strings.Contains(gitignore, want) {
			t.Errorf(".gitignore is missing %q", want)
		}
	}
	instructions := util.ReadText(filepath.Join(dir, "AGENTS.md"))
	for _, want := range []string{"Project profile", "not detected", "project's standard build/test entry points"} {
		if !strings.Contains(instructions, want) {
			t.Errorf("AGENTS.md is missing %q:\n%s", want, instructions)
		}
	}
}

func TestInitWorkspaceWritesDetectedProfile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "existing-go")
	if err := util.EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteText(filepath.Join(dir, "go.mod"), "module demo\n\ngo 1.23\n"); err != nil {
		t.Fatal(err)
	}
	result, err := InitWorkspace(context.Background(), InitOptions{
		Dir: dir, Goal: "Improve the Go service", Available: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Detected.Language != "go" || result.Detected.Commands["test"].Command != "go test ./..." {
		t.Fatalf("detected = %+v", result.Detected)
	}
	if result.Detected.Profile() != "go" {
		t.Fatalf("profile = %q", result.Detected.Profile())
	}
	var cfg config.Config
	if err := util.ReadJSON(result.ConfigFile, &cfg); err != nil {
		t.Fatal(err)
	}
	config.Normalize(&cfg)
	if cfg.Commands.Test == nil || cfg.Commands.Test.Command != "go test ./..." {
		t.Fatalf("test command = %+v", cfg.Commands.Test)
	}
	if cfg.Acceptance.Coverage.Min != 70 || !cfg.Acceptance.Lint || !cfg.Acceptance.Build {
		t.Fatalf("acceptance = %+v", cfg.Acceptance)
	}
	instructions := util.ReadText(filepath.Join(dir, "AGENTS.md"))
	for _, want := range []string{"## Project profile", "go", "- test: `go test ./...`", "repository's existing language"} {
		if !strings.Contains(instructions, want) {
			t.Errorf("AGENTS.md is missing %q:\n%s", want, instructions)
		}
	}
	if strings.Contains(instructions, "node") {
		t.Fatalf("AGENTS.md inherited Node.js instructions:\n%s", instructions)
	}
}

func TestInitWorkspaceRequiresGoal(t *testing.T) {
	_, err := InitWorkspace(context.Background(), InitOptions{
		Dir: filepath.Join(t.TempDir(), "no-goal"), Available: []string{},
	})
	if err == nil {
		t.Fatal("expected a missing-goal error")
	}
	if !strings.Contains(err.Error(), "--goal") {
		t.Fatalf("error = %v", err)
	}
}

func TestInitWorkspaceKeepsExistingGitignoreAndAgents(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "existing")
	if err := util.EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteText(filepath.Join(dir, ".gitignore"), "custom-output/\n"); err != nil {
		t.Fatal(err)
	}
	if err := util.WriteText(filepath.Join(dir, "AGENTS.md"), "# My agents\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := InitWorkspace(context.Background(), InitOptions{Dir: dir, Goal: "one", Available: []string{}}); err != nil {
		t.Fatal(err)
	}
	gitignore := util.ReadText(filepath.Join(dir, ".gitignore"))
	if !strings.Contains(gitignore, "custom-output/") || !strings.Contains(gitignore, ".forge/") {
		t.Fatalf(".gitignore = %q", gitignore)
	}
	if instructions := util.ReadText(filepath.Join(dir, "AGENTS.md")); instructions != "# My agents\n" {
		t.Fatalf("existing AGENTS.md was overwritten: %q", instructions)
	}
}

func TestInitWorkspaceRequiresForceForExistingConfig(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "existing")
	if _, err := InitWorkspace(context.Background(), InitOptions{Dir: dir, Goal: "one", Available: []string{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := InitWorkspace(context.Background(), InitOptions{Dir: dir, Goal: "two", Available: []string{}}); err == nil {
		t.Fatal("expected an existing-config error")
	}
	if _, err := InitWorkspace(context.Background(), InitOptions{Dir: dir, Goal: "two", Force: true, Available: []string{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := InitWorkspace(context.Background(), InitOptions{Dir: dir, Force: true, Available: []string{}}); err != nil {
		t.Fatal(err)
	}
	var cfg config.Config
	if err := util.ReadJSON(filepath.Join(dir, "forge.config.json"), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Goal != "two" {
		t.Fatalf("force without --goal should preserve the goal, got %q", cfg.Goal)
	}
}
