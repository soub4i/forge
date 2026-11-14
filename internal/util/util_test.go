package util

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRunIDFormatAndStability(t *testing.T) {
	stamp := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	first := RunID(stamp)
	second := RunID(stamp)
	if first != second {
		t.Fatalf("RunID is not deterministic: %q vs %q", first, second)
	}
	if len(first) != 22 || first[8] != '-' || first[15] != '-' {
		t.Fatalf("unexpected run id shape: %q", first)
	}
	if !strings.HasPrefix(first, "20260203-040506-") {
		t.Fatalf("unexpected run id prefix: %q", first)
	}
}

func TestTruncateAndTail(t *testing.T) {
	if got := Truncate("short", 100); got != "short" {
		t.Fatalf("Truncate short = %q", got)
	}
	got := Truncate("0123456789", 4)
	if !strings.HasPrefix(got, "0123") || !strings.Contains(got, "truncated") {
		t.Fatalf("Truncate = %q", got)
	}
	if got := Tail("0123456789", 4); !strings.HasSuffix(got, "6789") || !strings.Contains(got, "truncated") {
		t.Fatalf("Tail = %q", got)
	}
	if got := Tail("short", 0); got != "short" {
		t.Fatalf("Tail with zero max = %q", got)
	}
}

func TestFormatHelpers(t *testing.T) {
	durations := map[int64]string{0: "0ms", 500: "500ms", 1500: "1.5s", 65000: "1m 5s", 3661000: "61m 1s"}
	for input, want := range durations {
		if got := FormatDuration(input); got != want {
			t.Errorf("FormatDuration(%d) = %q, want %q", input, got, want)
		}
	}
	usd := map[float64]string{0: "$0.00", -1: "$0.00", 0.0012: "$0.0012", 1.234: "$1.23"}
	for input, want := range usd {
		if got := FormatUSD(input); got != want {
			t.Errorf("FormatUSD(%v) = %q, want %q", input, got, want)
		}
	}
	numbers := map[int64]string{0: "0", 999: "999", 1000: "1,000", 1234567: "1,234,567"}
	for input, want := range numbers {
		if got := FormatNumber(input); got != want {
			t.Errorf("FormatNumber(%d) = %q, want %q", input, got, want)
		}
	}
}

func TestHashObjectIsStable(t *testing.T) {
	first := HashObject(map[string]any{"a": 1, "b": []string{"x", "y"}})
	second := HashObject(map[string]any{"b": []string{"x", "y"}, "a": 1})
	if first != second {
		t.Fatal("map key order must not change the hash")
	}
	if first == HashObject(map[string]any{"a": 2}) {
		t.Fatal("different values must produce different hashes")
	}
	if len(first) != 64 {
		t.Fatalf("expected a sha256 hex digest, got %q", first)
	}
}

func TestExtractJSON(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{"plain", `{"verdict":"pass"}`, "pass"},
		{"fenced", "text\n```json\n{\"verdict\":\"fail\"}\n```\nmore", "fail"},
		{"embedded", `prefix {"verdict":"approve"} suffix`, "approve"},
		{"array", `[{"name":"x"}]`, ""},
		{"empty", "  ", ""},
		{"invalid", "not json at all", ""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			value := ExtractJSON(test.text)
			if value == nil {
				if test.want != "" {
					t.Fatalf("expected a value, got nil")
				}
				return
			}
			if test.want == "" {
				return
			}
			object, ok := value.(map[string]any)
			if !ok {
				t.Fatalf("expected an object, got %T", value)
			}
			if object["verdict"] != test.want {
				t.Fatalf("verdict = %v, want %q", object["verdict"], test.want)
			}
		})
	}
}

func TestParseJSONLines(t *testing.T) {
	text := "{\"type\":\"text\"}\nnoise\n\n[1,2]\n{bad json\n{\"type\":\"step_finish\"}"
	events := ParseJSONLines(text)
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2: %+v", len(events), events)
	}
	if events[0]["type"] != "text" || events[1]["type"] != "step_finish" {
		t.Fatalf("events = %+v", events)
	}
}

func TestReadWriteHelpers(t *testing.T) {
	dir := t.TempDir()
	textFile := filepath.Join(dir, "nested", "file.txt")
	if err := WriteText(textFile, "hello"); err != nil {
		t.Fatal(err)
	}
	if !FileExists(textFile) || DirExists(textFile) {
		t.Fatal("FileExists/DirExists disagree with a regular file")
	}
	if got := ReadText(textFile); got != "hello" {
		t.Fatalf("ReadText = %q", got)
	}
	if got := ReadText(filepath.Join(dir, "missing")); got != "" {
		t.Fatalf("missing ReadText = %q", got)
	}
	jsonFile := filepath.Join(dir, "value.json")
	if err := WriteJSON(jsonFile, map[string]any{"a": 1}); err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := ReadJSON(jsonFile, &value); err != nil {
		t.Fatal(err)
	}
	if value["a"] != float64(1) {
		t.Fatalf("value = %+v", value)
	}
	if !DirExists(dir) || FileExists(dir) {
		t.Fatal("a directory must not be reported as a file")
	}
}

func TestWalkFilesSkipsArtifactsAndBounds(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"README.md":                "# readme",
		"src/index.js":             "console.log(1)",
		"test/index.test.js":       "test",
		"node_modules/pkg/a.js":    "ignored",
		".git/config":              "ignored",
		"docs/goal.md":             "goal",
		".github/workflows/ci.yml": "ci",
	}
	for rel, content := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	walked, err := WalkFiles(dir, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, file := range walked {
		names = append(names, file.Rel)
	}
	joined := strings.Join(names, ",")
	if strings.Contains(joined, "node_modules") || strings.Contains(joined, ".git/") {
		t.Fatalf("artifacts were not skipped: %s", joined)
	}
	if !strings.Contains(joined, "README.md") || !strings.Contains(joined, "src/index.js") {
		t.Fatalf("expected project files: %s", joined)
	}
	if !strings.Contains(joined, ".github/workflows/ci.yml") {
		t.Fatalf(".github should be kept: %s", joined)
	}
	limited, err := WalkFiles(dir, 1, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 1 {
		t.Fatalf("maxFiles = %d", len(limited))
	}
	shallow, err := WalkFiles(dir, 0, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range shallow {
		if strings.Contains(file.Rel, "/") {
			t.Fatalf("maxDepth allowed a nested file: %s", file.Rel)
		}
	}
	small, err := WalkFiles(dir, 0, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(small) != 0 {
		t.Fatalf("maxBytes should filter every file here: %+v", small)
	}
}

func TestRunCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sh")
	}
	result := RunCommand(context.Background(), "printf hello", t.TempDir(), 5*time.Second)
	if !result.OK || result.Stdout != "hello" || result.Code != 0 {
		t.Fatalf("result = %+v", result)
	}
	failed := RunCommand(context.Background(), "exit 7", t.TempDir(), 5*time.Second)
	if failed.OK || failed.Code != 7 {
		t.Fatalf("failed = %+v", failed)
	}
	missing := RunCommand(context.Background(), "this-command-does-not-exist-xyz", t.TempDir(), 5*time.Second)
	if missing.OK || missing.Code == 0 {
		t.Fatalf("missing command = %+v", missing)
	}
	timedOut := RunCommand(context.Background(), "sleep 5", t.TempDir(), 20*time.Millisecond)
	if !timedOut.TimedOut {
		t.Fatalf("expected a timeout: %+v", timedOut)
	}
}

func TestWhichAndBinaryPaths(t *testing.T) {
	if !Which("sh") && !Which("cmd") {
		t.Fatal("expected a shell on PATH")
	}
	if Which("definitely-not-a-real-binary-xyz") {
		t.Fatal("unknown binary reported as available")
	}
	for _, path := range []string{"a.png", "b.PDF", "c.zip", "d.exe"} {
		if !IsBinaryPath(path) {
			t.Errorf("IsBinaryPath(%q) = false", path)
		}
	}
	for _, path := range []string{"a.js", "b.md", "Makefile", "c.json"} {
		if IsBinaryPath(path) {
			t.Errorf("IsBinaryPath(%q) = true", path)
		}
	}
}
