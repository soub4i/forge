package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/soub4i/forge/internal/util"
)

const Version = 1

var ConfigFilenames = []string{"forge.config.json", "harness.config.json", filepath.Join(".forge", "config.json")}

type ConfigError struct {
	Message string
	Issues  []string
}

func (e *ConfigError) Error() string { return e.Message }

type Coverage struct {
	Min  float64 `json:"min"`
	File *string `json:"file"`
}

func (c *Coverage) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "null" || trimmed == "false" {
		c.Min, c.File = 0, nil
		return nil
	}
	var number float64
	if err := json.Unmarshal(data, &number); err == nil {
		c.Min, c.File = number, nil
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		fmt.Sscanf(text, "%f", &number)
		c.Min, c.File = number, nil
		return nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	c.Min, c.File = 0, nil
	for _, key := range []string{"min", "threshold", "percent"} {
		if value, ok := raw[key]; ok {
			_ = json.Unmarshal(value, &c.Min)
			break
		}
	}
	if value, ok := raw["file"]; ok {
		var file string
		if json.Unmarshal(value, &file) == nil && file != "" {
			c.File = &file
		}
	}
	return nil
}

type Threshold struct {
	Metric string   `json:"metric"`
	Min    *float64 `json:"min"`
	Max    *float64 `json:"max"`
	Unit   string   `json:"unit"`
}

type Benchmarks struct {
	Required    bool        `json:"required"`
	ResultsFile string      `json:"resultsFile"`
	Thresholds  []Threshold `json:"thresholds"`
}

func (b *Benchmarks) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "null" || trimmed == "false" {
		b.Required, b.ResultsFile, b.Thresholds = false, "bench/results.json", nil
		return nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var thresholds []Threshold
		if err := json.Unmarshal(data, &thresholds); err != nil {
			return err
		}
		b.ResultsFile = "bench/results.json"
		b.Thresholds = normalizeThresholds(thresholds)
		b.Required = len(b.Thresholds) > 0
		return nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil // true or an unknown scalar: keep defaults
	}
	if value, ok := raw["required"]; ok {
		_ = json.Unmarshal(value, &b.Required)
	}
	if value, ok := raw["resultsFile"]; ok {
		var file string
		if json.Unmarshal(value, &file) == nil && file != "" {
			b.ResultsFile = file
		}
	}
	if b.ResultsFile == "" {
		b.ResultsFile = "bench/results.json"
	}
	if value, ok := raw["thresholds"]; ok {
		var thresholds []Threshold
		if json.Unmarshal(value, &thresholds) == nil {
			b.Thresholds = normalizeThresholds(thresholds)
		}
	}
	if _, hasRequired := raw["required"]; !hasRequired {
		b.Required = len(b.Thresholds) > 0
	}
	return nil
}

func normalizeThresholds(values []Threshold) []Threshold {
	out := make([]Threshold, 0, len(values))
	for _, threshold := range values {
		threshold.Metric = strings.TrimSpace(threshold.Metric)
		out = append(out, threshold)
	}
	return out
}

type Security struct {
	Enabled         bool      `json:"enabled"`
	MaxSeverity     string    `json:"maxSeverity"`
	RequireNoSecret bool      `json:"requireNoSecrets"`
	Scanners        []Scanner `json:"scanners"`
	Ignore          []string  `json:"ignore"`
}

type Scanner struct {
	Name      string `json:"name,omitempty"`
	Command   string `json:"command"`
	Format    string `json:"format,omitempty"`
	Required  bool   `json:"required,omitempty"`
	TimeoutMs int    `json:"timeoutMs,omitempty"`
}

type Review struct {
	Enabled             bool `json:"enabled"`
	MaxBlockingFindings int  `json:"maxBlockingFindings"`
}

type Acceptance struct {
	Tests      bool       `json:"tests"`
	Coverage   Coverage   `json:"coverage"`
	Lint       bool       `json:"lint"`
	Typecheck  bool       `json:"typecheck"`
	Build      bool       `json:"build"`
	Benchmarks Benchmarks `json:"benchmarks"`
	Security   Security   `json:"security"`
	Review     Review     `json:"review"`
}

type Command struct {
	Command   string `json:"command"`
	TimeoutMs int    `json:"timeoutMs,omitempty"`
	Optional  bool   `json:"optional,omitempty"`
}

func (c *Command) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "null" {
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		c.Command = text
		return nil
	}
	type alias Command
	var value alias
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*c = Command(value)
	return nil
}

type Commands struct {
	Install   *Command `json:"install"`
	Test      *Command `json:"test"`
	Coverage  *Command `json:"coverage"`
	Lint      *Command `json:"lint"`
	Typecheck *Command `json:"typecheck"`
	Build     *Command `json:"build"`
	Benchmark *Command `json:"benchmark"`
}

func (c *Commands) All() map[string]*Command {
	return map[string]*Command{
		"install": c.Install, "test": c.Test, "coverage": c.Coverage,
		"lint": c.Lint, "typecheck": c.Typecheck, "build": c.Build, "benchmark": c.Benchmark,
	}
}

type Pricing struct {
	Input            float64 `json:"input,omitempty"`
	Output           float64 `json:"output,omitempty"`
	InputPerM        float64 `json:"inputPerM,omitempty"`
	OutputPerM       float64 `json:"outputPerM,omitempty"`
	InputPerMillion  float64 `json:"inputPerMillion,omitempty"`
	OutputPerMillion float64 `json:"outputPerMillion,omitempty"`
}

func (p Pricing) InputPrice() float64 {
	for _, value := range []float64{p.Input, p.InputPerM, p.InputPerMillion} {
		if value != 0 {
			return value
		}
	}
	return 0
}

func (p Pricing) OutputPrice() float64 {
	for _, value := range []float64{p.Output, p.OutputPerM, p.OutputPerMillion} {
		if value != 0 {
			return value
		}
	}
	return 0
}

type ModelEntry struct {
	Model     string   `json:"model"`
	Tier      string   `json:"tier,omitempty"`
	Fallbacks []string `json:"fallbacks,omitempty"`
	Pricing   *Pricing `json:"pricing,omitempty"`
}

func (m *ModelEntry) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		m.Model = text
		return nil
	}
	type alias ModelEntry
	var value alias
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*m = ModelEntry(value)
	return nil
}

type Budget struct {
	MaxUSD        float64 `json:"maxUsd"`
	MaxMinutes    float64 `json:"maxMinutes"`
	MaxIterations int     `json:"maxIterations"`
	MaxTokens     int64   `json:"maxTokens"`
}

type Loop struct {
	RepairAttempts      int `json:"repairAttempts"`
	EscalateAfter       int `json:"escalateAfter"`
	StageTimeoutMinutes int `json:"stageTimeoutMinutes"`
	ModelRetries        int `json:"modelRetries"`
}

type ContextConfig struct {
	MaxFiles    int `json:"maxFiles"`
	MaxDocChars int `json:"maxDocChars"`
}

type Git struct {
	Checkpoint bool `json:"checkpoint"`
}

type Release struct {
	Enabled bool `json:"enabled"`
	Tag     bool `json:"tag"`
}

type Config struct {
	Schema     string                `json:"$schema,omitempty"`
	Version    int                   `json:"version"`
	Goal       string                `json:"goal"`
	Acceptance Acceptance            `json:"acceptance"`
	Commands   Commands              `json:"commands"`
	Models     map[string]ModelEntry `json:"models"`
	Pricing    map[string]Pricing    `json:"pricing"`
	Budget     Budget                `json:"budget"`
	Loop       Loop                  `json:"loop"`
	Context    ContextConfig         `json:"context"`
	Git        Git                   `json:"git"`
	Release    Release               `json:"release"`
}

var DefaultModels = map[string]ModelEntry{
	"architect":   {Model: "opencode/big-pickle", Tier: "reasoning", Fallbacks: []string{"opencode/nemotron-3.5-super-vl-free", "opencode/longcat-2.5-free"}},
	"designer":    {Model: "opencode/longcat-2.5-free", Tier: "reasoning", Fallbacks: []string{"opencode/big-pickle", "opencode/nemotron-3.5-super-vl-free"}},
	"implementer": {Model: "opencode/deepseek-v4.1-flash-free", Tier: "balanced", Fallbacks: []string{"opencode/glm-5.3-flash-free", "opencode/minimax-m3.1-flash-free"}},
	"tester":      {Model: "opencode/glm-5.3-flash-free", Tier: "balanced", Fallbacks: []string{"opencode/minimax-m3.1-flash-free", "opencode/deepseek-v4.1-flash-free"}},
	"fixer":       {Model: "opencode/minimax-m3.1-flash-free", Tier: "fast", Fallbacks: []string{"opencode/mimo-v2.6-flash-free", "opencode/deepseek-v4.1-flash-free"}},
	"benchmarker": {Model: "opencode/flint-40b-free", Tier: "fast", Fallbacks: []string{"opencode/mimo-v2.6-flash-free", "opencode/nemotron-3.5-lightning-free"}},
	"security":    {Model: "opencode/nemotron-3.5-super-vl-free", Tier: "reasoning", Fallbacks: []string{"opencode/big-pickle", "opencode/longcat-2.5-free"}},
	"reviewer":    {Model: "opencode/big-pickle", Tier: "reasoning", Fallbacks: []string{"opencode/longcat-2.5-free", "opencode/nemotron-3.5-super-vl-free"}},
	"release":     {Model: "opencode/inkling-2-small-free", Tier: "fast", Fallbacks: []string{"opencode/nemotron-3.5-lightning-free", "opencode/mimo-v2.6-flash-free"}},
	"triage":      {Model: "opencode/nemotron-3.5-lightning-free", Tier: "fast", Fallbacks: []string{"opencode/inkling-2-small-free", "opencode/flint-40b-free"}},
}

var StageLabels = map[string]string{
	"architect": "Architect", "designer": "Designer", "implementer": "Implementer",
	"tester": "Tester", "fixer": "Fixer", "benchmarker": "Benchmarker",
	"security": "Security review", "reviewer": "Independent review",
	"release": "Release", "triage": "Failure triage",
}

var StageTiers = map[string]string{
	"architect": "reasoning", "designer": "reasoning", "implementer": "balanced",
	"tester": "balanced", "fixer": "fast", "benchmarker": "fast",
	"security": "reasoning", "reviewer": "reasoning", "release": "fast", "triage": "fast",
}

// StageNames returns the known model stages in a stable order.
func StageNames() []string {
	names := make([]string, 0, len(DefaultModels))
	for stage := range DefaultModels {
		names = append(names, stage)
	}
	sort.Strings(names)
	return names
}

func Default() Config {
	models := make(map[string]ModelEntry, len(DefaultModels))
	for name, entry := range DefaultModels {
		models[name] = ModelEntry{Model: entry.Model, Tier: entry.Tier, Fallbacks: append([]string(nil), entry.Fallbacks...)}
	}
	return Config{
		Version: Version,
		Acceptance: Acceptance{
			Tests:      true,
			Coverage:   Coverage{Min: 0, File: nil},
			Lint:       true,
			Typecheck:  true,
			Build:      true,
			Benchmarks: Benchmarks{Required: false, ResultsFile: "bench/results.json", Thresholds: nil},
			Security:   Security{Enabled: true, MaxSeverity: "medium", RequireNoSecret: true, Scanners: nil, Ignore: nil},
			Review:     Review{Enabled: true, MaxBlockingFindings: 0},
		},
		Models:  models,
		Pricing: map[string]Pricing{},
		Budget:  Budget{MaxUSD: 10, MaxMinutes: 180, MaxIterations: 8, MaxTokens: 0},
		Loop:    Loop{RepairAttempts: 1, EscalateAfter: 2, StageTimeoutMinutes: 20, ModelRetries: 1},
		Context: ContextConfig{MaxFiles: 250, MaxDocChars: 6000},
		Git:     Git{Checkpoint: false},
		Release: Release{Enabled: true, Tag: false},
	}
}

func Normalize(c *Config) {
	if c.Version == 0 {
		c.Version = Version
	}
	c.Goal = strings.TrimSpace(c.Goal)
	if c.Acceptance.Benchmarks.ResultsFile == "" {
		c.Acceptance.Benchmarks.ResultsFile = "bench/results.json"
	}
	c.Acceptance.Benchmarks.Thresholds = normalizeThresholds(c.Acceptance.Benchmarks.Thresholds)
	if c.Acceptance.Security.MaxSeverity == "" {
		c.Acceptance.Security.MaxSeverity = "medium"
	}
	c.Acceptance.Security.MaxSeverity = NormalizeSeverity(c.Acceptance.Security.MaxSeverity, "medium")
	if c.Budget.MaxIterations < 0 {
		c.Budget.MaxIterations = 0
	}
	if c.Loop.RepairAttempts < 0 {
		c.Loop.RepairAttempts = 0
	}
	if c.Loop.EscalateAfter < 1 {
		c.Loop.EscalateAfter = 1
	}
	if c.Loop.StageTimeoutMinutes < 1 {
		c.Loop.StageTimeoutMinutes = 1
	}
	if c.Loop.ModelRetries < 0 {
		c.Loop.ModelRetries = 0
	}
	if c.Context.MaxFiles < 20 {
		c.Context.MaxFiles = 20
	}
	if c.Context.MaxDocChars < 1000 {
		c.Context.MaxDocChars = 1000
	}
	if c.Models == nil {
		c.Models = map[string]ModelEntry{}
	}
	for stage, fallback := range DefaultModels {
		entry, ok := c.Models[stage]
		if !ok {
			entry = ModelEntry{Model: fallback.Model, Tier: fallback.Tier, Fallbacks: append([]string(nil), fallback.Fallbacks...)}
		}
		if entry.Model == "" {
			entry.Model = fallback.Model
		}
		if entry.Tier == "" {
			entry.Tier = fallback.Tier
		}
		if entry.Fallbacks == nil {
			entry.Fallbacks = append([]string(nil), fallback.Fallbacks...)
		}
		cleaned := make([]string, 0, len(entry.Fallbacks))
		seen := map[string]bool{entry.Model: true}
		for _, model := range entry.Fallbacks {
			model = strings.TrimSpace(model)
			if model != "" && !seen[model] {
				seen[model] = true
				cleaned = append(cleaned, model)
			}
		}
		entry.Fallbacks = cleaned
		c.Models[stage] = entry
	}
	if c.Pricing == nil {
		c.Pricing = map[string]Pricing{}
	}
}

var SeverityOrder = map[string]int{"info": 0, "low": 1, "medium": 2, "high": 3, "critical": 4}

var severityAliases = map[string]string{
	"informational": "info", "note": "info", "minor": "low", "moderate": "medium",
	"warning": "medium", "error": "high", "major": "high", "blocker": "critical",
}

func NormalizeSeverity(value, fallback string) string {
	raw := strings.ToLower(strings.TrimSpace(value))
	if mapped, ok := severityAliases[raw]; ok {
		raw = mapped
	}
	if _, ok := SeverityOrder[raw]; ok {
		return raw
	}
	return fallback
}

func SeverityRank(value string) int {
	return SeverityOrder[NormalizeSeverity(value, "medium")]
}

func FindConfig(startDir, explicit string) (string, error) {
	if explicit != "" {
		resolved := explicit
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(startDir, explicit)
		}
		if !util.FileExists(resolved) {
			return "", &ConfigError{Message: fmt.Sprintf("Config file not found: %s", resolved)}
		}
		return filepath.Clean(resolved), nil
	}
	current, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}
	for {
		for _, name := range ConfigFilenames {
			candidate := filepath.Join(current, name)
			if util.FileExists(candidate) {
				return candidate, nil
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return "", nil
}

func GlobalPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "forge", "config.json")
}

type LoadOptions struct {
	ConfigFile     string
	RequireGoal    bool
	MaxIterations  *int
	BudgetUSD      *float64
	MaxMinutes     *float64
	ModelOverrides map[string]string
}

type Loaded struct {
	Config      Config
	File        string
	GlobalFile  string
	Fingerprint string
	Workspace   string
}

func Load(startDir string, options LoadOptions) (*Loaded, error) {
	file, err := FindConfig(startDir, options.ConfigFile)
	if err != nil {
		return nil, err
	}
	cfg := Default()
	globalFile := GlobalPath()
	if util.FileExists(globalFile) {
		if err := util.ReadJSON(globalFile, &cfg); err != nil {
			return nil, &ConfigError{Message: fmt.Sprintf("Could not parse config JSON: %s", globalFile)}
		}
	}
	if file != "" {
		if err := util.ReadJSON(file, &cfg); err != nil {
			return nil, &ConfigError{Message: fmt.Sprintf("Could not parse config JSON: %s", file)}
		}
	}
	Normalize(&cfg)
	if options.MaxIterations != nil {
		cfg.Budget.MaxIterations = *options.MaxIterations
	}
	if options.BudgetUSD != nil {
		cfg.Budget.MaxUSD = *options.BudgetUSD
	}
	if options.MaxMinutes != nil {
		cfg.Budget.MaxMinutes = *options.MaxMinutes
	}
	for stage, model := range options.ModelOverrides {
		if entry, ok := cfg.Models[stage]; ok {
			entry.Model = model
			cfg.Models[stage] = entry
		}
	}
	ApplyEnvOverrides(&cfg)
	if err := Validate(&cfg, options.RequireGoal); err != nil {
		return nil, err
	}
	workspace, err := filepath.Abs(startDir)
	if err != nil {
		return nil, err
	}
	return &Loaded{Config: cfg, File: file, GlobalFile: globalFile, Fingerprint: Fingerprint(cfg), Workspace: workspace}, nil
}

func ApplyEnvOverrides(c *Config) {
	for stage, entry := range c.Models {
		envName := "FORGE_MODEL_" + strings.ToUpper(stage)
		if value := os.Getenv(envName); value != "" {
			entry.Model = value
			c.Models[stage] = entry
		}
	}
	if value := os.Getenv("FORGE_MAX_ITERATIONS"); value != "" {
		fmt.Sscanf(value, "%d", &c.Budget.MaxIterations)
	}
	if value := os.Getenv("FORGE_BUDGET_USD"); value != "" {
		fmt.Sscanf(value, "%f", &c.Budget.MaxUSD)
	}
	if value := os.Getenv("FORGE_MAX_MINUTES"); value != "" {
		fmt.Sscanf(value, "%f", &c.Budget.MaxMinutes)
	}
}

func Validate(c *Config, requireGoal bool) error {
	var issues []string
	if requireGoal && c.Goal == "" {
		issues = append(issues, "`goal` is required. Run `forge init <dir> --goal \"...\"` or set it in forge.config.json.")
	}
	if c.Acceptance.Coverage.Min < 0 || c.Acceptance.Coverage.Min > 100 {
		issues = append(issues, "`acceptance.coverage.min` must be between 0 and 100.")
	}
	if _, ok := SeverityOrder[c.Acceptance.Security.MaxSeverity]; !ok {
		issues = append(issues, fmt.Sprintf("Unknown security severity: %s", c.Acceptance.Security.MaxSeverity))
	}
	names := make([]string, 0, len(c.Models))
	for stage := range c.Models {
		names = append(names, stage)
	}
	sort.Strings(names)
	for _, stage := range names {
		entry := c.Models[stage]
		if entry.Model == "" || !strings.Contains(entry.Model, "/") {
			issues = append(issues, fmt.Sprintf("models.%s.model must look like \"provider/model\" (got %q).", stage, entry.Model))
		}
	}
	for name, spec := range c.Commands.All() {
		if spec != nil && spec.Command == "" {
			issues = append(issues, fmt.Sprintf("commands.%s.command is required when using an object.", name))
		}
	}
	if c.Budget.MaxUSD < 0 || c.Budget.MaxMinutes < 0 || c.Budget.MaxIterations < 0 || c.Budget.MaxTokens < 0 {
		issues = append(issues, "budget values must be non-negative numbers.")
	}
	if len(issues) > 0 {
		return &ConfigError{Message: "Invalid Forge config:\n- " + strings.Join(issues, "\n- "), Issues: issues}
	}
	return nil
}

func Fingerprint(c Config) string {
	return util.HashObject(struct {
		Acceptance Acceptance            `json:"acceptance"`
		Commands   Commands              `json:"commands"`
		Models     map[string]ModelEntry `json:"models"`
		Budget     Budget                `json:"budget"`
		Loop       Loop                  `json:"loop"`
		Release    Release               `json:"release"`
	}{c.Acceptance, c.Commands, c.Models, c.Budget, c.Loop, c.Release})
}

// FingerprintFile hashes the integrity-sensitive fields of the raw config file
// so that in-run edits can be detected even if they are later normalized away.
func FingerprintFile(file string) (string, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	var raw struct {
		Acceptance json.RawMessage `json:"acceptance"`
		Commands   json.RawMessage `json:"commands"`
		Models     json.RawMessage `json:"models"`
		Budget     json.RawMessage `json:"budget"`
		Loop       json.RawMessage `json:"loop"`
		Release    json.RawMessage `json:"release"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", err
	}
	fields := map[string]json.RawMessage{
		"acceptance": raw.Acceptance,
		"commands":   raw.Commands,
		"models":     raw.Models,
		"budget":     raw.Budget,
		"loop":       raw.Loop,
		"release":    raw.Release,
	}
	for key, value := range fields {
		if len(value) == 0 {
			fields[key] = json.RawMessage("{}")
		}
	}
	return util.HashObject(fields), nil
}

func WriteFile(file string, c Config) error {
	Normalize(&c)
	c.Schema = "./.forge/forge.schema.json"
	return util.WriteJSON(file, c)
}

func (c Config) Command(name string) *Command {
	return c.Commands.All()[name]
}

func (c Config) ModelCandidates(stage, override string) []string {
	entry, ok := c.Models[stage]
	if !ok {
		return nil
	}
	primary := entry.Model
	if override != "" {
		primary = override
	}
	out := []string{primary}
	seen := map[string]bool{primary: true}
	for _, model := range entry.Fallbacks {
		if model != "" && !seen[model] {
			seen[model] = true
			out = append(out, model)
		}
	}
	return out
}

func (c Config) PricingFor(model string) Pricing {
	base := strings.Split(model, "#")[0]
	if pricing, ok := c.Pricing[model]; ok {
		return pricing
	}
	if pricing, ok := c.Pricing[base]; ok {
		return pricing
	}
	for _, entry := range c.Models {
		if strings.Split(entry.Model, "#")[0] == base && entry.Pricing != nil {
			return *entry.Pricing
		}
	}
	return Pricing{}
}

var ErrUnknownStage = errors.New("unknown stage")
