package git

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/soub4i/forge/internal/util"
)

func git(cwd string, args ...string) (string, string, int) {
	commandArgs := append([]string{"-C", cwd}, args...)
	cmd := exec.Command("git", commandArgs...)
	cmd.Env = os.Environ()
	cmd.WaitDelay = 5 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		code = 1
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		}
	}
	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), code
}

func gitWithEnv(cwd string, env map[string]string, args ...string) (string, string, int) {
	commandArgs := append([]string{"-C", cwd}, args...)
	cmd := exec.Command("git", commandArgs...)
	cmd.Env = os.Environ()
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	cmd.WaitDelay = 5 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		code = 1
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		}
	}
	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), code
}

// IsRepo reports whether cwd is inside a git work tree.
func IsRepo(cwd string) bool {
	stdout, _, code := git(cwd, "rev-parse", "--is-inside-work-tree")
	return code == 0 && stdout == "true"
}

// EnsureForgeExcluded adds `.forge/` to the repository exclude file.
func EnsureForgeExcluded(cwd string) bool {
	if !IsRepo(cwd) {
		return false
	}
	gitDir, _, code := git(cwd, "rev-parse", "--git-dir")
	if code != 0 || gitDir == "" {
		return false
	}
	resolved := gitDir
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(cwd, resolved)
	}
	excludeFile := filepath.Join(resolved, "info", "exclude")
	current := util.ReadText(excludeFile)
	for _, line := range strings.Split(current, "\n") {
		if strings.TrimSpace(line) == ".forge/" {
			return true
		}
	}
	if current != "" && !strings.HasSuffix(current, "\n") {
		current += "\n"
	}
	_ = util.EnsureDir(filepath.Dir(excludeFile))
	_ = util.WriteText(excludeFile, current+".forge/\n")
	return true
}

// Checkpoint stages and commits all changes.
type CheckpointResult struct {
	OK      bool   `json:"ok"`
	Skipped bool   `json:"skipped"`
	Commit  string `json:"commit,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// Checkpoint commits the current workspace state.
func Checkpoint(cwd, message string) CheckpointResult {
	if !IsRepo(cwd) {
		return CheckpointResult{OK: false, Skipped: true, Reason: "not a git repository"}
	}
	EnsureForgeExcluded(cwd)
	if _, stderr, code := git(cwd, "add", "-A"); code != 0 {
		return CheckpointResult{OK: false, Reason: valueOr(stderr, "git add failed")}
	}
	staged, _, _ := git(cwd, "diff", "--cached", "--name-only")
	if strings.TrimSpace(staged) == "" {
		return CheckpointResult{OK: true, Skipped: true, Reason: "nothing to commit"}
	}
	env := map[string]string{}
	if name, _, _ := git(cwd, "config", "user.name"); name == "" {
		env["GIT_AUTHOR_NAME"] = "Forge"
		env["GIT_COMMITTER_NAME"] = "Forge"
	}
	if email, _, _ := git(cwd, "config", "user.email"); email == "" {
		env["GIT_AUTHOR_EMAIL"] = "forge@localhost"
		env["GIT_COMMITTER_EMAIL"] = "forge@localhost"
	}
	if _, stderr, code := gitWithEnv(cwd, env, "commit", "-m", message); code != 0 {
		return CheckpointResult{OK: false, Reason: valueOr(stderr, "git commit failed")}
	}
	head, _, _ := git(cwd, "rev-parse", "--short", "HEAD")
	return CheckpointResult{OK: true, Commit: head}
}

// TagResult reports an annotated tag attempt.
type TagResult struct {
	OK      bool   `json:"ok"`
	Skipped bool   `json:"skipped"`
	Tag     string `json:"tag,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// Tag creates an annotated release tag if it does not exist.
func Tag(cwd, tagName, message string) TagResult {
	if !IsRepo(cwd) {
		return TagResult{OK: false, Reason: "not a git repository"}
	}
	if _, _, code := git(cwd, "rev-parse", "-q", "--verify", "refs/tags/"+tagName); code == 0 {
		return TagResult{OK: true, Skipped: true, Reason: "tag already exists"}
	}
	if message == "" {
		message = "Forge release " + tagName
	}
	if _, stderr, code := git(cwd, "tag", "-a", tagName, "-m", message); code != 0 {
		return TagResult{OK: false, Tag: tagName, Reason: valueOr(stderr, "git tag failed")}
	}
	return TagResult{OK: true, Tag: tagName}
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
