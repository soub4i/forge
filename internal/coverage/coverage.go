package coverage

import (
	"encoding/json"
	"encoding/xml"
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
	regexp.MustCompile(`(?i)\[\s*total\s*\]\s*([\d.]+)%`),
	regexp.MustCompile(`(?i)statement coverage[^\d]*([\d.]+)%`),
	regexp.MustCompile(`(?i)total\s+([\d.]+)%`),
	regexp.MustCompile(`(?i)lines?\s*(?:coverage)?\s*[.:]?\s*([\d.]+)%`),
	regexp.MustCompile(`(?i)total coverage:\s*([\d.]+)%`),
	regexp.MustCompile(`(?i)\(([\d.]+)%\)\s*covered`),
	regexp.MustCompile(`(?i)([\d.]+)%\s*(?:coverage|lines covered|covered)`),
}

var coverageCandidates = []string{
	"coverage/coverage-summary.json", "coverage.json", "coverage/coverage-final.json",
	"coverage/lcov.info", "lcov.info", "coverage.xml", "coverage/cobertura-coverage.xml",
	"coverage/coverage.cobertura.xml", "target/site/jacoco/jacoco.xml",
}

var discoveredCoverageNames = map[string]bool{
	"coverage.cobertura.xml": true, "cobertura-coverage.xml": true,
	"coverage.xml": true, "jacoco.xml": true, "lcov.info": true,
}

type jacocoReport struct {
	Counters []struct {
		Type    string `xml:"type,attr"`
		Missed  int    `xml:"missed,attr"`
		Covered int    `xml:"covered,attr"`
	} `xml:"counter"`
}

var lineRatePattern = regexp.MustCompile(`(?i)line-rate="([\d.]+)"`)

func extractCobertura(text string) *float64 {
	if match := lineRatePattern.FindStringSubmatch(text); len(match) == 2 {
		if rate, err := strconv.ParseFloat(match[1], 64); err == nil {
			value := rate * 100
			return &value
		}
	}
	return nil
}

func extractGoProfile(text string) *float64 {
	total, covered := 0, 0
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		statements, err := strconv.Atoi(fields[len(fields)-2])
		if err != nil || statements <= 0 {
			continue
		}
		count, err := strconv.Atoi(fields[len(fields)-1])
		if err != nil {
			continue
		}
		total += statements
		if count > 0 {
			covered += statements
		}
	}
	if total == 0 {
		return nil
	}
	value := float64(covered) / float64(total) * 100
	return &value
}

func extractJacoco(text string) *float64 {
	var report jacocoReport
	if err := xml.Unmarshal([]byte(text), &report); err != nil {
		return nil
	}
	for index := len(report.Counters) - 1; index >= 0; index-- {
		counter := report.Counters[index]
		if !strings.EqualFold(counter.Type, "LINE") {
			continue
		}
		total := counter.Missed + counter.Covered
		if total > 0 {
			value := float64(counter.Covered) / float64(total) * 100
			return &value
		}
	}
	return nil
}

func extractLCOV(text string) *float64 {
	linesFound, linesHit := 0, 0
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "LF:"):
			if number, err := strconv.Atoi(strings.TrimPrefix(line, "LF:")); err == nil {
				linesFound += number
			}
		case strings.HasPrefix(line, "LH:"):
			if number, err := strconv.Atoi(strings.TrimPrefix(line, "LH:")); err == nil {
				linesHit += number
			}
		}
	}
	if linesFound > 0 {
		value := float64(linesHit) / float64(linesFound) * 100
		return &value
	}
	return nil
}

func extractCoverageFile(full string) (*float64, bool) {
	text := util.ReadText(full)
	if text == "" {
		return nil, false
	}
	if value := util.ExtractJSON(text); value != nil {
		if percent := ExtractFromJSON(value); percent != nil {
			return percent, true
		}
	}
	lower := strings.ToLower(full)
	switch {
	case strings.HasSuffix(lower, ".xml"):
		if percent := extractCobertura(text); percent != nil {
			return percent, true
		}
		if percent := extractJacoco(text); percent != nil {
			return percent, true
		}
	case strings.HasSuffix(lower, ".info"):
		if percent := extractLCOV(text); percent != nil {
			return percent, true
		}
	}
	if percent := ExtractFromText(text); percent != nil {
		return percent, true
	}
	return nil, false
}

func discoveredCoverageFiles(cwd string) []string {
	files, _ := util.WalkFiles(cwd, 4000, 6, 0)
	var out []string
	for _, file := range files {
		if discoveredCoverageNames[strings.ToLower(filepath.Base(file.Rel))] {
			out = append(out, file.Rel)
		}
	}
	return out
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
			if percent, ok := extractCoverageFile(full); ok {
				return percent, file
			}
		}
	}
	for _, candidate := range coverageCandidates {
		full := filepath.Join(cwd, candidate)
		if !util.FileExists(full) {
			continue
		}
		if percent, ok := extractCoverageFile(full); ok {
			return percent, candidate
		}
	}
	for _, candidate := range discoveredCoverageFiles(cwd) {
		if percent, ok := extractCoverageFile(filepath.Join(cwd, candidate)); ok {
			return percent, candidate
		}
	}
	if percent := ExtractFromText(stdout); percent != nil {
		return percent, "stdout"
	}
	if util.FileExists(filepath.Join(cwd, "coverage.out")) {
		text := util.ReadText(filepath.Join(cwd, "coverage.out"))
		if percent := extractGoProfile(text); percent != nil {
			return percent, "coverage.out"
		}
		if percent := ExtractFromText(text); percent != nil {
			return percent, "coverage.out"
		}
	}
	return nil, ""
}
