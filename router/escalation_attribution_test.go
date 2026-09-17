package router

import (
	"strings"
	"testing"
)

// A typical production threshold for repeated_tool_error is 2 — an operator's
// -escalate-after style flag (raised from 1 to 2 because a single tool error on a
// trivial goal was escalating the orchestrator to the frontier), which OVERRIDES
// this package's own default of 3. The threshold was originally 1 when recoverable
// edit slips were common; once they became rare, a single blip should not climb a
// rung.
//
// Whether it drags tiers up prematurely is a question a BARE COUNT cannot answer:
// "tool_errors=1" reads identically for a genuine capability wall and a single network
// blip. These tests pin the attribution that makes the question answerable, and pin
// that adding it changed no routing decision.

func TestTriggerDetail_AttributesARepeatedToolError(t *testing.T) {
	got := triggerDetail(TriggerRepeatedToolError, Signals{
		ToolErrors: 3, TransientErrors: 2, ParseFailures: 1, UsageErrors: 4,
	})
	for _, want := range []string{"tool_errors=3", "transient=2", "parse=1", "usage_excluded=4"} {
		if !strings.Contains(got, want) {
			t.Errorf("detail must carry %q so a transcript says WHAT climbed the ladder: %q", want, got)
		}
	}
}

// TestTriggerDetail_UnattributedStaysTerse — when there is nothing to attribute, the
// detail must not grow a noisy all-zero suffix.
func TestTriggerDetail_UnattributedStaysTerse(t *testing.T) {
	got := triggerDetail(TriggerRepeatedToolError, Signals{ToolErrors: 2})
	if got != "tool_errors=2" {
		t.Fatalf("a pure tool-error turn should read exactly %q, got %q", "tool_errors=2", got)
	}
}

// TestAttributionChangesNoDecision is the load-bearing negative. These fields are
// observability only; if they could move a routing decision they would be a behaviour
// change smuggled in as telemetry.
func TestAttributionChangesNoDecision(t *testing.T) {
	base := Signals{ToolErrors: 1}
	attributed := Signals{ToolErrors: 1, TransientErrors: 1, UsageErrors: 9, ParseFailures: 0}

	for _, threshold := range []float64{1, 2, 3} {
		if fires(TriggerRepeatedToolError, base, threshold) != fires(TriggerRepeatedToolError, attributed, threshold) {
			t.Fatalf("attribution must not change whether the trigger fires (threshold %v)", threshold)
		}
	}
	// And no other trigger reads them either.
	for _, k := range []Trigger{TriggerRetryExhaustion, TriggerParseFailure, TriggerExplicitHandoff, TriggerLowConfidence} {
		if fires(k, base, 1) != fires(k, attributed, 1) {
			t.Fatalf("trigger %s must be unaffected by attribution fields", k)
		}
	}
}

// TestProductionThresholdIsTwo records the effect of the override, established from
// CODE rather than from a runtime banner. This package defaults to 3; a caller passes
// an escalate-after threshold (default 2) through WithRepeatedToolError, and that
// wins. The two defaults disagreeing is itself worth pinning — a reader of this
// package alone would conclude the threshold is 3.
func TestProductionThresholdIsTwo(t *testing.T) {
	if got := DefaultConfig().Triggers[TriggerRepeatedToolError].ThresholdValue; got != 3 {
		t.Fatalf("this package's own default is documented as 3, got %v", got)
	}
	cfg := DefaultConfig().WithRepeatedToolError(2)
	if got := cfg.Triggers[TriggerRepeatedToolError].ThresholdValue; got != 2 {
		t.Fatalf("an escalate-after override of 2 must win, got %v", got)
	}
	if fires(TriggerRepeatedToolError, Signals{ToolErrors: 1}, 2) {
		t.Fatal("at the production threshold, a SINGLE tool error must NOT escalate")
	}
	if !fires(TriggerRepeatedToolError, Signals{ToolErrors: 2}, 2) {
		t.Fatal("at the production threshold, TWO tool errors must escalate a rung")
	}
}
