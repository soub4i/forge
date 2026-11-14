package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitToolAvailable() bool {
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
	if !gitToolAvailable() {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.name", "Forge Test")
	runGit(t, dir, "config", "user.email", "forge@example.com")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "initial")
	return dir
}

func TestIsRepo(t *testing.T) {
	if IsRepo(t.TempDir()) {
		t.Fatal("a plain temp directory must not be a repo")
	}
	dir := initRepo(t)
	if !IsRepo(dir) {
		t.Fatal("initialized repo was not detected")
	}
}

func TestEnsureForgeExcluded(t *testing.T) {
	if EnsureForgeExcluded(t.TempDir()) {
		t.Fatal("non-repo should return false")
	}
	dir := initRepo(t)
	if !EnsureForgeExcluded(dir) {
		t.Fatal("repo should be updated")
	}
	exclude := filepath.Join(dir, ".git", "info", "exclude")
	content := string(mustRead(t, exclude))
	if !strings.Contains(content, ".forge/") {
		t.Fatalf("exclude file = %q", content)
	}
	if !EnsureForgeExcluded(dir) {
		t.Fatal("second call should also succeed")
	}
	again := string(mustRead(t, exclude))
	if strings.Count(again, ".forge/") != 1 {
		t.Fatalf("exclude entry was duplicated: %q", again)
	}
}

func mustRead(t *testing.T, file string) []byte {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestCheckpoint(t *testing.T) {
	if result := Checkpoint(t.TempDir(), "no repo"); !result.Skipped || result.OK {
		t.Fatalf("non-repo checkpoint = %+v", result)
	}
	dir := initRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "src.js"), []byte("export const x = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := Checkpoint(dir, "forge: iteration 1")
	if !result.OK || result.Skipped || result.Commit == "" {
		t.Fatalf("checkpoint = %+v", result)
	}
	if last := runGit(t, dir, "log", "-1", "--pretty=%s"); last != "forge: iteration 1" {
		t.Fatalf("commit message = %q", last)
	}
	clean := Checkpoint(dir, "nothing")
	if !clean.OK || !clean.Skipped {
		t.Fatalf("clean checkpoint = %+v", clean)
	}
}

func TestCheckpointExcludesForgeDir(t *testing.T) {
	dir := initRepo(t)
	if err := os.MkdirAll(filepath.Join(dir, ".forge", "runs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".forge", "state.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte("console.log(1)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := Checkpoint(dir, "forge: add app")
	if !result.OK {
		t.Fatalf("checkpoint = %+v", result)
	}
	tracked := runGit(t, dir, "ls-files")
	if strings.Contains(tracked, ".forge") {
		t.Fatalf(".forge must not be committed:\n%s", tracked)
	}
	if !strings.Contains(tracked, "app.js") {
		t.Fatalf("app.js was not committed:\n%s", tracked)
	}
}

func TestTag(t *testing.T) {
	if result := Tag(t.TempDir(), "v1.0.0", "release"); result.OK {
		t.Fatalf("non-repo tag = %+v", result)
	}
	dir := initRepo(t)
	first := Tag(dir, "v1.0.0", "Forge release v1.0.0")
	if !first.OK || first.Tag != "v1.0.0" || first.Skipped {
		t.Fatalf("tag = %+v", first)
	}
	second := Tag(dir, "v1.0.0", "")
	if !second.OK || !second.Skipped {
		t.Fatalf("existing tag = %+v", second)
	}
	tags := runGit(t, dir, "tag", "--list")
	if !strings.Contains(tags, "v1.0.0") {
		t.Fatalf("tags = %q", tags)
	}
}
