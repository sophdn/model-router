// Command routerdemo shows the cost-aware tier router making routing and
// escalation decisions on a small ladder of in-memory (Echo) adapters. It talks to
// no network and needs no model backend: every adapter is a deterministic fake, so
// the output is the routing logic alone.
//
// Run it with:
//
//	go run ./cmd/routerdemo
package main

import (
	"fmt"
	"strings"

	"github.com/sophdn/model-router/cost"
	"github.com/sophdn/model-router/laddercfg"
	"github.com/sophdn/model-router/modeltypes"
	"github.com/sophdn/model-router/router"
)

// ladder builds a fresh three-rung ladder of Echo adapters: a free local floor, a
// cheap hosted mid rung, and an expensive frontier rung. Each scenario builds its
// own so the runs are independent.
func ladder() []modeltypes.Adapter {
	return []modeltypes.Adapter{
		modeltypes.NewEcho("qwen3.8-27b"),                  // local  (free)
		modeltypes.NewEcho("google/gemini-3.1-flash-lite"), // mid    (cheap)
		modeltypes.NewEcho("claude-opus-4-8"),              // strong (frontier)
	}
}

func main() {
	fmt.Println("model-router demo — routing + escalation on a local/mid/strong ladder")
	fmt.Println("(all adapters are in-memory fakes; no network, no backend)")

	cheapRequest()
	faultEscalates()
	frontierIsCapped()
}

// cheapRequest: a clean turn stays on the free local floor. This is the common case
// the ladder exists to protect — most work should never leave the cheapest rung.
func cheapRequest() {
	section("1. a cheap request stays on the local tier")

	// Enable escalation so the router is a real ladder, then feed it a clean turn.
	r := router.NewLadder(ladder(), 0, router.WithConfig(router.DefaultConfig()))

	served(r, "top of turn")
	edge := r.Observe(router.Signals{}) // no tool errors, no faults — a clean turn
	report(r, edge, "after a clean turn")
}

// faultEscalates: a mid-turn context overflow the loop could not absorb climbs the
// ladder one rung immediately, so the retry runs on a larger-window model.
func faultEscalates() {
	section("2. a model-call fault escalates one tier")

	r := router.NewLadder(ladder(), 0, router.WithConfig(router.DefaultConfig()))

	served(r, "top of turn")
	edge := r.EscalateForFault(modeltypes.FaultContextOverflow)
	report(r, edge, "after a context-overflow fault")
	served(r, "next turn now runs on")
}

// frontierIsCapped: with WithBoundedTop the strong rung serves a fixed number of
// turns, then a further climb onto it is refused and bounced one rung down — so the
// frontier is escalation-only and usage-bounded, never a per-turn default.
func frontierIsCapped() {
	section("3. the frontier rung is usage-capped")

	// Bound the top rung to a single turn.
	r := router.NewLadder(ladder(), 0,
		router.WithConfig(router.DefaultConfig()),
		router.WithBoundedTop(1),
	)

	// Two tool-error turns climb local→mid→strong.
	r.Observe(router.Signals{ToolErrors: 9})
	r.Observe(router.Signals{ToolErrors: 9})
	fmt.Printf("  escalated to the top rung: current model is %q\n", r.CurrentModel())

	served(r, "first frontier turn (within the cap)")
	served(r, "second frontier turn (cap spent)")
	fmt.Printf("  frontier turns served: %d of %d; climbs blocked by the cap: %d\n",
		r.BoundedTurns(), r.BoundMax(), r.BoundBlocked())
}

// served prints the adapter NextAdapter hands out, plus its cost tier and the
// per-turn tool-round budget the ladder policy would grant that rung.
func served(r *router.Router, label string) {
	m := r.NextAdapter().Model()
	tier := cost.Classify(m)
	rounds := laddercfg.RoundsForModel(24, m) // 24 = an example base round budget
	fmt.Printf("  %-34s → %-30s [tier=%-6s rounds=%d]\n", label, m, tier, rounds)
}

// report prints the transition an Observe/Escalate call produced.
func report(r *router.Router, e router.Edge, label string) {
	switch e.Direction {
	case router.EdgeEscalate:
		fmt.Printf("  %-34s → ESCALATE %s→%s (trigger=%s: %s)\n",
			label, e.FromModel, e.ToModel, e.Trigger, e.Detail)
	case router.EdgeDeescalate:
		fmt.Printf("  %-34s → DE-ESCALATE %s→%s\n", label, e.FromModel, e.ToModel)
	default:
		fmt.Printf("  %-34s → no transition (state=%s, resting on %s)\n",
			label, r.State(), r.CurrentModel())
	}
}

func section(title string) {
	fmt.Println()
	fmt.Println(title)
	fmt.Println(strings.Repeat("-", len(title)))
}
