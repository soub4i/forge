package models

import (
	"strings"
	"testing"
	"time"

	"github.com/soub4i/forge/internal/config"
	"github.com/soub4i/forge/internal/util"
)

func TestBaseAndAvailability(t *testing.T) {
	if got := Base("opencode/big-pickle#thinking"); got != "opencode/big-pickle" {
		t.Fatalf("Base = %q", got)
	}
	available := []string{"opencode/big-pickle", "opencode/glm-5.3-flash-free"}
	if !IsAvailable("opencode/big-pickle#fast", available) {
		t.Fatal("variant of an available model should be available")
	}
	if IsAvailable("opencode/missing", available) {
		t.Fatal("unknown model reported as available")
	}
	if !IsAvailable("anything", nil) {
		t.Fatal("an empty availability list must trust the config")
	}
}

func TestResolveCandidatesAndFallbacks(t *testing.T) {
	cfg := config.Default()
	available := []string{"opencode/glm-5.3-flash-free", "opencode/deepseek-v4.1-flash-free"}
	resolved := Resolve(cfg, "implementer", available, "")
	if resolved.Selected != "opencode/deepseek-v4.1-flash-free" {
		t.Fatalf("selected = %q", resolved.Selected)
	}
	if len(resolved.Unavailable) == 0 {
		t.Fatal("unavailable models should be reported")
	}

	onlyFallback := Resolve(cfg, "implementer", []string{"opencode/minimax-m3.1-flash-free"}, "")
	if onlyFallback.Selected != "opencode/minimax-m3.1-flash-free" {
		t.Fatalf("fallback selection = %q", onlyFallback.Selected)
	}

	override := Resolve(cfg, "implementer", nil, "opencode/custom/model")
	if override.Selected != "opencode/custom/model" || override.Candidates[0] != "opencode/custom/model" {
		t.Fatalf("override = %+v", override)
	}
	if missing := Resolve(cfg, "nope", nil, ""); len(missing.Candidates) != 0 {
		t.Fatalf("unknown stage = %+v", missing)
	}
}

func TestPickInitial(t *testing.T) {
	cfg := config.Default()
	picked := PickInitial(cfg, []string{"opencode/glm-5.3-flash-free"})
	implementer := picked["implementer"]
	if implementer.Model != "opencode/glm-5.3-flash-free" {
		t.Fatalf("model = %q", implementer.Model)
	}
	for _, fallback := range implementer.Fallbacks {
		if fallback == implementer.Model {
			t.Fatalf("selected model is still a fallback: %v", implementer.Fallbacks)
		}
	}
	trusted := PickInitial(cfg, nil)
	if trusted["architect"].Model != cfg.Models["architect"].Model {
		t.Fatal("with no availability data the configured primary should be kept")
	}
}

func TestEstimateCost(t *testing.T) {
	cfg := config.Default()
	cfg.Pricing["opencode/big-pickle"] = config.Pricing{Input: 2, Output: 4}
	tokens := util.Tokens{Input: 1_000_000, Output: 500_000}
	if got := EstimateCost(cfg, "opencode/big-pickle#high", tokens); got != 4 {
		t.Fatalf("estimate = %v, want 4", got)
	}
	if got := EstimateCost(cfg, "unknown/model", tokens); got != 0 {
		t.Fatalf("unknown model estimate = %v", got)
	}
}

func TestLedgerAddAndSummary(t *testing.T) {
	cfg := config.Default()
	cfg.Pricing["opencode/glm-5.3-flash-free"] = config.Pricing{Input: 1, Output: 10}
	ledger := NewLedger(config.Budget{MaxUSD: 100})
	entry := ledger.Add(cfg, "tester", 1, "opencode/glm-5.3-flash-free", 0, util.Tokens{Input: 1000, Output: 100}, 250)
	if entry.Estimated != 0.002 {
		t.Fatalf("estimated = %v", entry.Estimated)
	}
	if entry.CostUSD != 0.002 {
		t.Fatalf("cost = %v", entry.CostUSD)
	}
	actual := ledger.Add(cfg, "tester", 1, "opencode/glm-5.3-flash-free", 0.5, util.Tokens{Input: 10, Output: 5}, 10)
	if actual.CostUSD != 0.5 {
		t.Fatalf("provider cost should win: %v", actual.CostUSD)
	}
	summary := ledger.Summary()
	if summary.CostUSD < 0.5 || len(summary.Entries) != 2 {
		t.Fatalf("summary = %+v", summary)
	}
	if summary.Tokens.Input != 1010 || summary.Tokens.Output != 105 {
		t.Fatalf("tokens = %+v", summary.Tokens)
	}
}

func TestOverBudget(t *testing.T) {
	ledger := NewLedger(config.Budget{MaxUSD: 1, MaxTokens: 100})
	if status := ledger.OverBudget(); status.Over {
		t.Fatalf("fresh ledger reported over budget: %+v", status)
	}
	ledger.CostUSD = 2
	if status := ledger.OverBudget(); !status.Over || status.Reason != "budget_usd" {
		t.Fatalf("usd status = %+v", status)
	}
	ledger.CostUSD = 0
	ledger.Tokens = util.Tokens{Input: 80, Output: 30}
	if status := ledger.OverBudget(); !status.Over || status.Reason != "budget_tokens" {
		t.Fatalf("token status = %+v", status)
	}
	ledger.Tokens = util.Tokens{}
	ledger.Budget = config.Budget{MaxMinutes: 1}
	ledger.StartedAt = time.Now().Add(-2 * time.Minute)
	if status := ledger.OverBudget(); !status.Over || status.Reason != "budget_time" {
		t.Fatalf("time status = %+v", status)
	}
	ledger.Budget = config.Budget{}
	if status := ledger.OverBudget(); status.Over {
		t.Fatalf("unlimited budget reported over: %+v", status)
	}
}

func TestRowsAndFormatTable(t *testing.T) {
	cfg := config.Default()
	cfg.Pricing["opencode/big-pickle"] = config.Pricing{InputPerM: 3, OutputPerM: 15}
	rows := Rows(cfg)
	if len(rows) != len(config.DefaultModels) {
		t.Fatalf("rows = %d", len(rows))
	}
	if rows[0].Stage != "architect" {
		t.Fatalf("rows are not sorted by stage: %+v", rows)
	}
	table := FormatTable(rows)
	for _, want := range []string{"STAGE", "TIER", "MODEL", "INPUT $/M", "architect", "opencode/big-pickle", "15"} {
		if !strings.Contains(table, want) {
			t.Errorf("table is missing %q:\n%s", want, table)
		}
	}
	empty := FormatTable(nil)
	if !strings.Contains(empty, "STAGE") {
		t.Fatalf("empty table = %q", empty)
	}
}
