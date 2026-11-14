package context

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/soub4i/forge/internal/config"
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

type ProjectFacts struct {
	Files         int            `json:"files"`
	TopExtensions []string       `json:"topExtensions"`
	Lockfiles     []string       `json:"lockfiles"`
	HasTests      bool           `json:"hasTests"`
	HasBench      bool           `json:"hasBench"`
	Language      string         `json:"language"`
	Package       map[string]any `json:"package,omitempty"`
	GoMod         string         `json:"goMod,omitempty"`
	Cargo         string         `json:"cargo,omitempty"`
	Pyproject     string         `json:"pyproject,omitempty"`
	MakeTargets   []string       `json:"makeTargets,omitempty"`
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

	facts := ProjectFacts{
		Files:         len(files),
		TopExtensions: top,
		HasTests:      anyDir(cwd, []string{"test", "tests", "__tests__", "spec"}),
		HasBench:      anyDir(cwd, []string{"bench", "benchmark", "benchmarks"}),
	}
	for _, name := range []string{"package-lock.json", "pnpm-lock.yaml", "yarn.lock", "bun.lockb", "go.sum", "Cargo.lock", "poetry.lock"} {
		if util.FileExists(filepath.Join(cwd, name)) {
			facts.Lockfiles = append(facts.Lockfiles, name)
		}
	}
	if data := util.ReadText(filepath.Join(cwd, "package.json")); data != "" {
		var pkg map[string]any
		if json.Unmarshal([]byte(data), &pkg) == nil {
			facts.Language = "node"
			entry := map[string]any{}
			entry["name"] = pkg["name"]
			entry["version"] = pkg["version"]
			entry["scripts"] = pkg["scripts"]
			if deps, ok := pkg["dependencies"].(map[string]any); ok {
				entry["dependencies"] = keys(deps)
			}
			if deps, ok := pkg["devDependencies"].(map[string]any); ok {
				entry["devDependencies"] = keys(deps)
			}
			facts.Package = entry
		}
	} else if data := util.ReadText(filepath.Join(cwd, "go.mod")); data != "" {
		facts.Language = "go"
		facts.GoMod = util.Truncate(data, 1200)
	} else if data := util.ReadText(filepath.Join(cwd, "Cargo.toml")); data != "" {
		facts.Language = "rust"
		facts.Cargo = util.Truncate(data, 1200)
	} else if data := util.ReadText(filepath.Join(cwd, "pyproject.toml")); data != "" {
		facts.Language = "python"
		facts.Pyproject = util.Truncate(data, 1800)
	} else {
		facts.Language = "unknown"
	}
	if data := util.ReadText(filepath.Join(cwd, "Makefile")); data != "" {
		for _, line := range strings.Split(data, "\n") {
			if index := strings.Index(line, ":"); index > 0 {
				target := line[:index]
				if !strings.ContainsAny(target, " \t#") {
					facts.MakeTargets = append(facts.MakeTargets, target)
				}
			}
			if len(facts.MakeTargets) >= 30 {
				break
			}
		}
	}
	return facts
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

func keys(value map[string]any) []string {
	out := make([]string, 0, len(value))
	for key := range value {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
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
