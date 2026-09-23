package adapters

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sophdn/model-router/modeltypes"
	"github.com/sophdn/model-router/tool"
)

// newOAI wires an OpenAICompat adapter to a test server and its client.
func newOAI(srv *httptest.Server) *OpenAICompat {
	return &OpenAICompat{
		BaseURL:    srv.URL + "/v1",
		ModelID:    "test-model",
		HTTPClient: srv.Client(),
	}
}

func TestOpenAICompatTextTurn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q, want /v1/chat/completions", r.URL.Path)
		}
		io.WriteString(w, `{
			"choices":[{"message":{"content":"hello there"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":11,"completion_tokens":3}
		}`)
	}))
	defer srv.Close()

	resp, err := newOAI(srv).Complete(context.Background(),
		[]modeltypes.ChatMessage{{Role: modeltypes.RoleUser, Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != "hello there" {
		t.Errorf("Text = %q, want %q", resp.Text, "hello there")
	}
	if resp.StopReason != modeltypes.StopEndTurn {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, modeltypes.StopEndTurn)
	}
	if resp.Usage.InputTokens != 11 || resp.Usage.OutputTokens != 3 {
		t.Errorf("Usage = %+v, want In=11 Out=3", resp.Usage)
	}
	if resp.Model != "test-model" {
		t.Errorf("Model = %q, want test-model", resp.Model)
	}
}

func TestOpenAICompatToolCallTurn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{
			"choices":[{"message":{"content":"","tool_calls":[
				{"id":"call_1","function":{"name":"fs","arguments":"{\"action\":\"read\",\"params\":{\"path\":\"/x\"}}"}}
			]},"finish_reason":"tool_calls"}],
			"usage":{"prompt_tokens":20,"completion_tokens":7}
		}`)
	}))
	defer srv.Close()

	specs := []tool.Spec{{Name: "fs", Description: "filesystem", InputSchema: map[string]any{"type": "object"}}}
	resp, err := newOAI(srv).Complete(context.Background(),
		[]modeltypes.ChatMessage{{Role: modeltypes.RoleUser, Content: "read /x"}}, specs)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.StopReason != modeltypes.StopToolUse {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, modeltypes.StopToolUse)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %d, want 1", len(resp.ToolCalls))
	}
	c := resp.ToolCalls[0]
	if c.ID != "call_1" || c.Surface != "fs" || c.Action != "read" {
		t.Errorf("call = %+v, want ID=call_1 Surface=fs Action=read", c)
	}
	if c.Params["path"] != "/x" {
		t.Errorf("params[path] = %v, want /x", c.Params["path"])
	}
}

func TestOpenAICompatRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"error":{"message":"slow down","type":"rate_limit_error"}}`)
	}))
	defer srv.Close()

	_, err := newOAI(srv).Complete(context.Background(),
		[]modeltypes.ChatMessage{{Role: modeltypes.RoleUser, Content: "hi"}}, nil)
	if got := FaultOf(err); got != modeltypes.FaultRateLimit {
		t.Errorf("FaultOf = %q, want %q (err=%v)", got, modeltypes.FaultRateLimit, err)
	}
}

func TestOpenAICompatContextOverflow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":{"message":"This model's maximum context length is 8192 tokens","code":"context_length_exceeded"}}`)
	}))
	defer srv.Close()

	_, err := newOAI(srv).Complete(context.Background(),
		[]modeltypes.ChatMessage{{Role: modeltypes.RoleUser, Content: "hi"}}, nil)
	if got := FaultOf(err); got != modeltypes.FaultContextOverflow {
		t.Errorf("FaultOf = %q, want %q (err=%v)", got, modeltypes.FaultContextOverflow, err)
	}
}

func TestOpenAICompatMalformedToolArgs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{
			"choices":[{"message":{"tool_calls":[
				{"id":"call_1","function":{"name":"fs","arguments":"{not valid json"}}
			]},"finish_reason":"tool_calls"}],
			"usage":{"prompt_tokens":5,"completion_tokens":5}
		}`)
	}))
	defer srv.Close()

	_, err := newOAI(srv).Complete(context.Background(),
		[]modeltypes.ChatMessage{{Role: modeltypes.RoleUser, Content: "hi"}}, nil)
	if got := FaultOf(err); got != modeltypes.FaultMalformedToolCall {
		t.Errorf("FaultOf = %q, want %q (err=%v)", got, modeltypes.FaultMalformedToolCall, err)
	}
}

func TestOpenAICompatUnexpectedStatusIsFaultNone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, `{"error":{"message":"boom"}}`)
	}))
	defer srv.Close()

	_, err := newOAI(srv).Complete(context.Background(),
		[]modeltypes.ChatMessage{{Role: modeltypes.RoleUser, Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if got := FaultOf(err); got != modeltypes.FaultNone {
		t.Errorf("FaultOf = %q, want %q", got, modeltypes.FaultNone)
	}
}
