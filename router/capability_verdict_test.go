package router

import (
	"testing"

	"github.com/sophdn/model-router/modeltypes"
)

func TestWithCapabilityVerdictDoNotRoute(t *testing.T) {
	floor := modeltypes.NewEcho("cheap", modeltypes.Response{StopReason: modeltypes.StopEndTurn})
	mid := modeltypes.NewEcho("mid", modeltypes.Response{StopReason: modeltypes.StopEndTurn})
	strong := modeltypes.NewEcho("strong", modeltypes.Response{StopReason: modeltypes.StopEndTurn})

	r := NewLadder(
		[]modeltypes.Adapter{floor, mid, strong}, 0,
		WithCapabilityVerdict("do-not-route"),
	)

	if r.CurrentRung() != 1 {
		t.Errorf("cur = %d, want 1 (lifted past floor)", r.CurrentRung())
	}
	if !r.CapabilityLiftApplied() {
		t.Error("expected CapabilityLiftApplied to be true")
	}
	if r.CapabilityVerdict() != "do-not-route" {
		t.Errorf("verdict = %q, want do-not-route", r.CapabilityVerdict())
	}
}

func TestWithCapabilityVerdictRouteFirst(t *testing.T) {
	floor := modeltypes.NewEcho("cheap", modeltypes.Response{StopReason: modeltypes.StopEndTurn})
	strong := modeltypes.NewEcho("strong", modeltypes.Response{StopReason: modeltypes.StopEndTurn})

	r := New(floor, strong, WithCapabilityVerdict("route-first"))

	if r.CurrentRung() != 0 {
		t.Errorf("cur = %d, want 0 (no lift for route-first)", r.CurrentRung())
	}
	if r.CapabilityLiftApplied() {
		t.Error("expected no lift for route-first")
	}
}

func TestWithCapabilityVerdictEmpty(t *testing.T) {
	floor := modeltypes.NewEcho("cheap", modeltypes.Response{StopReason: modeltypes.StopEndTurn})
	strong := modeltypes.NewEcho("strong", modeltypes.Response{StopReason: modeltypes.StopEndTurn})

	r := New(floor, strong, WithCapabilityVerdict(""))

	if r.CurrentRung() != 0 {
		t.Errorf("cur = %d, want 0 (no lift for empty verdict)", r.CurrentRung())
	}
}

func TestWithCapabilityVerdictSingleTier(t *testing.T) {
	floor := modeltypes.NewEcho("cheap", modeltypes.Response{StopReason: modeltypes.StopEndTurn})

	r := NewLadder(
		[]modeltypes.Adapter{floor}, 0,
		WithCapabilityVerdict("do-not-route"),
	)

	if r.CurrentRung() != 0 {
		t.Errorf("cur = %d, want 0 (no lift on single-tier)", r.CurrentRung())
	}
	if r.CapabilityLiftApplied() {
		t.Error("expected no lift on single-tier router")
	}
}

func TestWithCapabilityVerdictRespectsTopBound(t *testing.T) {
	floor := modeltypes.NewEcho("cheap", modeltypes.Response{StopReason: modeltypes.StopEndTurn})
	strong := modeltypes.NewEcho("strong", modeltypes.Response{StopReason: modeltypes.StopEndTurn})

	r := New(floor, strong,
		WithBoundedTop(2),
		WithCapabilityVerdict("do-not-route"),
	)

	if r.CapabilityLiftApplied() {
		t.Error("lift should not reach the bounded top rung")
	}
	if r.CurrentRung() != 0 {
		t.Errorf("cur = %d, want 0 (lift blocked by bound)", r.CurrentRung())
	}
}

func TestWithCapabilityVerdictRouteWithFallback(t *testing.T) {
	floor := modeltypes.NewEcho("cheap", modeltypes.Response{StopReason: modeltypes.StopEndTurn})
	mid := modeltypes.NewEcho("mid", modeltypes.Response{StopReason: modeltypes.StopEndTurn})

	r := NewLadder(
		[]modeltypes.Adapter{floor, mid}, 0,
		WithCapabilityVerdict("route-with-fallback"),
	)

	if r.CurrentRung() != 0 {
		t.Errorf("cur = %d, want 0 (no lift for route-with-fallback)", r.CurrentRung())
	}
}
