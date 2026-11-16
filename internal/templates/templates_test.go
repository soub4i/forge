package templates

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/soub4i/forge/internal/config"
	"github.com/soub4i/forge/internal/util"
)

func TestInitWorkspaceCLI(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "linecounter")
	result, err := InitWorkspace(context.Background(), InitOptions{
		Dir: dir, Template: "cli", Goal: "Build a fast line counter", Available: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Goal != "Build a fast line counter" {
		t.Fatalf("goal = %q", result.Goal)
	}
	required := []string{
		".gitignore", "package.json", "README.md", "AGENTS.md", "src/cli.js",
		"test/cli.test.js", "bench/run.js", ".forge/forge.schema.json", "docs/GOAL.md",
	}
	for _, rel := range required {
		if !util.FileExists(filepath.Join(dir, rel)) {
			t.Errorf("missing generated file: %s", rel)
		}
	}
	hasGitignore := false
	for _, rel := range result.Copied {
		if rel == ".gitignore" {
			hasGitignore = true
		}
	}
	if !hasGitignore {
		t.Error(".gitignore was not copied from the embedded template")
	}

	var cfg config.Config
	if err := util.ReadJSON(result.ConfigFile, &cfg); err != nil {
		t.Fatal(err)
	}
	config.Normalize(&cfg)
	if cfg.Goal != "Build a fast line counter" {
		t.Fatalf("config goal = %q", cfg.Goal)
	}
	if cfg.Commands.Test == nil || cfg.Commands.Test.Command == "" {
		t.Fatal("test command was not persisted")
	}
	if cfg.Acceptance.Coverage.Min != 70 {
		t.Fatalf("coverage minimum = %v", cfg.Acceptance.Coverage.Min)
	}
	if !cfg.Acceptance.Benchmarks.Required {
		t.Fatal("cli template benchmarks should be required")
	}
	if len(cfg.Models) != len(config.DefaultModels) {
		t.Fatalf("models = %d", len(cfg.Models))
	}
	if cfg.Schema != "./.forge/forge.schema.json" {
		t.Fatalf("schema = %q", cfg.Schema)
	}
	instructions := util.ReadText(filepath.Join(dir, "AGENTS.md"))
	if !strings.Contains(instructions, "Node.js >= 20") {
		t.Errorf("starter AGENTS.md lost its JavaScript-specific guidance:\n%s", instructions)
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
		Dir: dir, Template: "none", Goal: "Improve the Go service", Available: []string{},
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

func TestInitWorkspaceLogAggregator(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logpipe")
	result, err := InitWorkspace(context.Background(), InitOptions{
		Dir: dir, Template: "log-aggregator", Available: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Goal, "log aggregator") {
		t.Fatalf("goal = %q", result.Goal)
	}
	if !util.FileExists(filepath.Join(dir, "src", "aggregator.js")) {
		t.Fatal("log aggregator template was not copied")
	}
}

func TestInitWorkspaceNone(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "empty")
	result, err := InitWorkspace(context.Background(), InitOptions{
		Dir: dir, Template: "none", Goal: "Do something", Available: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Copied) != 0 {
		t.Fatalf("copied = %v", result.Copied)
	}
	if !util.FileExists(filepath.Join(dir, "forge.config.json")) {
		t.Fatal("config was not written")
	}
	gitignore := util.ReadText(filepath.Join(dir, ".gitignore"))
	for _, want := range []string{".venv/", "target/", "__pycache__/", "vendor/"} {
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

func TestInitWorkspaceRejectsUnknownTemplate(t *testing.T) {
	_, err := InitWorkspace(context.Background(), InitOptions{
		Dir: filepath.Join(t.TempDir(), "x"), Template: "nope", Available: []string{},
	})
	if err == nil {
		t.Fatal("expected an unknown template error")
	}
}

func TestInitWorkspaceRequiresForceForExistingConfig(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "existing")
	if _, err := InitWorkspace(context.Background(), InitOptions{Dir: dir, Template: "none", Goal: "one", Available: []string{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := InitWorkspace(context.Background(), InitOptions{Dir: dir, Template: "none", Goal: "two", Available: []string{}}); err == nil {
		t.Fatal("expected an existing-config error")
	}
	if _, err := InitWorkspace(context.Background(), InitOptions{Dir: dir, Template: "none", Goal: "two", Force: true, Available: []string{}}); err != nil {
		t.Fatal(err)
	}
}

func TestPackageNameFor(t *testing.T) {
	cases := map[string]string{
		"/tmp/My Tool!":        "my-tool",
		"/tmp/---":             "tool",
		"/tmp/line_counter.v2": "line_counter.v2",
	}
	for input, want := range cases {
		if got := PackageNameFor(input, "tool"); got != want {
			t.Errorf("PackageNameFor(%q) = %q, want %q", input, got, want)
		}
	}
}
