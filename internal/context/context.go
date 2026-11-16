package context

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/soub4i/forge/internal/config"
	"github.com/soub4i/forge/internal/gates"
	"github.com/soub4i/forge/internal/util"
)

var docOrder = []string{
	"docs/GOAL.md", "AGENTS.md", "README.md", "docs/ARCHITECTURE.md",
	"docs/DESIGN.md", "docs/PLAN.md", "docs/SECURITY.md", "docs/REVIEW.md",
}

type GitFacts struct {
	Repo       bool   `json:"repo"`
	Branch     string `json:"branch,omitempty"`
	Head       string `json:"head,omitempty"`
	LastCommit string `json:"lastCommit,omitempty"`
	Status     string `json:"status,omitempty"`
	DiffStat   string `json:"diffStat,omitempty"`
	Changed    string `json:"changed,omitempty"`
	BaseCommit string `json:"baseCommit,omitempty"`
}

type ManifestFact struct {
	Path    string `json:"path"`
	Preview string `json:"preview,omitempty"`
}

type ProjectFacts struct {
	Files          int               `json:"files"`
	TopExtensions  []string          `json:"topExtensions"`
	Lockfiles      []string          `json:"lockfiles"`
	HasTests       bool              `json:"hasTests"`
	HasBench       bool              `json:"hasBench"`
	Language       string            `json:"language"`
	Languages      []string          `json:"languages,omitempty"`
	PackageManager string            `json:"packageManager,omitempty"`
	Manifests      []ManifestFact    `json:"manifests,omitempty"`
	MakeTargets    []string          `json:"makeTargets,omitempty"`
	TaskRunners    []string          `json:"taskRunners,omitempty"`
	Commands       map[string]string `json:"detectedCommands,omitempty"`
}

type Doc struct {
	Path    string
	Content string
}

type Snapshot struct {
	Tree          string
	TreeTruncated bool
	Docs          []Doc
	Project       ProjectFacts
	Git           GitFacts
	Files         []util.FileInfo
}

func git(cwd string, args ...string) string {
	commandArgs := append([]string{"-C", cwd}, args...)
	cmd := exec.Command("git", commandArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return ""
	}
	return strings.TrimSpace(stdout.String())
}

func GitFactsFor(cwd, baseCommit string) GitFacts {
	if !util.DirExists(filepath.Join(cwd, ".git")) && git(cwd, "rev-parse", "--is-inside-work-tree") != "true" {
		return GitFacts{Repo: false}
	}
	status := git(cwd, "status", "--short")
	branch := git(cwd, "rev-parse", "--abbrev-ref", "HEAD")
	head := git(cwd, "rev-parse", "--short", "HEAD")
	last := git(cwd, "log", "-1", "--oneline")
	diffStat := git(cwd, "diff", "--stat", "HEAD")
	changed := status
	if baseCommit != "" {
		diffStat = git(cwd, "diff", "--stat", baseCommit, "--")
		changed = git(cwd, "diff", "--name-status", baseCommit, "--")
	}
	return GitFacts{
		Repo: true, Branch: branch, Head: head, LastCommit: last,
		Status: util.Truncate(status, 3000), DiffStat: util.Truncate(diffStat, 6000),
		Changed: util.Truncate(changed, 6000), BaseCommit: baseCommit,
	}
}

var lockfileNames = []string{
	"package-lock.json", "pnpm-lock.yaml", "yarn.lock", "bun.lockb", "bun.lock",
	"go.sum", "Cargo.lock", "poetry.lock", "uv.lock", "Pipfile.lock", "composer.lock",
	"Gemfile.lock", "pubspec.lock", "mix.lock", "Package.resolved", "packages.lock.json",
	"gradle.lockfile", "deno.lock", "flake.lock", "conan.lock", "vcpkg-lock.json",
}

var taskRunnerNames = []string{
	"Makefile", "makefile", "GNUmakefile", "justfile", "Justfile", ".justfile",
	"Taskfile.yml", "Taskfile.yaml", "deno.json", "deno.jsonc", "flake.nix",
	"WORKSPACE", "WORKSPACE.bazel", "MODULE.bazel", "BUILD.bazel", "Earthfile",
}

func ProjectFactsFor(cwd string) ProjectFacts {
	files, _ := util.WalkFiles(cwd, 5000, 8, 0)
	counts := map[string]int{}
	for _, file := range files {
		ext := strings.ToLower(filepath.Ext(file.Rel))
		if ext == "" {
			ext = "(none)"
		}
		counts[ext]++
	}
	type pair struct {
		ext   string
		count int
	}
	var pairs []pair
	for ext, count := range counts {
		pairs = append(pairs, pair{ext, count})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].count == pairs[j].count {
			return pairs[i].ext < pairs[j].ext
		}
		return pairs[i].count > pairs[j].count
	})
	if len(pairs) > 8 {
		pairs = pairs[:8]
	}
	top := make([]string, 0, len(pairs))
	for _, item := range pairs {
		top = append(top, item.ext+": "+itoa(item.count))
	}

	detected := gates.DetectProject(cwd)
	facts := ProjectFacts{
		Files:          len(files),
		TopExtensions:  top,
		HasTests:       hasTestFiles(files) || anyDir(cwd, []string{"test", "tests", "__tests__", "spec", "specs"}),
		HasBench:       hasBenchFiles(files) || anyDir(cwd, []string{"bench", "benches", "benchmark", "benchmarks", "perf", "performance"}),
		Language:       detected.Language,
		Languages:      detected.Languages,
		PackageManager: detected.PackageManager,
		Commands:       gates.DetectedCommandsToJSON(detected),
	}
	for _, name := range lockfileNames {
		if util.FileExists(filepath.Join(cwd, name)) {
			facts.Lockfiles = append(facts.Lockfiles, name)
		}
	}
	for index, rel := range detected.Manifests {
		if index >= 8 {
			break
		}
		facts.Manifests = append(facts.Manifests, ManifestFact{
			Path:    rel,
			Preview: util.Truncate(util.ReadText(filepath.Join(cwd, rel)), 1200),
		})
	}
	for _, name := range taskRunnerNames {
		if util.FileExists(filepath.Join(cwd, name)) {
			facts.TaskRunners = append(facts.TaskRunners, name)
		}
	}
	for _, name := range []string{"Makefile", "makefile", "GNUmakefile"} {
		data := util.ReadText(filepath.Join(cwd, name))
		if data == "" {
			continue
		}
		for _, line := range strings.Split(data, "\n") {
			line = strings.TrimRight(line, "\r")
			if strings.HasPrefix(line, "\t") || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "#") {
				continue
			}
			index := strings.Index(line, ":")
			if index <= 0 || index+1 < len(line) && line[index+1] == '=' {
				continue
			}
			target := line[:index]
			if !strings.ContainsAny(target, " \t=#") {
				facts.MakeTargets = append(facts.MakeTargets, target)
			}
			if len(facts.MakeTargets) >= 30 {
				break
			}
		}
		break
	}
	return facts
}

func hasTestFiles(files []util.FileInfo) bool {
	for _, file := range files {
		rel := strings.ToLower(filepath.ToSlash(file.Rel))
		base := path.Base(rel)
		dir := path.Dir(rel)
		for _, segment := range []string{"test", "tests", "__tests__", "spec", "specs"} {
			if dir == segment || strings.HasPrefix(dir, segment+"/") || strings.Contains(dir, "/"+segment+"/") || strings.HasSuffix(dir, "/"+segment) {
				return true
			}
		}
		for _, suffix := range []string{
			"_test.go", "_test.py", "_test.rb", "_test.rs", "_test.c", "_test.cc", "_test.cpp",
			"_test.exs", "_spec.rb", "_spec.lua", ".test.js", ".test.jsx", ".test.ts", ".test.tsx",
			".spec.js", ".spec.jsx", ".spec.ts", ".spec.tsx", "test.java", "tests.java",
			"test.kt", "test.scala", "test.cs", "tests.cs", "test.swift", "test.php",
		} {
			if strings.HasSuffix(base, suffix) {
				return true
			}
		}
		if strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py") {
			return true
		}
	}
	return false
}

func hasBenchFiles(files []util.FileInfo) bool {
	for _, file := range files {
		base := strings.ToLower(path.Base(filepath.ToSlash(file.Rel)))
		if strings.Contains(base, "bench") || strings.Contains(base, "perf") {
			return true
		}
	}
	return false
}

func BuildSnapshot(cwd string, cfg config.Config, baseCommit string) Snapshot {
	files, _ := util.WalkFiles(cwd, cfg.Context.MaxFiles, 6, 0)
	var treeParts []string
	for _, file := range files {
		treeParts = append(treeParts, file.Rel+" ("+formatBytes(file.Size)+")")
	}
	docs := make([]Doc, 0)
	seen := map[string]bool{}
	for _, rel := range docOrder {
		if seen[rel] {
			continue
		}
		seen[rel] = true
		content := util.ReadText(filepath.Join(cwd, rel))
		if strings.TrimSpace(content) == "" {
			continue
		}
		docs = append(docs, Doc{Path: rel, Content: util.Truncate(content, cfg.Context.MaxDocChars)})
	}
	return Snapshot{
		Tree:          strings.Join(treeParts, "\n"),
		TreeTruncated: len(files) >= cfg.Context.MaxFiles,
		Docs:          docs,
		Project:       ProjectFactsFor(cwd),
		Git:           GitFactsFor(cwd, baseCommit),
		Files:         files,
	}
}

func RenderSnapshot(snapshot Snapshot) string {
	var lines []string
	lines = append(lines, "### Repository files", "```")
	if snapshot.Tree == "" {
		lines = append(lines, "(empty repository)")
	} else {
		lines = append(lines, snapshot.Tree)
	}
	if snapshot.TreeTruncated {
		lines = append(lines, "[... file list truncated ...]")
	}
	lines = append(lines, "```", "### Project facts", "```json")
	data, _ := json.MarshalIndent(snapshot.Project, "", "  ")
	lines = append(lines, string(data), "```")
	if snapshot.Git.Repo {
		lines = append(lines, "### Git state", "```")
		lines = append(lines, "branch: "+valueOr(snapshot.Git.Branch, "(detached)")+"  head: "+valueOr(snapshot.Git.Head, "(none)"))
		if snapshot.Git.LastCommit != "" {
			lines = append(lines, "last: "+snapshot.Git.LastCommit)
		}
		if snapshot.Git.Status != "" {
			lines = append(lines, "status:\n"+snapshot.Git.Status)
		}
		if snapshot.Git.DiffStat != "" {
			lines = append(lines, "diff:\n"+snapshot.Git.DiffStat)
		}
		lines = append(lines, "```")
	}
	for _, doc := range snapshot.Docs {
		lines = append(lines, "### "+doc.Path, doc.Content)
	}
	return strings.Join(lines, "\n\n")
}

func anyDir(root string, names []string) bool {
	for _, name := range names {
		if util.DirExists(filepath.Join(root, name)) {
			return true
		}
	}
	return false
}

func formatBytes(size int64) string {
	const unit = 1024
	if size < unit {
		return strconv.FormatInt(size, 10) + " B"
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	units := "KMGTPE"
	if exp >= len(units) {
		exp = len(units) - 1
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), units[exp])
}

func itoa(value int) string {
	return strconv.Itoa(value)
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
