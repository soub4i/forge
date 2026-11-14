// Command bench exercises the real OpenCode JSONL event parser and writes
// machine-readable metrics to bench/results.json.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/soub4i/forge/internal/runner"
)

const eventsPerRun = 2000

func buildPayloads() [][]byte {
	payloads := make([][]byte, 0, eventsPerRun)
	for index := 0; index < eventsPerRun; index++ {
		event := map[string]any{
			"type":      "text",
			"sessionID": "ses_bench",
			"part":      map[string]any{"text": "line " + fmt.Sprint(index)},
		}
		if index%25 == 0 {
			event = map[string]any{
				"type": "step_finish",
				"part": map[string]any{
					"cost": 0.0,
					"tokens": map[string]any{
						"input": 100, "output": 20, "reasoning": 5,
						"cache": map[string]any{"read": 10, "write": 1},
					},
				},
			}
		}
		encoded, err := json.Marshal(event)
		if err != nil {
			panic(err)
		}
		payloads = append(payloads, encoded)
	}
	return payloads
}

// parseAll decodes the JSONL payloads and runs the production event summarizer.
func parseAll(payloads [][]byte) runner.Summary {
	events := make([]map[string]any, 0, len(payloads))
	for _, payload := range payloads {
		var event map[string]any
		if err := json.Unmarshal(payload, &event); err != nil {
			panic(err)
		}
		events = append(events, event)
	}
	return runner.SummarizeEvents(events)
}

func main() {
	payloads := buildPayloads()
	parseAll(payloads) // warm up

	const target = 750 * time.Millisecond
	started := time.Now()
	deadline := started.Add(target)
	runs := 0
	var totalEvents int64
	var last runner.Summary
	for time.Now().Before(deadline) {
		last = parseAll(payloads)
		runs++
		totalEvents += int64(len(payloads))
	}
	elapsed := time.Since(started)
	eventsPerSec := float64(totalEvents) / elapsed.Seconds()
	runsPerSec := float64(runs) / elapsed.Seconds()

	metrics := map[string]float64{
		"parse_ops_per_sec": eventsPerSec,
		"runs_per_sec":      runsPerSec,
		"runs":              float64(runs),
		"events_per_run":    float64(len(payloads)),
		"elapsed_ms":        float64(elapsed.Milliseconds()),
		"parsed_text_len":   float64(len(last.Text)),
	}
	result := map[string]any{
		"metrics": metrics,
		"runs":    runs,
		"unit":    "events/sec",
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "encode benchmark result:", err)
		os.Exit(1)
	}
	output := filepath.Join("bench", "results.json")
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "create bench directory:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(output, append(encoded, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write benchmark result:", err)
		os.Exit(1)
	}
	fmt.Println(string(encoded))
}
