package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sophdn/model-router/modeltypes"
	"github.com/sophdn/model-router/tool"
)

func TestDefaultClient(t *testing.T) {
	// A nil client yields a fresh default-timeout client.
	c := defaultClient(nil)
	if c == nil {
		t.Fatal("defaultClient(nil) = nil, want a client")
	}
	if c.Timeout != defaultTimeout {
		t.Errorf("default timeout = %v, want %v", c.Timeout, defaultTimeout)
	}
	// A supplied client is passed through unchanged.
	injected := &http.Client{Timeout: 5 * time.Second}
	if got := defaultClient(injected); got != injected {
		t.Errorf("defaultClient(injected) returned a different client")
	}
}

// timeoutErr is a net.Error that reports itself as a timeout.
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

var _ net.Error = timeoutErr{}

func TestClassifyRequestErr(t *testing.T) {
	// A context deadline maps onto FaultTimeout.
	if got := FaultOf(classifyRequestErr(context.DeadlineExceeded)); got != modeltypes.FaultTimeout {
		t.Errorf("deadline: FaultOf = %q, want %q", got, modeltypes.FaultTimeout)
	}
	// A wrapped net.Error that reports Timeout() also maps onto FaultTimeout.
	wrapped := fmt.Errorf("dialing: %w", timeoutErr{})
	if got := FaultOf(classifyRequestErr(wrapped)); got != modeltypes.FaultTimeout {
		t.Errorf("net timeout: FaultOf = %q, want %q", got, modeltypes.FaultTimeout)
	}
	// A non-timeout transport error is wrapped as a plain (FaultNone) error.
	plain := classifyRequestErr(errors.New("connection refused"))
	if got := FaultOf(plain); got != modeltypes.FaultNone {
		t.Errorf("plain: FaultOf = %q, want %q", got, modeltypes.FaultNone)
	}
	if !strings.Contains(plain.Error(), "connection refused") {
		t.Errorf("plain error = %q, want it to carry the cause", plain.Error())
	}
}

func TestBodySnippet(t *testing.T) {
	// A short body passes through trimmed.
	if got := bodySnippet([]byte("  hello  ")); got != "hello" {
		t.Errorf("bodySnippet(short) = %q, want %q", got, "hello")
	}
	// A body over the cap is truncated and ellipsized.
	long := strings.Repeat("x", 600)
	got := bodySnippet([]byte(long))
	if !strings.HasSuffix(got, "…") {
		t.Errorf("bodySnippet(long) = %q, want a trailing ellipsis", got)
	}
	// 512 runes of content + the one-rune ellipsis.
	if want := 512 + len("…"); len(got) != want {
		t.Errorf("bodySnippet(long) length = %d, want %d", len(got), want)
	}
}

func TestFaultErrorUnwrap(t *testing.T) {
	inner := errors.New("underlying")
	fe := faultErr(modeltypes.FaultTimeout, inner)
	if !errors.Is(fe, inner) {
		t.Error("errors.Is could not reach the wrapped error via Unwrap")
	}
	if fe.Unwrap() != inner {
		t.Error("Unwrap did not return the wrapped error")
	}
}

func TestDecodeToolArgsNonObject(t *testing.T) {
	// Valid JSON that is not an object is stashed whole under "value" rather than
	// being dropped, so the call still carries its payload.
	action, params, err := decodeToolArgs([]byte(`[1,2,3]`))
	if err != nil {
		t.Fatalf("decodeToolArgs(array) error = %v", err)
	}
	if action != "" {
		t.Errorf("action = %q, want empty", action)
	}
	if fmt.Sprint(params["value"]) != fmt.Sprint([]any{float64(1), float64(2), float64(3)}) {
		t.Errorf("params[value] = %v, want the array", params["value"])
	}

	// A bare scalar is stashed the same way.
	_, params, err = decodeToolArgs([]byte(`42`))
	if err != nil {
		t.Fatalf("decodeToolArgs(scalar) error = %v", err)
	}
	if params["value"] != float64(42) {
		t.Errorf("params[value] = %v, want 42", params["value"])
	}
}

func TestDecodeToolArgsDoubleEncoded(t *testing.T) {
	// A JSON string carrying a real envelope is unwrapped one level.
	action, params, err := decodeToolArgs([]byte(`"{\"action\":\"read\",\"params\":{\"path\":\"/x\"}}"`))
	if err != nil {
		t.Fatalf("decodeToolArgs(double-encoded) error = %v", err)
	}
	if action != "read" {
		t.Errorf("action = %q, want read", action)
	}
	if params["path"] != "/x" {
		t.Errorf("params[path] = %v, want /x", params["path"])
	}
}

func TestEncodeToolArgs(t *testing.T) {
	cases := []struct {
		name string
		call tool.Call
		want string
	}{
		{
			name: "action and params emit the envelope",
			call: tool.Call{Action: "read", Params: map[string]any{"path": "/x"}},
			want: `{"action":"read","params":{"path":"/x"}}`,
		},
		{
			name: "action alone emits an envelope with no params",
			call: tool.Call{Action: "list"},
			want: `{"action":"list"}`,
		},
		{
			name: "params without an action emit the bare object",
			call: tool.Call{Params: map[string]any{"path": "/x"}},
			want: `{"path":"/x"}`,
		},
		{
			name: "empty call emits an empty object",
			call: tool.Call{},
			want: `{}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := encodeToolArgs(c.call)
			// Compare structurally so field order does not matter.
			var gotV, wantV any
			if err := json.Unmarshal(got, &gotV); err != nil {
				t.Fatalf("encodeToolArgs produced invalid JSON %q: %v", got, err)
			}
			if err := json.Unmarshal([]byte(c.want), &wantV); err != nil {
				t.Fatalf("bad want literal: %v", err)
			}
			if fmt.Sprint(gotV) != fmt.Sprint(wantV) {
				t.Errorf("encodeToolArgs = %s, want %s", got, c.want)
			}
		})
	}
}

// TestEncodeToolArgsUnmarshalable confirms that params which cannot be JSON-encoded
// (a channel here) degrade to an empty object rather than panicking or emitting
// broken JSON, on both the bare-params and enveloped code paths.
func TestEncodeToolArgsUnmarshalable(t *testing.T) {
	bad := map[string]any{"ch": make(chan int)}
	if got := string(encodeToolArgs(tool.Call{Params: bad})); got != "{}" {
		t.Errorf("bare unmarshalable params = %q, want {}", got)
	}
	if got := string(encodeToolArgs(tool.Call{Action: "go", Params: bad})); got != "{}" {
		t.Errorf("enveloped unmarshalable params = %q, want {}", got)
	}
}

// TestEncodeDecodeRoundTrip confirms the envelope survives a full encode→decode
// cycle: what an assistant turn re-sends decodes back to the same Action/Params.
func TestEncodeDecodeRoundTrip(t *testing.T) {
	orig := tool.Call{Action: "write", Params: map[string]any{"path": "/y", "text": "hi"}}
	raw := encodeToolArgs(orig)
	action, params, err := decodeToolArgs(raw)
	if err != nil {
		t.Fatalf("decode error = %v", err)
	}
	if action != orig.Action {
		t.Errorf("action = %q, want %q", action, orig.Action)
	}
	if params["path"] != "/y" || params["text"] != "hi" {
		t.Errorf("params = %v, want the original params", params)
	}
}
