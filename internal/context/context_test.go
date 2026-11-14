package context

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/soub4i/forge/internal/config"
	"github.com/soub4i/forge/internal/util"
)

func writeContextFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestProjectFactsForNode(t *testing.T) {
	dir := t.TempDir()
	writeContextFile(t, dir, "package.json", `{
		"name": "demo",
		"version": "1.2.3",
		"scripts": {"test": "node --test"},
		"dependencies": {"left-pad": "1.0.0"},
		"devDependencies": {"typescript": "5.0.0"}
	}`)
	writeContextFile(t, dir, "package-lock.json", "{}")
	writeContextFile(t, dir, "Makefile", "test:\n\tnode --test\n\nbuild:\n\tnode build.js\n")
	if err := os.MkdirAll(filepath.Join(dir, "test"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "bench"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeContextFile(t, dir, "src/index.js", "console.log(1)")
	writeContextFile(t, dir, "src/util.js", "export const x = 1")

	facts := ProjectFactsFor(dir)
	if facts.Language != "node" || !facts.HasTests || !facts.HasBench {
		t.Fatalf("facts = %+v", facts)
	}
	if len(facts.Lockfiles) != 1 || facts.Lockfiles[0] != "package-lock.json" {
		t.Fatalf("lockfiles = %v", facts.Lockfiles)
	}
	if facts.Package["name"] != "demo" {
		t.Fatalf("package = %+v", facts.Package)
	}
	scripts, ok := facts.Package["scripts"].(map[string]any)
	if !ok || scripts["test"] != "node --test" {
		t.Fatalf("scripts = %+v", facts.Package["scripts"])
	}
	if deps, ok := facts.Package["dependencies"].([]string); !ok || len(deps) != 1 || deps[0] != "left-pad" {
		t.Fatalf("dependencies = %+v", facts.Package["dependencies"])
	}
	joined := strings.Join(facts.TopExtensions, ",")
	if !strings.Contains(joined, ".js: 2") {
		t.Fatalf("top extensions = %v", facts.TopExtensions)
	}
	if len(facts.MakeTargets) != 2 || facts.MakeTargets[0] != "test" {
		t.Fatalf("make targets = %v", facts.MakeTargets)
	}
}

func TestProjectFactsForOtherLanguages(t *testing.T) {
	goDir := t.TempDir()
	writeContextFile(t, goDir, "go.mod", "module demo\n\ngo 1.23\n")
	if facts := ProjectFactsFor(goDir); facts.Language != "go" || !strings.Contains(facts.GoMod, "module demo") {
		t.Fatalf("go facts = %+v", facts)
	}

	rustDir := t.TempDir()
	writeContextFile(t, rustDir, "Cargo.toml", "[package]\nname = \"demo\"\n")
	if facts := ProjectFactsFor(rustDir); facts.Language != "rust" || facts.Cargo == "" {
		t.Fatalf("rust facts = %+v", facts)
	}

	pythonDir := t.TempDir()
	writeContextFile(t, pythonDir, "pyproject.toml", "[project]\nname = \"demo\"\n")
	if facts := ProjectFactsFor(pythonDir); facts.Language != "python" || facts.Pyproject == "" {
		t.Fatalf("python facts = %+v", facts)
	}

	emptyDir := t.TempDir()
	if facts := ProjectFactsFor(emptyDir); facts.Language != "unknown" || facts.Files != 0 {
		t.Fatalf("empty facts = %+v", facts)
	}
}

func TestBuildSnapshotAndRender(t *testing.T) {
	dir := t.TempDir()
	writeContextFile(t, dir, "README.md", "# Demo tool\n\nUsage.")
	writeContextFile(t, dir, "AGENTS.md", "agent rules")
	writeContextFile(t, dir, "docs/GOAL.md", "the goal")
	writeContextFile(t, dir, "docs/ARCHITECTURE.md", strings.Repeat("architecture ", 1000))
	writeContextFile(t, dir, "src/index.js", "console.log(1)")

	cfg := config.Default()
	cfg.Context.MaxDocChars = 1000
	snapshot := BuildSnapshot(dir, cfg, "")
	if snapshot.Git.Repo {
		t.Fatal("a plain temp directory must not be reported as a git repository")
	}
	if !strings.Contains(snapshot.Tree, "README.md") || !strings.Contains(snapshot.Tree, "src/index.js") {
		t.Fatalf("tree = %q", snapshot.Tree)
	}
	if len(snapshot.Docs) != 4 {
		t.Fatalf("docs = %+v", snapshot.Docs)
	}
	if snapshot.Docs[0].Path != "docs/GOAL.md" {
		t.Fatalf("docs are not in contract order: %+v", snapshot.Docs)
	}
	for _, doc := range snapshot.Docs {
		if len(doc.Content) > 1100 {
			t.Fatalf("doc %s was not truncated: %d chars", doc.Path, len(doc.Content))
		}
	}
	if snapshot.Project.Language != "unknown" {
		t.Fatalf("project = %+v", snapshot.Project)
	}

	rendered := RenderSnapshot(snapshot)
	for _, want := range []string{"### Repository files", "README.md", "### Project facts", "### docs/GOAL.md", "the goal"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("snapshot is missing %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "### Git state") {
		t.Fatal("non-repo snapshot must not render a git section")
	}
}

func TestRenderSnapshotGitAndEmptyTree(t *testing.T) {
	snapshot := Snapshot{
		Git: GitFacts{
			Repo: true, Branch: "main", Head: "abc123", LastCommit: "abc123 init",
			Status: " M src/a.js", DiffStat: " 1 file changed",
		},
	}
	rendered := RenderSnapshot(snapshot)
	for _, want := range []string{"(empty repository)", "### Git state", "branch: main  head: abc123", "last: abc123 init", "status:", "diff:"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("render is missing %q:\n%s", want, rendered)
		}
	}

	detached := RenderSnapshot(Snapshot{Git: GitFacts{Repo: true}})
	if !strings.Contains(detached, "branch: (detached)  head: (none)") {
		t.Fatalf("detached render = %q", detached)
	}
}

func gitAvailable() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func initRepo(t *testing.T) string {
	t.Helper()
	if !gitAvailable() {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.name", "Forge Test")
	runGit(t, dir, "config", "user.email", "forge@example.com")
	writeContextFile(t, dir, "README.md", "# repo")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "initial")
	return dir
}

func TestGitFactsForRepo(t *testing.T) {
	dir := initRepo(t)
	head := runGit(t, dir, "rev-parse", "--short", "HEAD")
	writeContextFile(t, dir, "src/new.js", "export const x = 1")
	runGit(t, dir, "add", "-A")

	facts := GitFactsFor(dir, "")
	if !facts.Repo {
		t.Fatal("repo was not detected")
	}
	if facts.Head != head {
		t.Fatalf("head = %q, want %q", facts.Head, head)
	}
	if facts.Branch == "" || facts.LastCommit == "" {
		t.Fatalf("facts = %+v", facts)
	}
	if !strings.Contains(facts.Status, "src/") {
		t.Fatalf("status = %q", facts.Status)
	}

	based := GitFactsFor(dir, head)
	if !strings.Contains(based.Changed, "src/") {
		t.Fatalf("changed = %q", based.Changed)
	}
	if based.BaseCommit != head {
		t.Fatalf("base = %q", based.BaseCommit)
	}
}

func TestGitFactsForNonRepo(t *testing.T) {
	if facts := GitFactsFor(t.TempDir(), ""); facts.Repo {
		t.Fatalf("plain directory reported as repo: %+v", facts)
	}
}

func TestFormatBytesRendering(t *testing.T) {
	dir := t.TempDir()
	writeContextFile(t, dir, "big.txt", strings.Repeat("x", 2048))
	cfg := config.Default()
	snapshot := BuildSnapshot(dir, cfg, "")
	if !strings.Contains(snapshot.Tree, "big.txt (2.0 KB)") {
		t.Fatalf("tree = %q", snapshot.Tree)
	}
	if !util.FileExists(filepath.Join(dir, "big.txt")) {
		t.Fatal("test fixture disappeared")
	}
}
