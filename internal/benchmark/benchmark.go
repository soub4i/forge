package benchmark

import (
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"

	"github.com/soub4i/forge/internal/config"
	"github.com/soub4i/forge/internal/util"
)

type Failure struct {
	Metric   string  `json:"metric"`
	Reason   string  `json:"reason"`
	Value    float64 `json:"value,omitempty"`
	Expected float64 `json:"expected,omitempty"`
	Unit     string  `json:"unit,omitempty"`
	Message  string  `json:"message"`
}

type Result struct {
	OK         bool               `json:"ok"`
	Skipped    bool               `json:"skipped"`
	Required   bool               `json:"required"`
	Metrics    map[string]float64 `json:"metrics"`
	Source     string             `json:"source,omitempty"`
	Failures   []Failure          `json:"failures"`
	Thresholds []config.Threshold `json:"thresholds,omitempty"`
	Command    string             `json:"command,omitempty"`
	CommandOK  bool               `json:"commandOk"`
	DurationMs int64              `json:"durationMs,omitempty"`
	ExitCode   int                `json:"exitCode,omitempty"`
	LogFile    string             `json:"logFile,omitempty"`
	Stdout     string             `json:"stdout,omitempty"`
	Stderr     string             `json:"stderr,omitempty"`
}

func ParseData(value any) map[string]float64 {
	out := map[string]float64{}
	flatten(value, "", out)
	return out
}

func flatten(value any, prefix string, out map[string]float64) {
	switch typed := value.(type) {
	case float64:
		if prefix != "" {
			out[prefix] = typed
		}
	case json.Number:
		if number, err := typed.Float64(); err == nil && prefix != "" {
			out[prefix] = number
		}
	case []any:
		for _, item := range typed {
			object, ok := item.(map[string]any)
			if !ok {
				continue
			}
			name := stringValue(object["name"])
			if name == "" {
				name = stringValue(object["metric"])
			}
			if name == "" {
				name = stringValue(object["key"])
			}
			if name != "" {
				if number, ok := numberValue(object["value"]); ok {
					out[name] = number
				} else if nested, ok := object["metrics"]; ok {
					flatten(nested, name, out)
				} else {
					flatten(object, name, out)
				}
			} else {
				flatten(object, prefix, out)
			}
		}
	case map[string]any:
		if metrics, ok := typed["metrics"]; ok {
			flatten(metrics, prefix, out)
		}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			if key != "metrics" {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		for _, key := range keys {
			item := typed[key]
			next := key
			if prefix != "" {
				next = prefix + "." + key
			}
			if number, ok := numberValue(item); ok {
				out[next] = number
				if prefix == "" {
					if _, exists := out[key]; !exists {
						out[key] = number
					}
				}
				continue
			}
			if object, ok := item.(map[string]any); ok {
				flatten(object, next, out)
				if prefix == "" {
					if number, ok := numberValue(object["value"]); ok {
						out[key] = number
					}
				}
			}
		}
	}
}

func numberValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case json.Number:
		number, err := typed.Float64()
		return number, err == nil
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	}
	return 0, false
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func ReadResults(cwd string, cfg config.Config, stdout string) (map[string]float64, string, any) {
	resultsFile := cfg.Acceptance.Benchmarks.ResultsFile
	if resultsFile == "" {
		resultsFile = "bench/results.json"
	}
	full := resultsFile
	if !filepath.IsAbs(full) {
		full = filepath.Join(cwd, resultsFile)
	}
	if util.FileExists(full) {
		if value := util.ExtractJSON(util.ReadText(full)); value != nil {
			return ParseData(value), resultsFile, value
		}
	}
	if value := util.ExtractJSON(stdout); value != nil {
		return ParseData(value), "stdout", value
	}
	if util.FileExists(full) {
		if value := util.ExtractJSON(util.ReadText(full)); value != nil {
			return ParseData(value), resultsFile, value
		}
	}
	return map[string]float64{}, "", nil
}

func Evaluate(cfg config.Config, cwd, stdout string) Result {
	metrics, source, _ := ReadResults(cwd, cfg, stdout)
	failures := []Failure{}
	for _, threshold := range cfg.Acceptance.Benchmarks.Thresholds {
		value, ok := metrics[threshold.Metric]
		if !ok || math.IsNaN(value) {
			failures = append(failures, Failure{
				Metric: threshold.Metric, Reason: "missing",
				Message: fmt.Sprintf("Benchmark metric %q was not reported (source: %s).", threshold.Metric, valueOr(source, "none")),
			})
			continue
		}
		if threshold.Min != nil && value < *threshold.Min {
			failures = append(failures, Failure{
				Metric: threshold.Metric, Reason: "below_min", Value: value, Expected: *threshold.Min, Unit: threshold.Unit,
				Message: fmt.Sprintf("%s=%g%s is below minimum %g.", threshold.Metric, value, unitSuffix(threshold.Unit), *threshold.Min),
			})
		}
		if threshold.Max != nil && value > *threshold.Max {
			failures = append(failures, Failure{
				Metric: threshold.Metric, Reason: "above_max", Value: value, Expected: *threshold.Max, Unit: threshold.Unit,
				Message: fmt.Sprintf("%s=%g%s exceeds maximum %g.", threshold.Metric, value, unitSuffix(threshold.Unit), *threshold.Max),
			})
		}
	}
	return Result{OK: len(failures) == 0, Metrics: metrics, Source: source, Failures: failures, Thresholds: cfg.Acceptance.Benchmarks.Thresholds}
}

func Format(result Result) string {
	if len(result.Metrics) == 0 {
		return "No benchmark metrics reported."
	}
	keys := make([]string, 0, len(result.Metrics))
	for key := range result.Metrics {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var parts []string
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%g", key, result.Metrics[key]))
	}
	if result.Source != "" {
		return strings.Join(parts, ", ") + " (source: " + result.Source + ")"
	}
	return strings.Join(parts, ", ")
}

func unitSuffix(unit string) string {
	if unit == "" {
		return ""
	}
	return " " + unit
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
