package prompts

import (
	"strings"
	"testing"

	"github.com/soub4i/forge/internal/benchmark"
	"github.com/soub4i/forge/internal/config"
	"github.com/soub4i/forge/internal/context"
	"github.com/soub4i/forge/internal/gates"
	"github.com/soub4i/forge/internal/security"
)

func promptContext() Context {
	cfg := config.Default()
	cfg.Goal = "Build a fast developer CLI"
	cfg.Acceptance.Benchmarks.Thresholds = []config.Threshold{{Metric: "lines_per_sec", Min: floatPtr(1000), Unit: "lines/sec"}}
	return Context{
		Cwd:           "/tmp/example",
		Config:        cfg,
		Iteration:     2,
		MaxIterations: 5,
		Resolved: gates.Resolved{
			Commands: map[string]*gates.CommandSpec{
				"test":  {Command: "npm test", Source: "package.json#scripts.test"},
				"build": {Command: "npm run build", Source: "config"},
			},
			Sources: map[string]string{"test": "package.json#scripts.test", "build": "config"},
		},
		Snapshot: context.Snapshot{Tree: "README.md (10 B)"},
	}
}

func floatPtr(value float64) *float64 { return &value }

func TestBuildEveryStage(t *testing.T) {
	stages := map[string][]string{
		"architect":   {"Architect stage", "docs/ARCHITECTURE.md", "docs/PLAN.md", "architecture summary"},
		"designer":    {"Designer stage", "docs/DESIGN.md", "interface surface"},
		"implementer": {"Implementer stage", "Implement the next vertical slice", "files changed"},
		"tester":      {"Tester stage", "Harden the implementation", "coverage"},
		"fixer":       {"Fixer stage", "machine-checked gates failed", "root cause"},
		"benchmarker": {"Benchmarker stage", "bench/results.json", "lines_per_sec"},
		"security":    {"Security review stage", ".forge/security.json", "docs/SECURITY.md"},
		"reviewer":    {"Independent review stage", ".forge/review.json", "docs/REVIEW.md"},
		"release":     {"Release stage", "CHANGELOG.md", "docs/RELEASE.md"},
		"triage":      {"Failure triage stage", ".forge/triage.md", "root cause"},
	}
	for stage, wants := range stages {
		t.Run(stage, func(t *testing.T) {
			prompt := Build(stage, promptContext())
			for _, want := range wants {
				if !strings.Contains(prompt, want) {
					t.Errorf("%s prompt is missing %q", stage, want)
				}
			}
			for _, common := range []string{
				"Build a fast developer CLI",
				"## Acceptance criteria (machine-enforced)",
				"## Harness commands",
				"### Repository files",
				"## Your stage",
				"## Output contract",
				"Never edit `forge.config.json`",
			} {
				if !strings.Contains(prompt, common) {
					t.Errorf("%s prompt is missing common section %q", stage, common)
				}
			}
		})
	}
}

func TestPromptsAreEcosystemNeutral(t *testing.T) {
	prompt := Build("implementer", promptContext())
	for _, want := range []string{
		"repository's existing language, build system, package manager, and conventions",
		"project's conventional entry point",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("implementer prompt is missing %q", want)
		}
	}
	for _, unwanted := range []string{"npm install", "Node.js project"} {
		if strings.Contains(prompt, unwanted) {
			t.Errorf("implementer prompt still assumes a Node.js project: %q", unwanted)
		}
	}
	designer := Build("designer", promptContext())
	if strings.Contains(designer, "CLI commands/flags") {
		t.Fatal("designer prompt should not assume a CLI-only project")
	}
}

func TestBuildUnknownStage(t *testing.T) {
	prompt := Build("mystery", promptContext())
	if !strings.Contains(prompt, "Continue the goal-driven loop.") {
		t.Fatalf("unknown stage prompt = %q", prompt)
	}
	if !strings.Contains(prompt, "Reply with a concise summary.") {
		t.Fatalf("unknown stage contract = %q", prompt)
	}
}

func TestCommandTable(t *testing.T) {
	table := commandTable(promptContext().Resolved)
	for _, want := range []string{
		"- install: (missing)",
		"- test: `npm test` (package.json#scripts.test)",
		"- build: `npm run build` (config)",
	} {
		if !strings.Contains(table, want) {
			t.Errorf("command table is missing %q:\n%s", want, table)
		}
	}
	empty := commandTable(gates.Resolved{Commands: map[string]*gates.CommandSpec{}})
	if !strings.Contains(empty, "- benchmark: (missing)") {
		t.Fatalf("empty table = %q", empty)
	}
}

func TestFeedbackBlockIncludesAllEvidence(t *testing.T) {
	ctx := promptContext()
	ctx.Feedback = "the tests failed"
	ctx.GateReport = "### Machine-checked gates"
	ctx.SecurityGate = &security.Gate{
		OK: false, MaxAllowed: "medium",
		Findings: []security.Finding{{Severity: "high", Title: "shell injection", Recommendation: "quote arguments"}},
		Blocking: []security.Finding{{Severity: "high", Title: "shell injection", Recommendation: "quote arguments"}},
	}
	ctx.ReviewGate = &gates.ReviewGate{
		OK: false, Verdict: "changes_requested",
		Blocking: []gates.ReviewFinding{{Severity: "high", Title: "missing edge case", Recommendation: "add a test"}},
		Summary:  "not ready",
	}
	ctx.ScannerResults = []security.Finding{{Severity: "medium", Title: "vulnerable dependency", File: "package-lock.json", Line: 3}}
	ctx.Benchmark = &benchmark.Result{Metrics: map[string]float64{"lines_per_sec": 12}, Source: "bench/results.json"}
	ctx.History = []string{"Iteration 1: code gates fail (test)"}

	block := feedbackBlock(ctx)
	for _, want := range []string{
		"Feedback from previous iteration",
		"Latest machine gate report",
		"Security review",
		"Independent review",
		"Deterministic security scanner findings",
		"Benchmark evidence",
		"Iteration history",
		"Iteration 1: code gates fail",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("feedback is missing %q:\n%s", want, block)
		}
	}
	if feedbackBlock(Context{}) != "" {
		t.Fatal("empty context should produce no feedback block")
	}
}

func TestOutputContractBenchmarkMetrics(t *testing.T) {
	contract := outputContract("benchmarker", promptContext())
	if !strings.Contains(contract, "lines_per_sec") {
		t.Fatalf("benchmark contract = %q", contract)
	}
	if !strings.Contains(contract, "whether thresholds pass") {
		t.Fatalf("benchmark contract = %q", contract)
	}
	noThresholds := promptContext()
	noThresholds.Config.Acceptance.Benchmarks.Thresholds = nil
	if contract := outputContract("benchmarker", noThresholds); !strings.Contains(contract, "all reported metrics") {
		t.Fatalf("fallback benchmark contract = %q", contract)
	}
}

func TestWriteScopesStayNarrow(t *testing.T) {
	cases := map[string]string{
		"security":    "Treat source code as read-only",
		"reviewer":    "Treat source code as read-only",
		"triage":      "Treat source code as read-only",
		"architect":   "Do not edit production code",
		"designer":    "Do not edit production code",
		"release":     "Do not change application logic or tests",
		"implementer": "production source",
	}
	for stage, want := range cases {
		prompt := Build(stage, promptContext())
		if !strings.Contains(writeScopes[stage], want) && !strings.Contains(prompt, want) {
			t.Errorf("stage %s does not declare a narrow scope (%q)", stage, want)
		}
	}
}
