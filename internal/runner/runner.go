package runner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/soub4i/forge/internal/util"
)

type Summary struct {
	Text      string
	SessionID string
	CostUSD   float64
	Tokens    util.Tokens
	Errors    []string
	Steps     int
	ToolCalls int
}

type Options struct {
	Model   string
	Prompt  string
	Cwd     string
	Title   string
	Timeout time.Duration
	LogFile string
	Env     map[string]string
}

type Result struct {
	OK         bool
	Model      string
	Code       int
	TimedOut   bool
	Text       string
	SessionID  string
	CostUSD    float64
	Tokens     util.Tokens
	Errors     []string
	ToolCalls  int
	Steps      int
	DurationMs int64
	Stderr     string
	LogFile    string
	EventCount int
}

func SummarizeEvents(events []map[string]any) Summary {
	summary := Summary{}
	var textParts []string
	for _, event := range events {
		if event == nil {
			continue
		}
		if session, ok := event["sessionID"].(string); ok && summary.SessionID == "" {
			summary.SessionID = session
		}
		eventType, _ := event["type"].(string)
		part, _ := event["part"].(map[string]any)
		switch eventType {
		case "text":
			if text, ok := part["text"].(string); ok {
				textParts = append(textParts, text)
			}
		case "tool", "tool_use":
			summary.ToolCalls++
		case "step_finish":
			summary.Steps++
			summary.CostUSD += toFloat(part["cost"])
			tokens, _ := part["tokens"].(map[string]any)
			summary.Tokens.Input += toInt(tokens["input"])
			summary.Tokens.Output += toInt(tokens["output"])
			summary.Tokens.Reasoning += toInt(tokens["reasoning"])
			cache, _ := tokens["cache"].(map[string]any)
			summary.Tokens.CacheRead += toInt(cache["read"])
			summary.Tokens.CacheWrite += toInt(cache["write"])
		case "error":
			summary.Errors = append(summary.Errors, errorMessage(event))
		}
	}
	summary.Text = strings.Join(textParts, "")
	return summary
}

func errorMessage(event map[string]any) string {
	if value, ok := event["error"]; ok && value != nil {
		switch typed := value.(type) {
		case string:
			return typed
		case map[string]any:
			if message, ok := typed["message"].(string); ok && message != "" {
				return message
			}
			if name, ok := typed["name"].(string); ok && name != "" {
				return name
			}
		}
	}
	if message, ok := event["message"].(string); ok && message != "" {
		return message
	}
	data, _ := json.Marshal(event)
	return string(data)
}

func toFloat(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case json.Number:
		number, _ := typed.Float64()
		return number
	case string:
		number, _ := strconv.ParseFloat(typed, 64)
		return number
	}
	return 0
}

func toInt(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case json.Number:
		number, _ := typed.Int64()
		return number
	case string:
		number, _ := strconv.ParseInt(typed, 10, 64)
		return number
	}
	return 0
}

// RunStage runs one OpenCode session in the target workspace.
func RunStage(parent context.Context, options Options) Result {
	started := time.Now()
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Minute
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	args := []string{"run", "--format", "json", "--model", options.Model, "--auto", "--title", options.Title, options.Prompt}
	cmd := exec.CommandContext(ctx, "opencode", args...)
	cmd.Dir = options.Cwd
	env := append(os.Environ(), "NO_COLOR=1")
	for key, value := range options.Env {
		env = append(env, key+"="+value)
	}
	cmd.Env = env
	cmd.WaitDelay = 5 * time.Second

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{Model: options.Model, Errors: []string{err.Error()}, DurationMs: time.Since(started).Milliseconds(), LogFile: options.LogFile}
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	var logFile *os.File
	if options.LogFile != "" {
		_ = util.EnsureDir(dirOf(options.LogFile))
		logFile, _ = os.OpenFile(options.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if logFile != nil {
			defer logFile.Close()
		}
	}

	if err := cmd.Start(); err != nil {
		return Result{Model: options.Model, Errors: []string{err.Error()}, DurationMs: time.Since(started).Milliseconds(), LogFile: options.LogFile}
	}

	events := make([]map[string]any, 0)
	eventCount := 0
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		eventCount++
		if logFile != nil {
			_, _ = logFile.WriteString(line + "\n")
		}
		if !strings.HasPrefix(line, "{") && !strings.HasPrefix(line, "[") {
			continue
		}
		var event map[string]any
		decoder := json.NewDecoder(strings.NewReader(line))
		decoder.UseNumber()
		if err := decoder.Decode(&event); err == nil {
			events = append(events, event)
		}
	}
	waitErr := cmd.Wait()
	summary := SummarizeEvents(events)
	result := Result{
		OK:         true,
		Model:      options.Model,
		Code:       0,
		Text:       summary.Text,
		SessionID:  summary.SessionID,
		CostUSD:    summary.CostUSD,
		Tokens:     summary.Tokens,
		ToolCalls:  summary.ToolCalls,
		Steps:      summary.Steps,
		DurationMs: time.Since(started).Milliseconds(),
		Stderr:     stderr.String(),
		LogFile:    options.LogFile,
		EventCount: eventCount,
		Errors:     append([]string(nil), summary.Errors...),
	}
	if ctx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
		result.OK = false
		result.Code = -1
		result.Errors = append(result.Errors, fmt.Sprintf("Stage timed out after %d minutes.", int(timeout.Minutes())))
		return result
	}
	if waitErr != nil {
		result.OK = false
		result.Code = 1
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			result.Code = exitErr.ExitCode()
		}
		result.Errors = append(result.Errors, fmt.Sprintf("opencode exited with code %d.", result.Code))
	}
	if len(result.Errors) > 0 && result.Stderr != "" {
		result.Errors = append(result.Errors, util.Tail(result.Stderr, 2000))
	}
	if len(result.Errors) > 0 {
		result.OK = false
	}
	return result
}

func dirOf(path string) string {
	index := strings.LastIndexAny(path, "/\\")
	if index < 0 {
		return "."
	}
	return path[:index]
}
