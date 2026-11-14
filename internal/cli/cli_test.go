package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/soub4i/forge/internal/config"
	"github.com/soub4i/forge/internal/util"
)

func TestParseFlags(t *testing.T) {
	flags := parseFlags([]string{"--goal", "build a thing", "--force", "-v", "workspace", "--model", "a=b/c", "--model=d/e"})
	if len(flags.Args) != 1 || flags.Args[0] != "workspace" {
		t.Fatalf("args = %v", flags.Args)
	}
	if goal, ok := flags.String("goal"); !ok || goal != "build a thing" {
		t.Fatalf("goal = %q, %v", goal, ok)
	}
	if !flags.Bool("force") || !flags.Bool("v") {
		t.Fatal("boolean flags were not parsed")
	}
	models := flags.List("model")
	if len(models) != 2 || models[0] != "a=b/c" || models[1] != "d/e" {
		t.Fatalf("models = %v", models)
	}
}

func TestParseFlagsDoubleDash(t *testing.T) {
	flags := parseFlags([]string{"--", "--not-a-flag", "value"})
	if len(flags.Args) != 2 || flags.Args[0] != "--not-a-flag" {
		t.Fatalf("args = %v", flags.Args)
	}
}

func TestParseNumber(t *testing.T) {
	flags := parseFlags([]string{"--max-iterations", "4"})
	value, err := parseNumber(flags, "max-iterations")
	if err != nil || value == nil || *value != 4 {
		t.Fatalf("value = %v, err = %v", value, err)
	}
	bad := parseFlags([]string{"--budget=-5"})
	if _, err := parseNumber(bad, "budget"); err == nil {
		t.Fatal("negative numbers must be rejected")
	}
}

func TestParseModelOverrides(t *testing.T) {
	flags := parseFlags([]string{"--model", "implementer=opencode/glm-5.3-flash-free"})
	overrides, err := parseModelOverrides(flags)
	if err != nil {
		t.Fatal(err)
	}
	if overrides["implementer"] != "opencode/glm-5.3-flash-free" {
		t.Fatalf("overrides = %v", overrides)
	}
	if _, err := parseModelOverrides(parseFlags([]string{"--model", "nope=model"})); err == nil {
		t.Fatal("unknown stage must be rejected")
	}
	if _, err := parseModelOverrides(parseFlags([]string{"--model", "implementer=noslash"})); err == nil {
		t.Fatal("model without a provider prefix must be rejected")
	}
}

func TestHelpMentionsCommands(t *testing.T) {
	for _, command := range []string{"init", "run", "gates", "status", "doctor", "models"} {
		if !strings.Contains(Help, command) {
			t.Errorf("help is missing %q", command)
		}
	}
}

func fakeOpenCodeCLI(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
if [ "$1" = "models" ]; then
  echo "opencode/big-pickle"
  echo "opencode/deepseek-v4.1-flash-free"
  exit 0
fi
if [ "$1" = "--version" ]; then
  echo "1.2.3"
  exit 0
fi
exit 0
`
	if err := os.WriteFile(filepath.Join(dir, "opencode"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func writeCLIConfig(t *testing.T, dir string, mutate func(*config.Config)) {
	t.Helper()
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
	if mutate != nil {
		mutate(&cfg)
	}
	if err := config.WriteFile(filepath.Join(dir, "forge.config.json"), cfg); err != nil {
		t.Fatal(err)
	}
}

func TestMainDispatch(t *testing.T) {
	if code := Main([]string{"version"}); code != 0 {
		t.Fatalf("version exit = %d", code)
	}
	if code := Main([]string{"--version"}); code != 0 {
		t.Fatalf("--version exit = %d", code)
	}
	if code := Main([]string{"help"}); code != 0 {
		t.Fatalf("help exit = %d", code)
	}
	if code := Main(nil); code != 0 {
		t.Fatalf("no-arg exit = %d", code)
	}
	if code := Main([]string{"not-a-command"}); code != 1 {
		t.Fatalf("unknown command exit = %d", code)
	}
}

func TestInitStatusAndDryRun(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	fakeOpenCodeCLI(t)
	dir := filepath.Join(t.TempDir(), "workspace")
	if code := Main([]string{"init", dir, "--template", "none", "--goal", "Build a thing"}); code != 0 {
		t.Fatalf("init exit = %d", code)
	}
	if !util.FileExists(filepath.Join(dir, "forge.config.json")) {
		t.Fatal("config was not created")
	}
	if code := Main([]string{"status", dir}); code != 0 {
		t.Fatalf("status exit = %d", code)
	}
	if code := Main([]string{"run", dir, "--dry-run"}); code != 0 {
		t.Fatalf("dry-run exit = %d", code)
	}
	if code := Main([]string{"init", dir, "--template", "nope"}); code != 1 {
		t.Fatalf("invalid template exit = %d", code)
	}
}

func TestGatesCommand(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	writeCLIConfig(t, dir, nil)
	if code := Main([]string{"gates", dir}); code != 0 {
		t.Fatalf("passing gates exit = %d", code)
	}
	writeCLIConfig(t, dir, func(cfg *config.Config) {
		cfg.Commands.Test = &config.Command{Command: "false"}
	})
	if code := Main([]string{"gates", dir}); code != 1 {
		t.Fatalf("failing gates exit = %d", code)
	}
}

func TestDoctorAndModelsCommands(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	fakeOpenCodeCLI(t)
	dir := t.TempDir()
	writeCLIConfig(t, dir, nil)
	if code := Main([]string{"doctor", dir}); code != 0 {
		t.Fatalf("doctor exit = %d", code)
	}
	if code := Main([]string{"doctor", dir, "--json"}); code != 0 {
		t.Fatalf("doctor json exit = %d", code)
	}
	if code := Main([]string{"models", dir, "--json"}); code != 0 {
		t.Fatalf("models json exit = %d", code)
	}
	if code := Main([]string{"models", dir}); code != 0 {
		t.Fatalf("models exit = %d", code)
	}
}

func TestStatusJSON(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	if err := util.WriteJSON(filepath.Join(dir, ".forge", "state.json"), map[string]any{
		"version": 1,
		"runs": []map[string]any{{
			"id": "run-1", "status": "success", "goal": "x", "iterations": 1,
			"report": ".forge/runs/run-1/report.md",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if code := Main([]string{"status", dir, "--json"}); code != 0 {
		t.Fatalf("status json exit = %d", code)
	}
	if code := Main([]string{"status", dir, "--report"}); code != 0 {
		t.Fatalf("status report exit = %d", code)
	}
}
