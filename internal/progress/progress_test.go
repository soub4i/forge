package progress

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDisabledDisplayPrintsStableStageLines(t *testing.T) {
	var out bytes.Buffer
	display := New(Options{Writer: &out})
	display.Start("Implementer", "iter 1 · opencode/model")
	display.SetActivity("editing internal/runner/runner.go")
	display.Event(map[string]any{"type": "tool", "part": map[string]any{"tool": "bash"}})
	display.Finish("Implementer", true, 1500*time.Millisecond, "100 in / 20 out")

	text := out.String()
	for _, want := range []string{"→", "Implementer", "opencode/model", "✓", "1.5s", "100 in / 20 out"} {
		if !strings.Contains(text, want) {
			t.Errorf("output is missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, clearLine) {
		t.Fatalf("disabled display wrote terminal control codes: %q", text)
	}
}

func TestDisplayDoesNotLeakLiveLine(t *testing.T) {
	var out bytes.Buffer
	display := New(Options{Writer: &out, Enabled: true, Width: 80})
	display.Start("Tester", "iter 1")
	display.Event(map[string]any{
		"type": "tool",
		"part": map[string]any{"tool": "bash", "state": map[string]any{"input": map[string]any{"command": "go test ./..."}}},
	})
	time.Sleep(150 * time.Millisecond)
	display.Finish("Tester", false, 900*time.Millisecond, "stage failed")

	text := out.String()
	if !strings.Contains(text, clearLine) || !strings.Contains(text, "running go test ./...") {
		t.Fatalf("live output = %q", text)
	}
	lines := strings.Split(text, "\n")
	if last := lines[len(lines)-2]; !strings.Contains(last, "✗ Tester") {
		t.Fatalf("last result line = %q", last)
	}
}

func TestDescribeTool(t *testing.T) {
	cwd := "/work/project"
	cases := []struct {
		name string
		part map[string]any
		want string
	}{
		{"bash", map[string]any{"tool": "bash", "state": map[string]any{"input": map[string]any{"command": "go test ./..."}}}, "running go test ./..."},
		{"edit", map[string]any{"tool": "edit", "state": map[string]any{"input": map[string]any{"filePath": "/work/project/src/main.go"}}}, "editing src/main.go"},
		{"grep", map[string]any{"tool": "grep", "state": map[string]any{"input": map[string]any{"pattern": "TODO"}}}, "searching for TODO"},
		{"plan", map[string]any{"tool": "todowrite"}, "updating the plan"},
		{"unknown", map[string]any{"tool": "custom"}, "using custom"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := describeTool(tc.part, cwd); got != tc.want {
				t.Fatalf("activity = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEventAccumulatesLiveCounters(t *testing.T) {
	var out bytes.Buffer
	display := New(Options{Writer: &out, Enabled: true, Width: 80})
	display.Start("Architect", "iter 1")
	display.Event(map[string]any{"type": "tool", "part": map[string]any{"tool": "read"}})
	display.Event(map[string]any{"type": "step_finish", "part": map[string]any{
		"cost":   0.0125,
		"tokens": map[string]any{"input": json.Number("1200"), "output": json.Number("300")},
	}})
	display.mu.Lock()
	tools, input, output, cost := display.toolCalls, display.input, display.output, display.cost
	display.mu.Unlock()
	display.Close()

	if tools != 1 || input != 1200 || output != 300 || cost != 0.0125 {
		t.Fatalf("counters = tools:%d input:%d output:%d cost:%v", tools, input, output, cost)
	}
}

func TestBar(t *testing.T) {
	if got := Bar(0, 4); got != "[░░░░░░░░]" {
		t.Fatalf("empty bar = %q", got)
	}
	if got := Bar(2, 4); got != "[████░░░░]" {
		t.Fatalf("half bar = %q", got)
	}
	if got := Bar(9, 4); got != "[████████]" {
		t.Fatalf("full bar = %q", got)
	}
}

func TestSetActivityUsesTerminalWidth(t *testing.T) {
	var out bytes.Buffer
	display := New(Options{Writer: &out, Enabled: true, Width: 100})
	display.Start("Machine gates", "iteration 1")
	display.SetActivity("check 5/5 [████████] lint ✓ build ✓ test ✓ coverage ✓ benchmark ✓")
	time.Sleep(150 * time.Millisecond)
	display.Close()

	text := out.String()
	if !strings.Contains(text, "benchmark ✓") {
		t.Fatalf("late gate name was lost: %q", text)
	}
	if strings.Contains(text, "bench…") {
		t.Fatalf("activity was truncated before rendering: %q", text)
	}
}

func TestTruncateAndFormatCompact(t *testing.T) {
	if got := truncate("abcdef", 4); got != "abc…" {
		t.Fatalf("truncate = %q", got)
	}
	if got := truncate("abc", 4); got != "abc" {
		t.Fatalf("truncate short = %q", got)
	}
	for input, want := range map[int64]string{950: "950", 1500: "1.5k", 2_500_000: "2.5m"} {
		if got := formatCompact(input); got != want {
			t.Errorf("formatCompact(%d) = %q, want %q", input, got, want)
		}
	}
}
