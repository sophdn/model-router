package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/sophdn/model-router/modeltypes"
	"github.com/sophdn/model-router/tool"
)

// defaultTimeout is the per-call transport timeout used when an adapter is
// constructed without its own *http.Client. Model completions can run long, so it
// is generous; a caller wanting tighter bounds injects its own client.
const defaultTimeout = 120 * time.Second

// defaultClient returns c when non-nil, else a fresh *http.Client with the default
// timeout. Adapters route every request through this so a test can inject an
// httptest server's client.
func defaultClient(c *http.Client) *http.Client {
	if c != nil {
		return c
	}
	return &http.Client{Timeout: defaultTimeout}
}

// decodeToolArgs turns a tool-call arguments blob (raw JSON bytes: an
// OpenAI-compatible function.arguments string's contents, or an Anthropic
// tool_use input object) into the Action/Params envelope carried by a tool.Call.
//
// Envelope rule: when the decoded value is an object with a string "action"
// and/or a map "params", those fields ARE the envelope (Action from "action",
// Params from "params"). Otherwise the whole decoded object is the params. Empty
// input yields an empty action and nil params. Bytes that are present but not
// valid JSON yield a *FaultError of FaultMalformedToolCall — the signal the loop
// re-prompts on.
func decodeToolArgs(raw []byte) (action string, params map[string]any, err error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return "", nil, nil
	}
	var v any
	if e := json.Unmarshal(trimmed, &v); e != nil {
		return "", nil, faultErr(modeltypes.FaultMalformedToolCall, fmt.Errorf("decode tool-call arguments: %w", e))
	}
	// Args can arrive double-encoded as a JSON string carrying the real blob — an
	// OpenAI-compatible endpoint returns function.arguments as a string, and some
	// gateways forward an Anthropic tool_use input the same way. Unwrap one level:
	// a string whose contents are not valid JSON is a malformed tool call.
	if s, ok := v.(string); ok {
		var inner any
		if e := json.Unmarshal([]byte(s), &inner); e != nil {
			return "", nil, faultErr(modeltypes.FaultMalformedToolCall, fmt.Errorf("decode nested tool-call arguments: %w", e))
		}
		v = inner
	}
	obj, ok := v.(map[string]any)
	if !ok {
		// Valid JSON but not an object (an array or scalar): stash it whole so the
		// call still carries the payload rather than dropping it.
		return "", map[string]any{"value": v}, nil
	}
	action, params = envelopeFromObject(obj)
	return action, params, nil
}

// envelopeFromObject applies the Action/Params envelope rule to a decoded object.
func envelopeFromObject(obj map[string]any) (action string, params map[string]any) {
	envelope := false
	if a, ok := obj["action"].(string); ok {
		action = a
		envelope = true
	}
	if p, ok := obj["params"].(map[string]any); ok {
		params = p
		envelope = true
	}
	if !envelope {
		params = obj
	}
	return action, params
}

// encodeToolArgs is the inverse of decodeToolArgs: it serializes a prior
// tool.Call's Action/Params back into the arguments JSON an assistant turn
// re-sends. When Action is set it emits the {action, params} envelope; otherwise
// it emits the bare params object.
func encodeToolArgs(c tool.Call) json.RawMessage {
	if c.Action == "" {
		if c.Params == nil {
			return json.RawMessage("{}")
		}
		b, err := json.Marshal(c.Params)
		if err != nil {
			return json.RawMessage("{}")
		}
		return b
	}
	env := map[string]any{"action": c.Action}
	if c.Params != nil {
		env["params"] = c.Params
	}
	b, err := json.Marshal(env)
	if err != nil {
		return json.RawMessage("{}")
	}
	return b
}

// classifyRequestErr maps a client.Do transport error onto a fault: a deadline or
// a net timeout becomes FaultTimeout; anything else is returned wrapped, so its
// FaultOf is FaultNone.
func classifyRequestErr(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return faultErr(modeltypes.FaultTimeout, err)
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return faultErr(modeltypes.FaultTimeout, err)
	}
	return fmt.Errorf("request failed: %w", err)
}

// bodySnippet trims a response body to a bounded, log-safe excerpt.
func bodySnippet(body []byte) string {
	const max = 512
	s := bytes.TrimSpace(body)
	if len(s) > max {
		return string(s[:max]) + "…"
	}
	return string(s)
}
