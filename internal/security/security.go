package security

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/soub4i/forge/internal/config"
	"github.com/soub4i/forge/internal/util"
)

type Finding struct {
	Source         string `json:"source,omitempty"`
	Severity       string `json:"severity"`
	Title          string `json:"title"`
	File           string `json:"file,omitempty"`
	Line           int    `json:"line,omitempty"`
	Recommendation string `json:"recommendation,omitempty"`
	Evidence       string `json:"evidence,omitempty"`
	Blocking       bool   `json:"blocking,omitempty"`
}

type SecretResult struct {
	OK           bool      `json:"ok"`
	Findings     []Finding `json:"findings"`
	ScannedFiles int       `json:"scannedFiles"`
}

type ScannerResult struct {
	OK       bool      `json:"ok"`
	Findings []Finding `json:"findings"`
	Scanners []string  `json:"scanners"`
	Warnings []string  `json:"warnings"`
}

type Report struct {
	Missing  bool      `json:"missing"`
	Verdict  string    `json:"verdict"`
	Summary  string    `json:"summary"`
	Findings []Finding `json:"findings"`
}

type Gate struct {
	OK         bool      `json:"ok"`
	Findings   []Finding `json:"findings"`
	Blocking   []Finding `json:"blocking"`
	MaxAllowed string    `json:"maxAllowed"`
	Report     *Report   `json:"report,omitempty"`
}

type pattern struct {
	ID          string
	Severity    string
	Description string
	Regex       *regexp.Regexp
	ValueGroup  int
	Allow       func(string) bool
}

var secretPatterns = []pattern{
	{
		ID: "aws-access-key", Severity: "critical", Description: "AWS access key id",
		Regex: regexp.MustCompile(`\b(AKIA[0-9A-Z]{16})\b`), ValueGroup: 1,
	},
	{
		ID: "github-token", Severity: "high", Description: "GitHub token",
		Regex: regexp.MustCompile(`\b(gh[pousr]_[A-Za-z0-9]{36,255})\b`), ValueGroup: 1,
	},
	{
		ID: "private-key", Severity: "critical", Description: "Private key block",
		Regex: regexp.MustCompile(`-----BEGIN (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----`),
	},
	{
		ID: "generic-secret", Severity: "high", Description: "Hard-coded credential",
		Regex:      regexp.MustCompile(`(?i)(?:api[_-]?key|secret|token|password|passwd|pwd)\s*[:=]\s*["'` + "`" + `]([^"'` + "`" + `\s]{12,})["'` + "`" + `]`),
		ValueGroup: 1,
		Allow: func(value string) bool {
			lower := strings.ToLower(value)
			for _, allowed := range []string{"process.env", "os.environ", "import.meta.env", "example", "sample", "placeholder", "changeme", "your_", "your-", "dummy", "fake", "test", "xxxx", "redacted", "${", "$"} {
				if strings.Contains(lower, allowed) {
					return true
				}
			}
			if strings.HasPrefix(value, "$") || strings.HasPrefix(value, "<") {
				return true
			}
			return false
		},
	},
}

func ScanSecrets(cwd string, cfg config.Config) SecretResult {
	ignore := cfg.Acceptance.Security.Ignore
	files, _ := util.WalkFiles(cwd, 5000, 10, 1024*1024)
	var findings []Finding
	seen := map[string]bool{}
	for _, file := range files {
		if matchesIgnore(file.Rel, ignore) {
			continue
		}
		base := path.Base(file.Rel)
		if base == ".env" || (strings.HasPrefix(base, ".env.") && base != ".env.example" && base != ".env.sample" && base != ".env.template") {
			if !isGitIgnored(cwd, file.Rel) {
				findings = append(findings, Finding{
					Source: "secret-scan", Severity: "high", Title: "Environment file may be committed: " + file.Rel,
					File: file.Rel, Line: 1,
					Recommendation: "Remove the file from the repository, rotate any real credentials, and add it to .gitignore.",
				})
			}
			continue
		}
		if util.IsBinaryPath(file.Rel) {
			continue
		}
		text := util.ReadText(filepath.Join(cwd, file.Rel))
		if text == "" {
			continue
		}
		for _, pattern := range secretPatterns {
			matches := pattern.Regex.FindAllStringSubmatchIndex(text, -1)
			for _, match := range matches {
				value := text[match[0]:match[1]]
				if pattern.ValueGroup > 0 && len(match) > pattern.ValueGroup*2+1 {
					value = text[match[pattern.ValueGroup*2]:match[pattern.ValueGroup*2+1]]
				}
				if pattern.Allow != nil && pattern.Allow(value) {
					continue
				}
				if strings.Contains(lineContaining(text, match[0]), "forge:allow-secret") {
					continue
				}
				line := 1 + strings.Count(text[:match[0]], "\n")
				key := fmt.Sprintf("%s:%s:%d", pattern.ID, file.Rel, line)
				if seen[key] {
					continue
				}
				seen[key] = true
				findings = append(findings, Finding{
					Source: "secret-scan", Severity: pattern.Severity,
					Title: fmt.Sprintf("%s in %s:%d", pattern.Description, file.Rel, line),
					File:  file.Rel, Line: line,
					Recommendation: "Remove the credential, rotate it, and load secrets from environment variables or a secret manager.",
					Evidence:       redact(value),
				})
			}
		}
	}
	return SecretResult{OK: len(findings) == 0, Findings: findings, ScannedFiles: len(files)}
}

func redact(value string) string {
	if len(value) <= 8 {
		return "***"
	}
	return value[:4] + "…" + value[len(value)-2:]
}

// lineContaining returns the line around a byte offset without its newline.
func lineContaining(text string, index int) string {
	start := strings.LastIndex(text[:index], "\n") + 1
	end := strings.Index(text[index:], "\n")
	if end < 0 {
		return text[start:]
	}
	return text[start : index+end]
}

func matchesIgnore(rel string, patterns []string) bool {
	rel = filepath.ToSlash(rel)
	for _, item := range patterns {
		item = strings.TrimSpace(filepath.ToSlash(item))
		if item == "" {
			continue
		}
		if strings.HasSuffix(item, "/**") && strings.HasPrefix(rel, strings.TrimSuffix(item, "**")) {
			return true
		}
		if matched, _ := path.Match(item, rel); matched {
			return true
		}
		if matched, _ := path.Match(item, path.Base(rel)); matched {
			return true
		}
	}
	return false
}

func isGitIgnored(cwd, rel string) bool {
	result := util.RunCommand(context.Background(), "git check-ignore -q "+shellQuote(rel), cwd, 10*time.Second)
	return result.OK
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func Normalize(finding Finding, source string) Finding {
	if finding.Source == "" {
		finding.Source = source
	}
	finding.Severity = config.NormalizeSeverity(finding.Severity, "medium")
	if finding.Title == "" {
		finding.Title = "Security finding"
	}
	if finding.Recommendation == "" {
		finding.Recommendation = "Review and remediate."
	}
	return finding
}

func Merge(groups ...[]Finding) []Finding {
	var out []Finding
	seen := map[string]bool{}
	for _, group := range groups {
		for _, finding := range group {
			normalized := Normalize(finding, "security")
			key := fmt.Sprintf("%s:%s:%s:%d", normalized.Source, normalized.Title, normalized.File, normalized.Line)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, normalized)
		}
	}
	return out
}

func EvaluateGate(cfg config.Config, findings []Finding, report *Report) Gate {
	maxAllowed := config.NormalizeSeverity(cfg.Acceptance.Security.MaxSeverity, "medium")
	merged := Merge(findings, reportFindings(report))
	var blocking []Finding
	for _, finding := range merged {
		if config.SeverityRank(finding.Severity) > config.SeverityRank(maxAllowed) {
			blocking = append(blocking, finding)
		}
	}
	if report != nil && report.Missing {
		blocking = append(blocking, Finding{
			Source: "security-review", Severity: "high",
			Title:          "Security review did not produce a valid .forge/security.json report",
			File:           ".forge/security.json",
			Recommendation: "Re-run the security stage and ensure it writes the required JSON report.",
		})
	} else if report != nil && strings.EqualFold(report.Verdict, "fail") && len(blocking) == 0 {
		blocking = append(blocking, Finding{
			Source: "security-review", Severity: "high",
			Title:          "Security reviewer returned verdict=fail without specific findings",
			File:           ".forge/security.json",
			Recommendation: "Ask the reviewer for concrete, actionable findings.",
		})
	}
	return Gate{OK: len(blocking) == 0, Findings: merged, Blocking: blocking, MaxAllowed: maxAllowed, Report: report}
}

func reportFindings(report *Report) []Finding {
	if report == nil {
		return nil
	}
	return report.Findings
}

func FormatFindings(findings []Finding, maxItems int) string {
	if len(findings) == 0 {
		return "No security findings."
	}
	if maxItems <= 0 || maxItems > len(findings) {
		maxItems = len(findings)
	}
	var lines []string
	for index, finding := range findings[:maxItems] {
		location := ""
		if finding.File != "" {
			location = " (" + finding.File
			if finding.Line > 0 {
				location += fmt.Sprintf(":%d", finding.Line)
			}
			location += ")"
		}
		lines = append(lines, fmt.Sprintf("%d. [%s] %s%s\n   %s", index+1, strings.ToUpper(finding.Severity), finding.Title, location, finding.Recommendation))
	}
	return strings.Join(lines, "\n")
}

func AutoScanners(cwd string) []config.Scanner {
	var scanners []config.Scanner
	if util.FileExists(filepath.Join(cwd, "package-lock.json")) && util.Which("npm") {
		scanners = append(scanners, config.Scanner{Name: "npm-audit", Command: "npm audit --omit=dev --json", Format: "npm-audit"})
	}
	if (util.FileExists(filepath.Join(cwd, "requirements.txt")) || util.FileExists(filepath.Join(cwd, "pyproject.toml"))) && util.Which("pip-audit") {
		scanners = append(scanners, config.Scanner{Name: "pip-audit", Command: "pip-audit -f json", Format: "pip-audit"})
	}
	if util.FileExists(filepath.Join(cwd, "go.mod")) && util.Which("govulncheck") {
		scanners = append(scanners, config.Scanner{Name: "govulncheck", Command: "govulncheck -json ./...", Format: "govulncheck"})
	}
	if util.FileExists(filepath.Join(cwd, "Cargo.lock")) && util.Which("cargo-audit") {
		scanners = append(scanners, config.Scanner{Name: "cargo-audit", Command: "cargo audit --json", Format: "json"})
	}
	return scanners
}

func RunScanners(ctx context.Context, cfg config.Config, cwd, runDir string) ScannerResult {
	scanners := append([]config.Scanner{}, AutoScanners(cwd)...)
	scanners = append(scanners, cfg.Acceptance.Security.Scanners...)
	result := ScannerResult{OK: true}
	for _, scanner := range scanners {
		if scanner.Command == "" {
			continue
		}
		name := scanner.Name
		if name == "" {
			name = strings.Fields(scanner.Command)[0]
		}
		format := scanner.Format
		if format == "" {
			format = inferFormat(name, scanner.Command)
		}
		timeout := 180 * time.Second
		if scanner.TimeoutMs > 0 {
			timeout = time.Duration(scanner.TimeoutMs) * time.Millisecond
		}
		run := util.RunCommand(ctx, scanner.Command, cwd, timeout)
		result.Scanners = append(result.Scanners, name)
		logFile := filepath.Join(runDir, "security", name+".log")
		_ = util.WriteText(logFile, fmt.Sprintf("$ %s\nexit: %d\nduration: %s\n\n--- stdout ---\n%s\n--- stderr ---\n%s", scanner.Command, run.Code, util.FormatDuration(run.Duration.Milliseconds()), run.Stdout, run.Stderr))
		findings := ParseOutput(format, run.Stdout+"\n"+run.Stderr)
		result.Findings = append(result.Findings, findings...)
		if !run.OK && scanner.Required {
			result.OK = false
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s failed with exit code %d", name, run.Code))
		} else if !run.OK && len(findings) == 0 {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s exited with code %d; no findings parsed", name, run.Code))
		}
	}
	if len(result.Findings) > 0 {
		result.OK = false
	}
	return result
}

func inferFormat(name, command string) string {
	haystack := strings.ToLower(name + " " + command)
	switch {
	case strings.Contains(haystack, "npm"):
		return "npm-audit"
	case strings.Contains(haystack, "pip-audit") || strings.Contains(haystack, "pip audit"):
		return "pip-audit"
	case strings.Contains(haystack, "semgrep"):
		return "semgrep"
	case strings.Contains(haystack, "trivy"):
		return "trivy"
	case strings.Contains(haystack, "gitleaks"):
		return "gitleaks"
	case strings.Contains(haystack, "govulncheck"):
		return "govulncheck"
	case strings.Contains(haystack, "--json") || strings.Contains(haystack, "-f json"):
		return "json"
	}
	return "text"
}

func ParseOutput(format, text string) []Finding {
	switch format {
	case "npm-audit":
		return parseNpmAudit(text)
	case "pip-audit":
		return parsePipAudit(text)
	case "semgrep":
		return parseSemgrep(text)
	case "trivy":
		return parseTrivy(text)
	case "gitleaks":
		return parseGitleaks(text)
	case "govulncheck":
		return parseGovulncheck(text)
	case "json":
		return parseGenericJSON(text)
	case "text":
		return parseText(text)
	}
	return nil
}

func parseNpmAudit(text string) []Finding {
	value := util.ExtractJSON(text)
	root, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	vulnerabilities, _ := root["vulnerabilities"].(map[string]any)
	var out []Finding
	for name, item := range vulnerabilities {
		vuln, _ := item.(map[string]any)
		severity := config.NormalizeSeverity(stringValue(vuln["severity"]), "medium")
		via := ""
		if list, ok := vuln["via"].([]any); ok && len(list) > 0 {
			if object, ok := list[0].(map[string]any); ok {
				via = stringValue(object["title"])
			} else {
				via = stringValue(list[0])
			}
		}
		title := "Vulnerable dependency: " + name
		if via != "" {
			title += " — " + via
		}
		out = append(out, Normalize(Finding{
			Severity: severity, Title: title, File: "package-lock.json",
			Recommendation: "Update " + name + " to a patched version.",
		}, "npm-audit"))
	}
	return out
}

func parsePipAudit(text string) []Finding {
	value := util.ExtractJSON(text)
	list, _ := value.([]any)
	var out []Finding
	for _, item := range list {
		object, _ := item.(map[string]any)
		vulns, _ := object["vulns"].([]any)
		for _, vuln := range vulns {
			v, _ := vuln.(map[string]any)
			out = append(out, Normalize(Finding{
				Severity: "high", Title: "Python advisory: " + stringValue(v["id"]),
				File: stringValue(object["name"]), Recommendation: "Upgrade the package.",
			}, "pip-audit"))
		}
	}
	return out
}

func parseSemgrep(text string) []Finding {
	value := util.ExtractJSON(text)
	root, _ := value.(map[string]any)
	results, _ := root["results"].([]any)
	var out []Finding
	for _, item := range results {
		object, _ := item.(map[string]any)
		extra, _ := object["extra"].(map[string]any)
		start, _ := object["start"].(map[string]any)
		out = append(out, Normalize(Finding{
			Severity: stringValue(extra["severity"]), Title: stringValue(extra["message"]),
			File: stringValue(object["path"]), Line: intValue(start["line"]),
			Recommendation: "Review the rule and remediate the issue.",
		}, "semgrep"))
	}
	return out
}

func parseTrivy(text string) []Finding {
	value := util.ExtractJSON(text)
	root, _ := value.(map[string]any)
	results, _ := root["Results"].([]any)
	var out []Finding
	for _, item := range results {
		object, _ := item.(map[string]any)
		vulns, _ := object["Vulnerabilities"].([]any)
		for _, vuln := range vulns {
			v, _ := vuln.(map[string]any)
			out = append(out, Normalize(Finding{
				Severity: stringValue(v["Severity"]), Title: "Vulnerability " + stringValue(v["VulnerabilityID"]) + " in " + stringValue(v["PkgName"]),
				File: stringValue(object["Target"]), Recommendation: "Upgrade the dependency.",
			}, "trivy"))
		}
	}
	return out
}

func parseGitleaks(text string) []Finding {
	value := util.ExtractJSON(text)
	list, _ := value.([]any)
	var out []Finding
	for _, item := range list {
		object, _ := item.(map[string]any)
		out = append(out, Normalize(Finding{
			Severity: "high", Title: stringValue(object["Description"]),
			File: stringValue(object["File"]), Line: intValue(object["StartLine"]),
			Evidence:       redact(stringValue(object["Secret"])),
			Recommendation: "Remove the secret, rotate it, and load it from a secret manager.",
		}, "gitleaks"))
	}
	return out
}

func parseGovulncheck(text string) []Finding {
	var out []Finding
	for _, line := range strings.Split(text, "\n") {
		value := util.ExtractJSON(line)
		object, _ := value.(map[string]any)
		finding, _ := object["finding"].(map[string]any)
		if finding == nil {
			continue
		}
		out = append(out, Normalize(Finding{
			Severity: "high", Title: "Go vulnerability: " + stringValue(finding["osv"]),
			File: "go.mod", Recommendation: "Review the advisory and update dependencies.",
		}, "govulncheck"))
	}
	return out
}

func parseGenericJSON(text string) []Finding {
	value := util.ExtractJSON(text)
	var out []Finding
	var walk func(any)
	walk = func(item any) {
		switch typed := item.(type) {
		case []any:
			for _, child := range typed {
				walk(child)
			}
		case map[string]any:
			severity := stringValue(typed["severity"])
			title := stringValue(typed["title"])
			if title == "" {
				title = stringValue(typed["message"])
			}
			if severity != "" && title != "" {
				out = append(out, Normalize(Finding{
					Severity: severity, Title: title, File: stringValue(typed["file"]),
					Line: intValue(typed["line"]), Recommendation: "Review the finding.",
				}, "scanner"))
			}
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(value)
	return out
}

func parseText(text string) []Finding {
	var out []Finding
	for _, line := range strings.Split(text, "\n") {
		lower := strings.ToLower(line)
		for _, severity := range []string{"critical", "high", "medium", "low"} {
			if strings.Contains(lower, severity) && (strings.Contains(lower, "vulnerab") || strings.Contains(lower, "secret") || strings.Contains(lower, "finding")) {
				out = append(out, Normalize(Finding{Severity: severity, Title: strings.TrimSpace(line), Recommendation: "Review the finding."}, "scanner"))
				break
			}
		}
	}
	return out
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func intValue(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case json.Number:
		number, _ := typed.Int64()
		return int(number)
	}
	return 0
}

func SortFindings(findings []Finding) {
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Severity != findings[j].Severity {
			return config.SeverityRank(findings[i].Severity) > config.SeverityRank(findings[j].Severity)
		}
		return findings[i].Title < findings[j].Title
	})
}
