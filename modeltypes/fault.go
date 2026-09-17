package modeltypes

// FaultKind classifies a recoverable model-call failure — the fault classes the
// agent loop can absorb (compact-and-retry, re-prompt, escalate, or end the turn
// gracefully) instead of aborting the whole run. FaultNone means the error is not
// a recognised recoverable fault and the caller should treat it as fatal.
//
// This package carries only the pure fault taxonomy. Turning a concrete transport
// error into one of these kinds is provider-specific work that belongs in an
// adapter, so it lives behind the Adapter seam rather than here. Keeping the
// taxonomy free of net/http is what lets the router and the ladder policy stay
// sans-IO and dependency-free.
type FaultKind string

const (
	// FaultNone is "not a recoverable model-call fault".
	FaultNone FaultKind = ""
	// FaultContextOverflow is the provider rejecting a prompt that exceeds the
	// model's context window. Recovery: compact the transcript and retry, or
	// escalate to a larger-window rung.
	FaultContextOverflow FaultKind = "context_overflow"
	// FaultMalformedToolCall is the model emitting a truncated or invalid tool-call
	// arguments blob the adapter cannot decode. Recovery: a bounded corrective
	// re-prompt, then escalate.
	FaultMalformedToolCall FaultKind = "malformed_tool_call"
	// FaultTimeout is a model call exceeding its deadline (the per-call transport
	// timeout, or the per-turn context deadline). Recovery: shrink the prompt and
	// retry or escalate while turn budget remains; end the turn gracefully once the
	// turn deadline itself is spent.
	FaultTimeout FaultKind = "timeout"
	// FaultRateLimit is the provider throttling the caller. It is transient and
	// recoverable: back off and retry the same rung, then de-escalate to the free
	// local floor for the turn rather than retrying a throttled paid rung. Unlike
	// the other faults it never escalates UP — climbing into a more-rate-limited
	// rung is the wrong move.
	FaultRateLimit FaultKind = "rate_limit"
)
