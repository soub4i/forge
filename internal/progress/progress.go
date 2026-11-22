// Package progress renders live terminal feedback for long-running harness work.
//
// The display is intentionally dependency-free. When it is enabled it keeps a
// single status line at the bottom of the terminal: a spinner, elapsed time,
// the current model/tool action, and live tool/token counters. When it is
// disabled (pipes, CI, --no-progress) it still writes stable stage and result
// lines so logs remain readable.
package progress

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/soub4i/forge/internal/util"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const (
	colorReset = "\x1b[0m"
	colorBold  = "\x1b[1m"
	colorDim   = "\x1b[2m"
	colorCyan  = "\x1b[36m"
	colorGreen = "\x1b[32m"
	colorRed   = "\x1b[31m"
	colorAmber = "\x1b[33m"
	clearLine  = "\r\x1b[2K"
)

// Options configures a Display.
type Options struct {
	Writer  io.Writer
	Cwd     string
	Enabled bool
	Color   bool
	Width   int
}

// Display owns all live progress output for one command.
type Display struct {
	out     io.Writer
	cwd     string
	enabled bool
	color   bool
	width   int

	mu        sync.Mutex
	running   bool
	started   time.Time
	frame     int
	activity  string
	toolCalls int
	input     int64
	output    int64
	cost      float64
	stop      chan struct{}
	done      chan struct{}
}

// IsTerminal reports whether f is an interactive terminal.
func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// New creates a progress display.
func New(options Options) *Display {
	out := options.Writer
	if out == nil {
		out = os.Stdout
	}
	width := options.Width
	if width <= 0 {
		width = 80
		if value, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && value >= 40 {
			width = value
		}
	}
	return &Display{
		out: out, cwd: options.Cwd, enabled: options.Enabled,
		color: options.Color && options.Enabled, width: width,
	}
}

// Enabled reports whether the display animates a live status line.
func (d *Display) Enabled() bool {
	return d != nil && d.enabled
}

// Start prints a permanent stage header and begins the live status line.
func (d *Display) Start(label, detail string) {
	if d == nil {
		return
	}
	d.stopLive()
	header := "  " + d.paint(colorCyan, "→") + " " + d.paint(colorBold, label)
	if detail != "" {
		header += "  " + d.paint(colorDim, "· "+detail)
	}
	fmt.Fprintln(d.out, header)
	if !d.enabled {
		return
	}
	d.mu.Lock()
	d.running = true
	d.started = time.Now()
	d.frame = 0
	d.activity = "starting"
	d.toolCalls = 0
	d.input = 0
	d.output = 0
	d.cost = 0
	d.stop = make(chan struct{})
	d.done = make(chan struct{})
	go d.animate(d.stop, d.done)
	d.mu.Unlock()
}

// SetActivity replaces the action shown in the live status line.
func (d *Display) SetActivity(text string) {
	if d == nil || !d.enabled {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.running {
		// Keep the full activity here; renderLocked truncates to the real
		// terminal width. Truncating early hid the last gate name in narrow
		// but still sufficient terminals.
		d.activity = normalize(text)
	}
}

// Event updates live counters from one OpenCode JSONL event.
func (d *Display) Event(event map[string]any) {
	if d == nil || !d.enabled || len(event) == 0 {
		return
	}
	eventType, _ := event["type"].(string)
	part, _ := event["part"].(map[string]any)

	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.running {
		return
	}
	switch eventType {
	case "tool", "tool_use":
		d.toolCalls++
		if text := describeTool(part, d.cwd); text != "" {
			d.activity = text
		}
	case "step_start":
		if d.activity == "starting" || d.activity == "" {
			d.activity = "thinking"
		}
	case "step_finish":
		input, output, cost := stepCounters(part)
		d.input += input
		d.output += output
		d.cost += cost
	case "text":
		if d.activity == "starting" || d.activity == "" {
			d.activity = "writing response"
		}
	case "error":
		d.activity = "model reported an error"
	}
}

// Finish clears the live line and prints a permanent result line.
func (d *Display) Finish(label string, ok bool, duration time.Duration, detail string) {
	if d == nil {
		return
	}
	d.stopLive()
	mark, color := "✓", colorGreen
	if !ok {
		mark, color = "✗", colorRed
	}
	line := "  " + d.paint(color, mark) + " " + d.paint(colorBold, label)
	if duration > 0 {
		line += "  " + d.paint(colorDim, "· "+util.FormatDuration(duration.Milliseconds()))
	}
	if detail != "" {
		line += "  " + d.paint(colorDim, "· "+detail)
	}
	fmt.Fprintln(d.out, line)
}

// Outcome prints a result line for a check that had no dedicated start line.
func (d *Display) Outcome(label string, ok bool, detail string) {
	d.Finish(label, ok, 0, detail)
}

// Close clears any live line still on screen.
func (d *Display) Close() {
	d.stopLive()
}

func (d *Display) animate(stop, done chan struct{}) {
	ticker := time.NewTicker(120 * time.Millisecond)
	defer ticker.Stop()
	defer close(done)

	d.mu.Lock()
	d.renderLocked()
	d.mu.Unlock()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			d.mu.Lock()
			d.frame++
			d.renderLocked()
			d.mu.Unlock()
		}
	}
}

func (d *Display) stopLive() {
	if d == nil {
		return
	}
	d.mu.Lock()
	if !d.running {
		d.mu.Unlock()
		return
	}
	close(d.stop)
	done := d.done
	d.mu.Unlock()

	<-done

	d.mu.Lock()
	d.running = false
	fmt.Fprint(d.out, clearLine)
	d.mu.Unlock()
}

func (d *Display) renderLocked() {
	frame := spinnerFrames[d.frame%len(spinnerFrames)]
	parts := []string{util.FormatDuration(time.Since(d.started).Milliseconds())}
	if d.activity != "" {
		parts = append(parts, d.activity)
	}
	if d.toolCalls > 0 {
		parts = append(parts, fmt.Sprintf("%d tools", d.toolCalls))
	}
	tokens := d.input + d.output
	if tokens > 0 {
		parts = append(parts, formatCompact(tokens)+" tok")
	}
	if d.cost > 0 {
		parts = append(parts, util.FormatUSD(d.cost))
	}
	plain := frame + " " + strings.Join(parts, " · ")
	plain = truncate(plain, d.width-2)
	if d.color {
		rest := strings.TrimPrefix(plain, frame)
		plain = d.paint(colorCyan, frame) + rest
	}
	fmt.Fprint(d.out, clearLine+"  "+plain)
}

func (d *Display) paint(code, text string) string {
	if !d.color || text == "" {
		return text
	}
	return code + text + colorReset
}

// Bar renders a compact completion bar for finite checklists such as gates.
func Bar(done, total int) string {
	const width = 8
	if total <= 0 {
		return ""
	}
	if done < 0 {
		done = 0
	}
	if done > total {
		done = total
	}
	filled := done * width / total
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "]"
}

func formatCompact(value int64) string {
	if value >= 1_000_000 {
		return fmt.Sprintf("%.1fm", float64(value)/1_000_000)
	}
	if value >= 1_000 {
		return fmt.Sprintf("%.1fk", float64(value)/1_000)
	}
	return strconv.FormatInt(value, 10)
}

func truncate(text string, max int) string {
	if max <= 0 {
		return ""
	}
	if utf8.RuneCountInString(text) <= max {
		return text
	}
	if max == 1 {
		return "…"
	}
	runes := []rune(text)
	return string(runes[:max-1]) + "…"
}

func normalize(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func sanitize(text string, max int) string {
	return truncate(normalize(text), max)
}

func describeTool(part map[string]any, cwd string) string {
	tool := stringValue(part["tool"])
	if tool == "" {
		tool = stringValue(part["name"])
	}
	tool = sanitize(tool, 32)
	if tool == "" {
		return ""
	}

	state, _ := part["state"].(map[string]any)
	input := part["input"]
	if input == nil {
		input = state["input"]
	}
	fields, _ := input.(map[string]any)
	path := relativePath(cwd, firstString(fields, "filePath", "file_path", "path", "file", "filename"))
	pattern := firstString(fields, "pattern", "query", "regex")
	command := sanitize(firstString(fields, "command", "cmd"), 48)
	description := sanitize(firstString(fields, "description", "url", "prompt"), 48)
	detail := path
	if detail == "" {
		detail = pattern
	}
	if detail == "" {
		detail = description
	}

	verb := "using"
	switch strings.ToLower(tool) {
	case "bash", "shell", "terminal":
		verb = "running"
		detail = command
	case "read", "view", "cat":
		verb = "reading"
	case "write", "create":
		verb = "writing"
	case "edit", "patch", "apply_patch", "replace":
		verb = "editing"
	case "grep", "search":
		verb = "searching for"
		if detail == "" {
			detail = description
		}
	case "glob", "find":
		verb = "finding"
	case "list", "ls":
		verb = "listing"
	case "webfetch", "fetch":
		verb = "fetching"
	case "todowrite", "todo", "todoread":
		return "updating the plan"
	case "task", "agent":
		return "delegating a subtask"
	}

	text := verb + " " + tool
	if verb != "using" {
		text = verb
	}
	if detail != "" {
		text += " " + detail
	}
	return sanitize(text, 60)
}

func stepCounters(part map[string]any) (int64, int64, float64) {
	if part == nil {
		return 0, 0, 0
	}
	tokens, _ := part["tokens"].(map[string]any)
	return number(tokens["input"]), number(tokens["output"]), amount(part["cost"])
}

func number(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	case int:
		return int64(typed)
	case int64:
		return typed
	case string:
		parsed, _ := strconv.ParseInt(typed, 10, 64)
		return parsed
	}
	return 0
}

func amount(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case json.Number:
		parsed, _ := typed.Float64()
		return parsed
	case string:
		parsed, _ := strconv.ParseFloat(typed, 64)
		return parsed
	}
	return 0
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func firstString(fields map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringValue(fields[key]); value != "" {
			return value
		}
	}
	return ""
}

func relativePath(cwd, path string) string {
	if path == "" {
		return ""
	}
	if cwd != "" {
		if rel, err := filepath.Rel(cwd, path); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(path)
}
