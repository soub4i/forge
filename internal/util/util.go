package util

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// FileInfo is a file discovered while walking a workspace.
type FileInfo struct {
	Rel  string
	Size int64
}

// CommandResult is the bounded result of a shell command.
type CommandResult struct {
	Command    string
	Code       int
	Stdout     string
	Stderr     string
	TimedOut   bool
	Duration   time.Duration
	OK         bool
	Signal     string
	StartedAt  time.Time
	FinishedAt time.Time
}

// Tokens is provider-reported token usage for one model call.
type Tokens struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	Reasoning  int64 `json:"reasoning"`
	CacheRead  int64 `json:"cacheRead"`
	CacheWrite int64 `json:"cacheWrite"`
}

// Log writes a line to stdout.
func Log(message string) {
	fmt.Fprintln(os.Stdout, message)
}

// Warn writes a warning line to stderr.
func Warn(message string) {
	fmt.Fprintln(os.Stderr, "warning:", message)
}

// Error writes an error line to stderr.
func Error(message string) {
	fmt.Fprintln(os.Stderr, "error:", message)
}

// CommandExists reports whether a binary is on PATH.
func CommandExists(name string) bool {
	return Which(name)
}

var skipDirs = map[string]bool{
	".git": true, ".forge": true, "node_modules": true, "coverage": true,
	"dist": true, "build": true, "vendor": true, ".venv": true, "venv": true,
	"__pycache__": true, ".idea": true, ".vscode": true, ".next": true,
	".cache": true, "target": true, "bin": false,
}

// NowISO returns an RFC3339 timestamp with millisecond precision.
func NowISO() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

// RunID returns a sortable, unique-enough run identifier.
func RunID(t time.Time) string {
	const alphabet = "0123456789abcdef"
	seed := t.UnixNano()
	var b strings.Builder
	b.WriteString(t.UTC().Format("20060102-150405"))
	b.WriteByte('-')
	for i := 0; i < 6; i++ {
		b.WriteByte(alphabet[seed%16])
		seed /= 16
	}
	return b.String()
}

func EnsureDir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}

func FileExists(file string) bool {
	info, err := os.Stat(file)
	return err == nil && !info.IsDir()
}

func DirExists(dir string) bool {
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}

func ReadText(file string) string {
	data, err := os.ReadFile(file)
	if err != nil {
		return ""
	}
	return string(data)
}

func WriteText(file, text string) error {
	if err := EnsureDir(filepath.Dir(file)); err != nil {
		return err
	}
	return os.WriteFile(file, []byte(text), 0o644)
}

func ReadJSON(file string, target any) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func WriteJSON(file string, value any) error {
	if err := EnsureDir(filepath.Dir(file)); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(file, data, 0o644)
}

// Truncate keeps the beginning of text within max bytes.
func Truncate(text string, max int) string {
	if max <= 0 || len(text) <= max {
		return text
	}
	return text[:max] + "\n[... truncated ...]"
}

// Tail keeps the end of text within max bytes.
func Tail(text string, max int) string {
	if max <= 0 || len(text) <= max {
		return text
	}
	return "[... truncated ...]\n" + text[len(text)-max:]
}

func FormatDuration(ms int64) string {
	d := time.Duration(ms) * time.Millisecond
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
}

func FormatUSD(value float64) string {
	if value <= 0 {
		return "$0.00"
	}
	if value < 0.01 {
		return fmt.Sprintf("$%.4f", value)
	}
	return fmt.Sprintf("$%.2f", value)
}

func FormatNumber(value int64) string {
	s := strconv.FormatInt(value, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// HashObject returns a stable SHA-256 of a JSON-serializable value.
func HashObject(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		data = []byte(fmt.Sprintf("%v", value))
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ParseJSONLines decodes newline-delimited JSON, ignoring noise.
func ParseJSONLines(text string) []map[string]any {
	var events []map[string]any
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || (!strings.HasPrefix(line, "{") && !strings.HasPrefix(line, "[")) {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err == nil {
			events = append(events, event)
		}
	}
	return events
}

var fencedJSON = regexp.MustCompile("(?s)```(?:json)?\\s*(.*?)```")

// ExtractJSON finds the first useful JSON value in model output.
func ExtractJSON(text string) any {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	if value, ok := decodeJSONValue(trimmed); ok {
		return value
	}
	for _, match := range fencedJSON.FindAllStringSubmatch(trimmed, -1) {
		if value, ok := decodeJSONValue(strings.TrimSpace(match[1])); ok {
			return value
		}
	}
	for _, pair := range [][2]string{{"{", "}"}, {"[", "]"}} {
		start := strings.Index(trimmed, pair[0])
		end := strings.LastIndex(trimmed, pair[1])
		if start >= 0 && end > start {
			if value, ok := decodeJSONValue(trimmed[start : end+1]); ok {
				return value
			}
		}
	}
	return nil
}

func decodeJSONValue(text string) (any, bool) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, false
	}
	return value, true
}

// RunCommand executes a shell command with a timeout and bounded capture.
func RunCommand(parent context.Context, command, cwd string, timeout time.Duration) CommandResult {
	started := time.Now()
	ctx := parent
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(parent, timeout)
		defer cancel()
	}
	shell := "/bin/sh"
	args := []string{"-c", command}
	if filepath.Separator == '\\' {
		shell = "cmd"
		args = []string{"/C", command}
	}
	cmd := exec.CommandContext(ctx, shell, args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "NO_COLOR=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := CommandResult{
		Command:    command,
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		StartedAt:  started,
		FinishedAt: time.Now(),
		Duration:   time.Since(started),
	}
	if ctx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
		result.Code = -1
		return result
	}
	if err == nil {
		result.OK = true
		return result
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.Code = exitErr.ExitCode()
		return result
	}
	result.Code = -1
	result.Stderr = result.Stderr + "\n" + err.Error()
	return result
}

func Which(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// WalkFiles returns repository files, skipping dependencies and artifacts.
func WalkFiles(root string, maxFiles, maxDepth int, maxBytes int64) ([]FileInfo, error) {
	if maxFiles <= 0 {
		maxFiles = 5000
	}
	if maxDepth <= 0 {
		maxDepth = 8
	}
	var files []FileInfo
	rootDepth := strings.Count(filepath.Clean(root), string(filepath.Separator))
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path == root {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if skipDirs[entry.Name()] || strings.HasPrefix(entry.Name(), ".") && entry.Name() != ".github" && entry.Name() != ".env.example" {
				return filepath.SkipDir
			}
			depth := strings.Count(filepath.Clean(path), string(filepath.Separator)) - rootDepth
			if depth >= maxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil
		}
		if maxBytes > 0 && info.Size() > maxBytes {
			return nil
		}
		files = append(files, FileInfo{Rel: rel, Size: info.Size()})
		if len(files) >= maxFiles {
			return fs.SkipAll
		}
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].Rel < files[j].Rel })
	return files, err
}

func IsBinaryPath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico", ".pdf", ".zip", ".gz", ".tar", ".exe", ".dll", ".so", ".woff", ".woff2", ".ttf":
		return true
	}
	return false
}
