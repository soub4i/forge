package security

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/soub4i/forge/internal/config"
	"github.com/soub4i/forge/internal/util"
)

func writeTempFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanSecretsFindsAwsKey(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "config.js", `const key = "AKIAIOSFODNN7EXAMPLE";`) // forge:allow-secret
	cfg := config.Default()
	result := ScanSecrets(dir, cfg)
	if result.OK {
		t.Fatal("expected a secret finding")
	}
	if len(result.Findings) != 1 {
		t.Fatalf("findings = %d", len(result.Findings))
	}
	if result.Findings[0].Severity != "critical" {
		t.Fatalf("severity = %q", result.Findings[0].Severity)
	}
	if result.Findings[0].Evidence == "" {
		t.Fatal("expected redacted evidence")
	}
	if result.Findings[0].Evidence == "AKIAIOSFODNN7EXAMPLE" { // forge:allow-secret
		t.Fatal("evidence must be redacted")
	}
}

func TestScanSecretsIgnoresPlaceholders(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "example.js", `const apiKey = "your_api_key_here";`)
	cfg := config.Default()
	result := ScanSecrets(dir, cfg)
	if !result.OK {
		t.Fatalf("placeholder should not be reported: %+v", result.Findings)
	}
}

func TestScanSecretsIgnorePatterns(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "vendor/secret.txt", `AKIAIOSFODNN7EXAMPLE`) // forge:allow-secret
	cfg := config.Default()
	cfg.Acceptance.Security.Ignore = []string{"vendor/**"}
	result := ScanSecrets(dir, cfg)
	if !result.OK {
		t.Fatalf("ignored file should not be scanned: %+v", result.Findings)
	}
}

func TestScanSecretsInlineAllowMarker(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "fixture.js", "const key = \"AKIAIOSFODNN7EXAMPLE\"; // forge:allow-secret\n")
	cfg := config.Default()
	result := ScanSecrets(dir, cfg)
	if !result.OK {
		t.Fatalf("inline marker should suppress the finding: %+v", result.Findings)
	}
}

func TestParseNpmAudit(t *testing.T) {
	text := `{
		"vulnerabilities": {
			"lodash": {
				"severity": "high",
				"via": [{"title": "Prototype pollution"}]
			}
		}
	}`
	findings := ParseOutput("npm-audit", text)
	if len(findings) != 1 {
		t.Fatalf("findings = %d", len(findings))
	}
	if findings[0].Source != "npm-audit" || findings[0].Severity != "high" {
		t.Fatalf("finding = %+v", findings[0])
	}
}

func TestEvaluateGateSeverity(t *testing.T) {
	cfg := config.Default()
	low := Normalize(Finding{Severity: "low", Title: "low issue"}, "review")
	high := Normalize(Finding{Severity: "high", Title: "high issue"}, "review")
	gate := EvaluateGate(cfg, []Finding{low, high}, &Report{Missing: false, Verdict: "fail", Findings: []Finding{}})
	if gate.OK {
		t.Fatal("high finding above medium should block")
	}
	if len(gate.Blocking) != 1 || gate.Blocking[0].Title != "high issue" {
		t.Fatalf("blocking = %+v", gate.Blocking)
	}
}

func TestEvaluateGateMissingReport(t *testing.T) {
	cfg := config.Default()
	gate := EvaluateGate(cfg, nil, &Report{Missing: true, Findings: []Finding{}})
	if gate.OK {
		t.Fatal("missing security report must fail the gate")
	}
}

func TestMergeDeduplicates(t *testing.T) {
	a := Finding{Source: "scan", Severity: "high", Title: "same", File: "a.js", Line: 1}
	b := Finding{Source: "scan", Severity: "high", Title: "same", File: "a.js", Line: 1}
	merged := Merge([]Finding{a}, []Finding{b})
	if len(merged) != 1 {
		t.Fatalf("merged = %d", len(merged))
	}
}

func TestScanSecretsVariants(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "keys.js", "const key = \"ghp_"+strings.Repeat("a", 36)+"\";\n")
	writeTempFile(t, dir, "id_rsa", "-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----\n") // forge:allow-secret
	writeTempFile(t, dir, "config.js", `const password = "supersecretvalue123";`)                                    // forge:allow-secret
	writeTempFile(t, dir, "binary.png", "AKIAIOSFODNN7EXAMPLE")                                                      // forge:allow-secret
	cfg := config.Default()
	result := ScanSecrets(dir, cfg)
	titles := []string{}
	for _, finding := range result.Findings {
		titles = append(titles, finding.Title)
	}
	joined := strings.Join(titles, "\n")
	for _, want := range []string{"GitHub token", "Private key block", "Hard-coded credential"} {
		if !strings.Contains(joined, want) {
			t.Errorf("findings are missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "binary.png") {
		t.Fatalf("binary files must be skipped:\n%s", joined)
	}
	for _, finding := range result.Findings {
		if finding.Evidence != "" && strings.Contains(finding.Evidence, "supersecretvalue123") {
			t.Fatalf("evidence was not redacted: %+v", finding)
		}
		if finding.Recommendation == "" || finding.Severity == "" {
			t.Fatalf("finding was not normalized: %+v", finding)
		}
	}
}

func TestScanSecretsDotEnv(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, ".env", "AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE\n") // forge:allow-secret
	writeTempFile(t, dir, ".env.example", "AWS_ACCESS_KEY_ID=your_key_here\n")
	cfg := config.Default()
	result := ScanSecrets(dir, cfg)
	foundEnv := false
	for _, finding := range result.Findings {
		if finding.Title == "Environment file may be committed: .env" {
			foundEnv = true
		}
		if strings.Contains(finding.File, ".env.example") {
			t.Fatalf(".env.example must be allowed: %+v", finding)
		}
	}
	if !foundEnv {
		t.Fatalf("missing .env finding: %+v", result.Findings)
	}
}

func TestParsePipAudit(t *testing.T) {
	text := `[{"name":"requests","vulns":[{"id":"PYSEC-1"},{"id":"PYSEC-2"}]}]`
	findings := ParseOutput("pip-audit", text)
	if len(findings) != 2 || findings[0].File != "requests" || findings[0].Severity != "high" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestParseSemgrep(t *testing.T) {
	text := `{"results":[{"check_id":"rule","path":"src/a.js","start":{"line":12},"extra":{"severity":"ERROR","message":"unsafe eval"}}]}`
	findings := ParseOutput("semgrep", text)
	if len(findings) != 1 {
		t.Fatalf("findings = %+v", findings)
	}
	finding := findings[0]
	if finding.Severity != "high" || finding.Title != "unsafe eval" || finding.File != "src/a.js" || finding.Line != 12 {
		t.Fatalf("finding = %+v", finding)
	}
	if finding.Source != "semgrep" {
		t.Fatalf("source = %q", finding.Source)
	}
}

func TestParseTrivy(t *testing.T) {
	text := `{"Results":[{"Target":"package-lock.json","Vulnerabilities":[{"VulnerabilityID":"CVE-1","PkgName":"lodash","Severity":"CRITICAL"}]}]}`
	findings := ParseOutput("trivy", text)
	if len(findings) != 1 || findings[0].Severity != "critical" || !strings.Contains(findings[0].Title, "CVE-1") {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestParseGitleaks(t *testing.T) {
	text := `[{"Description":"AWS key","File":"src/a.js","StartLine":3,"Secret":"AKIAIOSFODNN7EXAMPLE"}]` // forge:allow-secret
	findings := ParseOutput("gitleaks", text)
	if len(findings) != 1 || findings[0].File != "src/a.js" || findings[0].Line != 3 {
		t.Fatalf("findings = %+v", findings)
	}
	if strings.Contains(findings[0].Evidence, "AKIAIOSFODNN7EXAMPLE") { // forge:allow-secret
		t.Fatalf("gitleaks evidence was not redacted: %+v", findings[0])
	}
}

func TestParseGovulncheck(t *testing.T) {
	text := `{"finding":{"osv":"GO-2026-0001"}}
noise
{"finding":{"osv":"GO-2026-0002"}}`
	findings := ParseOutput("govulncheck", text)
	if len(findings) != 2 || !strings.Contains(findings[0].Title, "GO-2026-0001") {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestParsePnpmAudit(t *testing.T) {
	text := `{"advisories":{"1":{"module_name":"lodash","severity":"high","title":"prototype pollution"}}}`
	findings := ParseOutput("pnpm-audit", text)
	if len(findings) != 1 || findings[0].Severity != "high" || !strings.Contains(findings[0].Title, "lodash") {
		t.Fatalf("findings = %+v", findings)
	}
	if findings[0].File != "pnpm-lock.yaml" {
		t.Fatalf("file = %q", findings[0].File)
	}
}

func TestParseYarnAudit(t *testing.T) {
	text := `{"type":"auditAdvisory","data":{"advisory":{"module_name":"minimist","severity":"critical","title":"prototype pollution"}}}`
	findings := ParseOutput("yarn-audit", text)
	if len(findings) != 1 || findings[0].Severity != "critical" || !strings.Contains(findings[0].Title, "minimist") {
		t.Fatalf("findings = %+v", findings)
	}
	if findings[0].File != "yarn.lock" {
		t.Fatalf("file = %q", findings[0].File)
	}
}

func TestParseComposerAudit(t *testing.T) {
	text := `{"advisories":{"vendor/pkg":[{"severity":"medium","title":"unsafe deserialization"}]}}`
	findings := ParseOutput("composer-audit", text)
	if len(findings) != 1 || findings[0].Severity != "medium" || !strings.Contains(findings[0].Title, "vendor/pkg") {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestParseBundlerAudit(t *testing.T) {
	text := `{"results":[{"gem":{"name":"rack"},"advisory":{"severity":"high","title":"header injection"}}]}`
	findings := ParseOutput("bundler-audit", text)
	if len(findings) != 1 || findings[0].Severity != "high" || !strings.Contains(findings[0].Title, "rack") {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestParseDotnetVulnerable(t *testing.T) {
	text := `The following packages have vulnerabilities:
   [net8.0]:
   Top-level Package      Requested   Resolved   Latest
   > Newtonsoft.Json      12.0.1      12.0.1     13.0.3   High
   > System.Text.Json     7.0.0       7.0.0      8.0.4    Moderate
`
	findings := ParseOutput("dotnet-vulnerable", text)
	if len(findings) != 2 {
		t.Fatalf("findings = %+v", findings)
	}
	if findings[0].Severity != "high" || !strings.Contains(findings[0].Title, "Newtonsoft.Json") {
		t.Fatalf("first finding = %+v", findings[0])
	}
	if findings[1].Severity != "medium" {
		t.Fatalf("moderate should normalize to medium: %+v", findings[1])
	}
}

func TestParseOSV(t *testing.T) {
	text := `{"results":[{"source":{"path":"package-lock.json"},"packages":[{"package":{"name":"lodash"},"vulnerabilities":[{"id":"GHSA-1","summary":"prototype pollution"}]}]}]}`
	findings := ParseOutput("osv", text)
	if len(findings) != 1 || !strings.Contains(findings[0].Title, "GHSA-1") || findings[0].File != "package-lock.json" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestParseGenericJSON(t *testing.T) {
	text := `{"issues":[{"severity":"medium","message":"bad pattern","file":"a.js","line":4}]}`
	findings := ParseOutput("json", text)
	if len(findings) != 1 || findings[0].Title != "bad pattern" || findings[0].Line != 4 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestParseTextAndInferFormat(t *testing.T) {
	text := "HIGH vulnerability in dependency\nclean line\nsecret leaked in log\nlow finding"
	findings := ParseOutput("text", text)
	if len(findings) != 2 {
		t.Fatalf("findings = %+v", findings)
	}
	if findings[0].Severity != "high" {
		t.Fatalf("first finding = %+v", findings[0])
	}
	formats := map[string]string{
		"npm-audit":         "npm",
		"pnpm-audit":        "pnpm audit --json",
		"yarn-audit":        "yarn audit --json",
		"composer-audit":    "composer audit --format=json",
		"bundler-audit":     "bundle-audit check --format json",
		"dotnet-vulnerable": "dotnet list package --vulnerable",
		"osv":               "osv-scanner scan source --format json",
		"pip-audit":         "pip-audit",
		"semgrep":           "semgrep --json",
		"trivy":             "trivy image",
		"gitleaks":          "gitleaks detect",
		"govulncheck":       "govulncheck ./...",
		"json":              "scanner --json",
		"text":              "plain scanner",
	}
	for want, haystack := range formats {
		if got := inferFormat(haystack, haystack); got != want {
			t.Errorf("inferFormat(%q) = %q, want %q", haystack, got, want)
		}
	}
	if ParseOutput("unknown", "{}") != nil {
		t.Fatal("unknown format should return nil")
	}
}

func TestRunScanners(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()
	runDir := t.TempDir()
	cfg := config.Default()
	cfg.Acceptance.Security.Scanners = []config.Scanner{
		{Name: "npm-audit", Command: `printf '{"vulnerabilities":{"lodash":{"severity":"high","via":[{"title":"prototype pollution"}]}}}'`, Format: "npm-audit"},
		{Name: "empty", Command: ""},
	}
	result := RunScanners(context.Background(), cfg, dir, runDir)
	if len(result.Findings) != 1 || !strings.Contains(result.Findings[0].Title, "lodash") {
		t.Fatalf("result = %+v", result)
	}
	if result.OK {
		t.Fatal("findings should fail the scanner gate")
	}
	if len(result.Scanners) != 1 || result.Scanners[0] != "npm-audit" {
		t.Fatalf("scanners = %+v", result.Scanners)
	}
	if !util.FileExists(filepath.Join(runDir, "security", "npm-audit.log")) {
		t.Fatal("scanner log was not written")
	}

	required := config.Default()
	required.Acceptance.Security.Scanners = []config.Scanner{{Name: "broken", Command: "exit 2", Required: true}}
	requiredResult := RunScanners(context.Background(), required, dir, runDir)
	if requiredResult.OK || len(requiredResult.Warnings) != 1 {
		t.Fatalf("required scanner = %+v", requiredResult)
	}

	soft := config.Default()
	soft.Acceptance.Security.Scanners = []config.Scanner{{Name: "broken", Command: "exit 2"}}
	softResult := RunScanners(context.Background(), soft, dir, runDir)
	if !softResult.OK || len(softResult.Warnings) != 1 {
		t.Fatalf("optional scanner = %+v", softResult)
	}
}

func TestAutoScanners(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "package-lock.json", "{}")
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "npm"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	scanners := AutoScanners(dir)
	if len(scanners) != 1 || scanners[0].Name != "npm-audit" {
		t.Fatalf("scanners = %+v", scanners)
	}
	if len(AutoScanners(t.TempDir())) != 0 {
		t.Fatal("empty project should not detect scanners")
	}
}

func TestAutoScannersForOtherEcosystems(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"pnpm-lock.yaml", "yarn.lock", "composer.lock", "Gemfile.lock"} {
		writeTempFile(t, dir, name, "{}")
	}
	writeTempFile(t, dir, "demo.csproj", `<Project />`)
	binDir := t.TempDir()
	for _, name := range []string{"pnpm", "yarn", "composer", "bundle-audit", "dotnet", "osv-scanner"} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", binDir)
	found := map[string]bool{}
	for _, scanner := range AutoScanners(dir) {
		found[scanner.Name] = true
	}
	for _, want := range []string{"pnpm-audit", "yarn-audit", "composer-audit", "bundler-audit", "dotnet-vulnerable", "osv-scanner"} {
		if !found[want] {
			t.Errorf("missing scanner %q in %+v", want, found)
		}
	}

	cfg := config.Default()
	cfg.Acceptance.Security.Scanners = []config.Scanner{{Name: "pnpm-audit", Command: "exit 0", Format: "pnpm-audit"}}
	result := RunScanners(context.Background(), cfg, dir, t.TempDir())
	seen := 0
	for _, name := range result.Scanners {
		if name == "pnpm-audit" {
			seen++
		}
	}
	if seen != 1 {
		t.Fatalf("explicit scanner should replace the auto scanner: %+v", result.Scanners)
	}
}

func TestEvaluateGateReportAndVerdicts(t *testing.T) {
	cfg := config.Default()
	report := &Report{
		Verdict:  "fail",
		Findings: []Finding{{Severity: "high", Title: "report finding"}},
	}
	gate := EvaluateGate(cfg, nil, report)
	if gate.OK || len(gate.Blocking) != 1 || gate.Blocking[0].Title != "report finding" {
		t.Fatalf("gate = %+v", gate)
	}
	if gate.Findings[0].Source != "security" {
		t.Fatalf("report findings were not normalized: %+v", gate.Findings)
	}

	failNoFindings := EvaluateGate(cfg, nil, &Report{Verdict: "fail", Findings: []Finding{}})
	if failNoFindings.OK || len(failNoFindings.Blocking) != 1 {
		t.Fatalf("fail verdict = %+v", failNoFindings)
	}

	pass := EvaluateGate(cfg, nil, &Report{Verdict: "pass", Findings: []Finding{}})
	if !pass.OK {
		t.Fatalf("pass verdict = %+v", pass)
	}

	strict := config.Default()
	strict.Acceptance.Security.MaxSeverity = "high"
	medium := EvaluateGate(strict, []Finding{{Severity: "medium", Title: "medium"}}, &Report{Verdict: "pass", Findings: []Finding{}})
	if !medium.OK {
		t.Fatalf("medium should pass when max is high: %+v", medium)
	}
}

func TestFormatFindingsAndSort(t *testing.T) {
	if FormatFindings(nil, 10) != "No security findings." {
		t.Fatal("empty findings message changed")
	}
	findings := []Finding{
		{Severity: "low", Title: "low", Recommendation: "r"},
		{Severity: "critical", Title: "critical", File: "a.js", Line: 3, Recommendation: "fix"},
	}
	SortFindings(findings)
	if findings[0].Title != "critical" {
		t.Fatalf("sorted = %+v", findings)
	}
	formatted := FormatFindings(findings, 1)
	if !strings.Contains(formatted, "critical") || !strings.Contains(formatted, "a.js:3") || strings.Contains(formatted, "low") {
		t.Fatalf("formatted = %q", formatted)
	}
	all := FormatFindings(findings, 0)
	if !strings.Contains(all, "critical") || !strings.Contains(all, "low") {
		t.Fatalf("maxItems=0 should show all: %q", all)
	}
}

func TestMatchesIgnoreAndRedact(t *testing.T) {
	patterns := []string{"vendor/**", "*.min.js", "coverage.json", ""}
	cases := map[string]bool{
		"vendor/lib/a.js": true, "src/app.min.js": true, "coverage.json": true,
		"src/app.js": false,
	}
	for rel, want := range cases {
		if got := matchesIgnore(rel, patterns); got != want {
			t.Errorf("matchesIgnore(%q) = %v, want %v", rel, got, want)
		}
	}
	if redact("short") != "***" {
		t.Fatal("short values should be fully redacted")
	}
	if got := redact("abcdefghijkl"); got != "abcd…kl" {
		t.Fatalf("redact = %q", got)
	}
}
