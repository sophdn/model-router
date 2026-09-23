// Command routerdemo-live wires the cost-aware router to REAL model adapters and
// runs a live turn, so you can watch it route against a local llama.cpp server and
// an Anthropic fallback rather than the in-memory fakes of ./cmd/routerdemo.
//
// Configure the rungs with environment variables, then run it:
//
//	LLAMA_BASE_URL=http://localhost:8081/v1 \
//	ANTHROPIC_API_KEY=sk-ant-... \
//	go run ./cmd/routerdemo-live
//
// Each rung is optional. With only one variable set the ladder has one rung; with
// both, a simulated fault shows a real fallback from the local rung to Anthropic.
// With neither, the command prints what to set and exits non-zero.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sophdn/model-router/adapters"
	"github.com/sophdn/model-router/modeltypes"
	"github.com/sophdn/model-router/router"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "routerdemo-live:", err)
		os.Exit(1)
	}
}

func run() error {
	rungs, labels := buildLadder()
	if len(rungs) == 0 {
		return fmt.Errorf(`no model configured. Set at least one rung and re-run:

  local llama.cpp (cheap tier):
    export LLAMA_BASE_URL=http://localhost:8081/v1   # your llama.cpp host/port + /v1
    export LLAMA_MODEL=local                          # optional; the served model id

  Anthropic (fallback tier):
    export ANTHROPIC_API_KEY=sk-ant-...
    export ANTHROPIC_MODEL=claude-haiku-4-5-20251001  # optional; any model your key can call

  then: go run ./cmd/routerdemo-live`)
	}

	fmt.Println("model-router live demo — routing real model calls")
	for i, l := range labels {
		fmt.Printf("  rung %d: %s\n", i, l)
	}

	r := router.NewLadder(rungs, 0, router.WithConfig(router.DefaultConfig()))

	// One real turn on the rung the router rests on (the cheapest).
	if err := oneTurn(r, "hello turn"); err != nil {
		return err
	}

	// With a higher rung available, simulate a fault to show a real fallback.
	if len(rungs) > 1 {
		edge := r.EscalateForFault(modeltypes.FaultContextOverflow)
		fmt.Printf("\n  simulated a context-overflow fault -> escalated %s -> %s\n", edge.FromModel, edge.ToModel)
		if err := oneTurn(r, "escalated turn"); err != nil {
			return err
		}
	}
	return nil
}

// oneTurn runs a single real completion on the router's current rung and prints
// the outcome. A recoverable failure is reported with its fault kind, recovered
// through adapters.FaultOf exactly as the agent loop would to feed
// router.EscalateForFault.
func oneTurn(r *router.Router, label string) error {
	ad := r.NextAdapter()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	messages := []modeltypes.ChatMessage{
		{Role: modeltypes.RoleUser, Content: "Say hello in one short sentence."},
	}
	fmt.Printf("\n  %s -> calling %s ...\n", label, ad.Model())

	resp, err := ad.Complete(ctx, messages, nil)
	if err != nil {
		fmt.Printf("    call failed (fault=%s): %v\n", adapters.FaultOf(err), err)
		return fmt.Errorf("model call failed on %s", ad.Model())
	}
	fmt.Printf("    model:  %s\n", resp.Model)
	fmt.Printf("    reply:  %s\n", collapse(resp.Text))
	fmt.Printf("    usage:  in=%d out=%d  stop=%s\n", resp.Usage.InputTokens, resp.Usage.OutputTokens, resp.StopReason)
	return nil
}

// buildLadder assembles the ladder cheapest->strongest from the configured env
// vars: the local llama.cpp rung first, the Anthropic rung second.
func buildLadder() (rungs []modeltypes.Adapter, labels []string) {
	if base := os.Getenv("LLAMA_BASE_URL"); base != "" {
		model := envOr("LLAMA_MODEL", "local")
		rungs = append(rungs, &adapters.OpenAICompat{BaseURL: base, ModelID: model})
		labels = append(labels, fmt.Sprintf("local llama.cpp  %s  (%s)", model, base))
	}
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		model := envOr("ANTHROPIC_MODEL", "claude-haiku-4-5-20251001")
		rungs = append(rungs, &adapters.Anthropic{APIKey: key, ModelID: model})
		labels = append(labels, fmt.Sprintf("anthropic  %s", model))
	}
	return rungs, labels
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// collapse folds whitespace and newlines into single spaces for a tidy one-line
// print of the model's reply.
func collapse(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
