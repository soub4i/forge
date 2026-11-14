package runner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func fakeOpenCode(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "opencode")
	if err := os.WriteFile(file, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func TestRunStageSuccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake binary uses a shell script")
	}
	fakeOpenCode(t, `#!/bin/sh
echo '{"sessionID":"ses_fake","type":"text","part":{"text":"completed work"}}'
echo '{"type":"step_finish","part":{"cost":0.03,"tokens":{"input":100,"output":20,"reasoning":5,"cache":{"read":10,"write":2}}}}'
echo 'not-json noise'
exit 0
`)
	dir := t.TempDir()
	logFile := filepath.Join(dir, "events", "session.jsonl")
	result := RunStage(context.Background(), Options{
		Model: "opencode/big-pickle", Prompt: "do the work", Cwd: dir,
		Title: "forge test", Timeout: 5 * time.Second, LogFile: logFile,
		Env: map[string]string{"FORGE_STAGE": "implementer"},
	})
	if !result.OK || result.Code != 0 || result.TimedOut {
		t.Fatalf("result = %+v", result)
	}
	if result.Text != "completed work" {
		t.Fatalf("text = %q", result.Text)
	}
	if result.SessionID != "ses_fake" || result.Steps != 1 || result.EventCount != 3 {
		t.Fatalf("result = %+v", result)
	}
	if result.CostUSD != 0.03 {
		t.Fatalf("cost = %v", result.CostUSD)
	}
	if result.Tokens.Input != 100 || result.Tokens.Output != 20 || result.Tokens.Reasoning != 5 {
		t.Fatalf("tokens = %+v", result.Tokens)
	}
	if result.Tokens.CacheRead != 10 || result.Tokens.CacheWrite != 2 {
		t.Fatalf("cache = %+v", result.Tokens)
	}
	logged, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logged), "completed work") || !strings.Contains(string(logged), "not-json noise") {
		t.Fatalf("log file = %q", logged)
	}
}

func TestRunStageFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake binary uses a shell script")
	}
	fakeOpenCode(t, `#!/bin/sh
echo '{"type":"error","error":{"message":"provider exploded"}}'
echo 'boom' >&2
exit 4
`)
	result := RunStage(context.Background(), Options{
		Model: "opencode/big-pickle", Prompt: "fail", Cwd: t.TempDir(), Timeout: 5 * time.Second,
	})
	if result.OK || result.Code != 4 {
		t.Fatalf("result = %+v", result)
	}
	joined := strings.Join(result.Errors, "\n")
	for _, want := range []string{"provider exploded", "opencode exited with code 4", "boom"} {
		if !strings.Contains(joined, want) {
			t.Errorf("errors are missing %q:\n%s", want, joined)
		}
	}
}

func TestRunStageTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake binary uses a shell script")
	}
	fakeOpenCode(t, "#!/bin/sh\nexec sleep 5\n")
	result := RunStage(context.Background(), Options{
		Model: "opencode/big-pickle", Prompt: "hang", Cwd: t.TempDir(), Timeout: 50 * time.Millisecond,
	})
	if !result.TimedOut || result.OK {
		t.Fatalf("result = %+v", result)
	}
	if result.Code != -1 {
		t.Fatalf("code = %d", result.Code)
	}
}

func TestRunStageMissingBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH handling differs on Windows")
	}
	t.Setenv("PATH", t.TempDir())
	result := RunStage(context.Background(), Options{
		Model: "opencode/big-pickle", Prompt: "x", Cwd: t.TempDir(), Timeout: time.Second,
	})
	if result.OK || len(result.Errors) == 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestSummarizeEvents(t *testing.T) {
	events := []map[string]any{
		{"sessionID": "ses_123", "type": "text", "part": map[string]any{"text": "hello "}},
		{"type": "tool_use", "part": map[string]any{"tool": "bash"}},
		{"type": "text", "part": map[string]any{"text": "world"}},
		{"type": "step_finish", "part": map[string]any{
			"cost":   0.0125,
			"tokens": map[string]any{"input": json.Number("100"), "output": json.Number("20"), "reasoning": json.Number("5"), "cache": map[string]any{"read": json.Number("30"), "write": json.Number("2")}},
		}},
		{"type": "error", "error": map[string]any{"message": "model failed"}},
	}
	summary := SummarizeEvents(events)
	if summary.Text != "hello world" {
		t.Fatalf("text = %q", summary.Text)
	}
	if summary.SessionID != "ses_123" {
		t.Fatalf("session = %q", summary.SessionID)
	}
	if summary.ToolCalls != 1 || summary.Steps != 1 {
		t.Fatalf("tools=%d steps=%d", summary.ToolCalls, summary.Steps)
	}
	if summary.CostUSD != 0.0125 {
		t.Fatalf("cost = %v", summary.CostUSD)
	}
	if summary.Tokens.Input != 100 || summary.Tokens.Output != 20 || summary.Tokens.Reasoning != 5 {
		t.Fatalf("tokens = %+v", summary.Tokens)
	}
	if summary.Tokens.CacheRead != 30 || summary.Tokens.CacheWrite != 2 {
		t.Fatalf("cache = %+v", summary.Tokens)
	}
	if len(summary.Errors) != 1 || summary.Errors[0] != "model failed" {
		t.Fatalf("errors = %+v", summary.Errors)
	}
}

func TestSummarizeEventsIgnoresUnknown(t *testing.T) {
	events := []map[string]any{
		{"type": "unknown", "part": map[string]any{"x": 1}},
		{"no_type": true},
	}
	summary := SummarizeEvents(events)
	if summary.Text != "" || len(summary.Errors) != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

func TestSummarizeEventsAlternativeErrorShapes(t *testing.T) {
	events := []map[string]any{
		{"type": "error", "error": "plain failure"},
		{"type": "error", "message": "top level message"},
		{"type": "error", "error": map[string]any{"name": "NameOnly"}},
		{"type": "error", "unknown": true},
	}
	summary := SummarizeEvents(events)
	if len(summary.Errors) != 4 {
		t.Fatalf("errors = %+v", summary.Errors)
	}
	if summary.Errors[0] != "plain failure" || summary.Errors[1] != "top level message" || summary.Errors[2] != "NameOnly" {
		t.Fatalf("errors = %+v", summary.Errors)
	}
	if !strings.Contains(summary.Errors[3], "unknown") {
		t.Fatalf("fallback error = %q", summary.Errors[3])
	}
}
