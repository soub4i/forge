package models

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/soub4i/forge/internal/config"
	"github.com/soub4i/forge/internal/util"
)

// ListAvailable returns model IDs known to the local OpenCode install.
func ListAvailable(ctx context.Context) []string {
	result := util.RunCommand(ctx, "opencode models", "", 20*time.Second)
	if !result.OK {
		return nil
	}
	var out []string
	for _, line := range strings.Split(result.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "/") {
			continue
		}
		out = append(out, line)
	}
	return out
}

func Base(model string) string {
	return strings.Split(model, "#")[0]
}

func IsAvailable(model string, available []string) bool {
	if len(available) == 0 {
		return true
	}
	base := Base(model)
	for _, item := range available {
		if Base(item) == base {
			return true
		}
	}
	return false
}

type Candidates struct {
	Stage       string
	Primary     string
	Candidates  []string
	Selected    string
	Unavailable []string
}

func Resolve(cfg config.Config, stage string, available []string, override string) Candidates {
	candidates := cfg.ModelCandidates(stage, override)
	if len(candidates) == 0 {
		return Candidates{Stage: stage}
	}
	primary := candidates[0]
	if len(available) == 0 {
		return Candidates{Stage: stage, Primary: primary, Candidates: candidates, Selected: primary}
	}
	var usable, unavailable []string
	for _, model := range candidates {
		if IsAvailable(model, available) {
			usable = append(usable, model)
		} else {
			unavailable = append(unavailable, model)
		}
	}
	selected := primary
	if len(usable) > 0 {
		selected = usable[0]
	}
	return Candidates{Stage: stage, Primary: primary, Candidates: candidates, Selected: selected, Unavailable: unavailable}
}

func PickInitial(cfg config.Config, available []string) map[string]config.ModelEntry {
	out := map[string]config.ModelEntry{}
	for stage, entry := range cfg.Models {
		candidates := append([]string{entry.Model}, entry.Fallbacks...)
		selected := entry.Model
		for _, model := range candidates {
			if IsAvailable(model, available) {
				selected = model
				break
			}
		}
		fallbacks := make([]string, 0, len(entry.Fallbacks))
		for _, model := range entry.Fallbacks {
			if model != selected {
				fallbacks = append(fallbacks, model)
			}
		}
		out[stage] = config.ModelEntry{Model: selected, Tier: entry.Tier, Fallbacks: fallbacks}
	}
	return out
}

func EstimateCost(cfg config.Config, model string, tokens util.Tokens) float64 {
	pricing := cfg.PricingFor(model)
	return (float64(tokens.Input)/1_000_000)*pricing.InputPrice() + (float64(tokens.Output)/1_000_000)*pricing.OutputPrice()
}

type LedgerEntry struct {
	Stage      string      `json:"stage"`
	Iteration  int         `json:"iteration"`
	Model      string      `json:"model"`
	DurationMs int64       `json:"durationMs"`
	Tokens     util.Tokens `json:"tokens"`
	CostUSD    float64     `json:"costUsd"`
	ActualCost float64     `json:"actualCostUsd"`
	Estimated  float64     `json:"estimatedCostUsd"`
}

type LedgerSummary struct {
	CostUSD   float64       `json:"costUsd"`
	Tokens    util.Tokens   `json:"tokens"`
	ElapsedMs int64         `json:"elapsedMs"`
	Entries   []LedgerEntry `json:"entries"`
}

func (l *Ledger) Summary() LedgerSummary {
	return LedgerSummary{CostUSD: l.CostUSD, Tokens: l.Tokens, ElapsedMs: l.Elapsed().Milliseconds(), Entries: l.Entries}
}

type Ledger struct {
	Budget    config.Budget
	Entries   []LedgerEntry
	StartedAt time.Time
	CostUSD   float64
	Tokens    util.Tokens
}

func NewLedger(budget config.Budget) *Ledger {
	return &Ledger{Budget: budget, StartedAt: time.Now()}
}

func (l *Ledger) Add(cfg config.Config, stage string, iteration int, model string, costUSD float64, tokens util.Tokens, durationMs int64) LedgerEntry {
	estimated := EstimateCost(cfg, model, tokens)
	actual := costUSD
	cost := actual
	if cost <= 0 {
		cost = estimated
	}
	entry := LedgerEntry{
		Stage: stage, Iteration: iteration, Model: model, DurationMs: durationMs,
		Tokens: tokens, CostUSD: cost, ActualCost: actual, Estimated: estimated,
	}
	l.Entries = append(l.Entries, entry)
	l.CostUSD += cost
	l.Tokens.Input += tokens.Input
	l.Tokens.Output += tokens.Output
	l.Tokens.Reasoning += tokens.Reasoning
	l.Tokens.CacheRead += tokens.CacheRead
	l.Tokens.CacheWrite += tokens.CacheWrite
	return entry
}

func (l *Ledger) Elapsed() time.Duration { return time.Since(l.StartedAt) }

type BudgetStatus struct {
	Over   bool
	Reason string
	Detail string
}

func (l *Ledger) OverBudget() BudgetStatus {
	if l.Budget.MaxUSD > 0 && l.CostUSD > l.Budget.MaxUSD {
		return BudgetStatus{true, "budget_usd", fmt.Sprintf("Spent %s of %s", util.FormatUSD(l.CostUSD), util.FormatUSD(l.Budget.MaxUSD))}
	}
	if l.Budget.MaxTokens > 0 && l.Tokens.Input+l.Tokens.Output > l.Budget.MaxTokens {
		return BudgetStatus{true, "budget_tokens", fmt.Sprintf("Used %d of %d tokens", l.Tokens.Input+l.Tokens.Output, l.Budget.MaxTokens)}
	}
	if l.Budget.MaxMinutes > 0 && l.Elapsed() > time.Duration(l.Budget.MaxMinutes*float64(time.Minute)) {
		return BudgetStatus{true, "budget_time", fmt.Sprintf("Ran %d of %.0f minutes", int(l.Elapsed().Minutes()), l.Budget.MaxMinutes)}
	}
	return BudgetStatus{}
}

type Row struct {
	Stage     string         `json:"stage"`
	Tier      string         `json:"tier"`
	Model     string         `json:"model"`
	Fallbacks []string       `json:"fallbacks"`
	Pricing   config.Pricing `json:"pricing"`
}

func Rows(cfg config.Config) []Row {
	stages := make([]string, 0, len(cfg.Models))
	for stage := range cfg.Models {
		stages = append(stages, stage)
	}
	sort.Strings(stages)
	rows := make([]Row, 0, len(stages))
	for _, stage := range stages {
		entry := cfg.Models[stage]
		rows = append(rows, Row{Stage: stage, Tier: entry.Tier, Model: entry.Model, Fallbacks: entry.Fallbacks, Pricing: cfg.PricingFor(entry.Model)})
	}
	return rows
}

func FormatTable(rows []Row) string {
	stageWidth, tierWidth, modelWidth := 10, 9, 20
	for _, row := range rows {
		if len(row.Stage) > stageWidth {
			stageWidth = len(row.Stage)
		}
		if len(row.Tier) > tierWidth {
			tierWidth = len(row.Tier)
		}
		if len(row.Model) > modelWidth {
			modelWidth = len(row.Model)
		}
	}
	header := fmt.Sprintf("%-*s  %-*s  %-*s  %9s  %10s", stageWidth, "STAGE", tierWidth, "TIER", modelWidth, "MODEL", "INPUT $/M", "OUTPUT $/M")
	lines := []string{header, strings.Repeat("-", len(header))}
	for _, row := range rows {
		lines = append(lines, fmt.Sprintf("%-*s  %-*s  %-*s  %9.0f  %10.0f", stageWidth, row.Stage, tierWidth, row.Tier, modelWidth, row.Model, row.Pricing.InputPrice(), row.Pricing.OutputPrice()))
	}
	return strings.Join(lines, "\n")
}
