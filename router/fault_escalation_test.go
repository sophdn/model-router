package router

import (
	"testing"

	"github.com/sophdn/model-router/modeltypes"
)

// TestEscalateForFaultMapsToTaxonomy: a model-call fault the loop could not absorb
// climbs one rung, mapped onto the closed escalation taxonomy (malformed→
// parse_failure, overflow/timeout→retry_exhaustion). This is the fix for
// escalation-ladder-ignores-model-call-faults.
func TestEscalateForFaultMapsToTaxonomy(t *testing.T) {
	cases := []struct {
		fault   modeltypes.FaultKind
		trigger Trigger
	}{
		{modeltypes.FaultContextOverflow, TriggerRetryExhaustion},
		{modeltypes.FaultTimeout, TriggerRetryExhaustion},
		{modeltypes.FaultMalformedToolCall, TriggerParseFailure},
	}
	for _, c := range cases {
		r := New(stub{"qwen", true}, stub{"haiku", true})
		edge := r.EscalateForFault(c.fault)
		if edge.Direction != EdgeEscalate {
			t.Fatalf("%s: direction = %q, want escalate", c.fault, edge.Direction)
		}
		if edge.Trigger != c.trigger {
			t.Errorf("%s: trigger = %q, want %q", c.fault, edge.Trigger, c.trigger)
		}
		if edge.FromModel != "qwen" || edge.ToModel != "haiku" {
			t.Errorf("%s: edge %s→%s, want qwen→haiku", c.fault, edge.FromModel, edge.ToModel)
		}
		if r.State() != StateEscalated {
			t.Errorf("%s: router should be escalated after a fault climb", c.fault)
		}
	}
}

// TestEscalateForFaultAtTopRungIsNoop: with no higher rung the fault escalation
// yields EdgeNone (the loop then falls back to local recovery or a clear failure).
func TestEscalateForFaultAtTopRungIsNoop(t *testing.T) {
	// Single-rung ladder: cur is already the top.
	r := NewLadder([]modeltypes.Adapter{stub{"qwen", true}}, 0)
	if edge := r.EscalateForFault(modeltypes.FaultContextOverflow); edge.Direction != EdgeNone {
		t.Errorf("single-rung fault escalation = %q, want none", edge.Direction)
	}

	// Two-rung ladder already at the top stays put.
	r2 := New(stub{"qwen", true}, stub{"haiku", true})
	r2.EscalateForFault(modeltypes.FaultTimeout) // climb to top
	if edge := r2.EscalateForFault(modeltypes.FaultTimeout); edge.Direction != EdgeNone {
		t.Errorf("top-rung fault escalation = %q, want none", edge.Direction)
	}
}

// TestEscalateForFaultUnknownKind: an unrecognised fault maps to no trigger and
// does not climb.
func TestEscalateForFaultUnknownKind(t *testing.T) {
	r := New(stub{"qwen", true}, stub{"haiku", true})
	if edge := r.EscalateForFault(modeltypes.FaultNone); edge.Direction != EdgeNone {
		t.Errorf("unknown fault escalation = %q, want none", edge.Direction)
	}
	if r.State() != StateCheap {
		t.Error("an unknown fault must not move the router off its floor")
	}
}
