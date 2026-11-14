package coverage

import (
	"encoding/json"
	"math"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/soub4i/forge/internal/config"
	"github.com/soub4i/forge/internal/util"
)

var textPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)total:\s*\(statements\)\s*([\d.]+)%`),
	regexp.MustCompile(`(?i)all files\s*\|\s*([\d.]+)`),
	regexp.MustCompile(`(?i)coverage:\s*([\d.]+)%`),
	regexp.MustCompile(`(?i)TOTAL\s+\d+\s+\d+\s+([\d.]+)%`),
	regexp.MustCompile(`(?i)total\s+([\d.]+)%`),
	regexp.MustCompile(`(?i)lines?\s*[.:]?\s*([\d.]+)%`),
	regexp.MustCompile(`(?i)([\d.]+)%\s*(?:coverage|lines covered)`),
}

func ExtractFromJSON(value any) *float64 {
	switch typed := value.(type) {
	case map[string]any:
		for _, path := range [][]string{
			{"total", "lines", "pct"}, {"total", "statements", "pct"},
			{"totals", "percent_covered"}, {"percent_covered"}, {"percentCovered"},
			{"coverage"}, {"total", "percent"}, {"lines", "pct"},
		} {
			if number := lookupNumber(typed, path); number != nil {
				return number
			}
		}
		for _, item := range typed {
			if nested := ExtractFromJSON(item); nested != nil {
				return nested
			}
		}
	case []any:
		for _, item := range typed {
			if nested := ExtractFromJSON(item); nested != nil {
				return nested
			}
		}
	}
	return nil
}

func lookupNumber(value map[string]any, path []string) *float64 {
	var current any = value
	for _, key := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current, ok = object[key]
		if !ok {
			return nil
		}
	}
	if number, ok := numberValue(current); ok {
		return &number
	}
	return nil
}

func numberValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case json.Number:
		number, err := typed.Float64()
		return number, err == nil
	case string:
		number, err := strconv.ParseFloat(strings.TrimSuffix(typed, "%"), 64)
		return number, err == nil
	}
	return 0, false
}

func ExtractFromText(text string) *float64 {
	for _, pattern := range textPatterns {
		match := pattern.FindStringSubmatch(text)
		if len(match) < 2 {
			continue
		}
		number, err := strconv.ParseFloat(match[1], 64)
		if err == nil && !math.IsNaN(number) {
			return &number
		}
	}
	return nil
}

func Extract(cwd string, cfg config.Config, stdout string) (*float64, string) {
	if cfg.Acceptance.Coverage.File != nil && *cfg.Acceptance.Coverage.File != "" {
		file := *cfg.Acceptance.Coverage.File
		full := file
		if !filepath.IsAbs(full) {
			full = filepath.Join(cwd, file)
		}
		if util.FileExists(full) {
			if value := util.ExtractJSON(util.ReadText(full)); value != nil {
				if percent := ExtractFromJSON(value); percent != nil {
					return percent, file
				}
			}
		}
	}
	for _, candidate := range []string{"coverage/coverage-summary.json", "coverage.json", "coverage/coverage-final.json"} {
		full := filepath.Join(cwd, candidate)
		if !util.FileExists(full) {
			continue
		}
		if value := util.ExtractJSON(util.ReadText(full)); value != nil {
			if percent := ExtractFromJSON(value); percent != nil {
				return percent, candidate
			}
		}
	}
	if percent := ExtractFromText(stdout); percent != nil {
		return percent, "stdout"
	}
	if util.FileExists(filepath.Join(cwd, "coverage.out")) {
		if percent := ExtractFromText(util.ReadText(filepath.Join(cwd, "coverage.out"))); percent != nil {
			return percent, "coverage.out"
		}
	}
	return nil, ""
}
