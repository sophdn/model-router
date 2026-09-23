package adapters

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sophdn/model-router/modeltypes"
	"github.com/sophdn/model-router/tool"
)

// newAnthropic wires an Anthropic adapter to a test server and its client.
func newAnthropic(srv *httptest.Server) *Anthropic {
	return &Anthropic{
		APIKey:     "test-key",
		ModelID:    "claude-test",
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
	}
}

func TestAnthropicTextTurn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %q, want /v1/messages", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("x-api-key = %q, want test-key", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != anthropicVersion {
			t.Errorf("anthropic-version = %q, want %q", r.Header.Get("anthropic-version"), anthropicVersion)
		}
		// Verify system messages were hoisted and max_tokens defaulted.
		var body antRequest
		json.NewDecoder(r.Body).Decode(&body)
		if body.System != "be terse" {
			t.Errorf("system = %q, want %q", body.System, "be terse")
		}
		if body.MaxTokens != anthropicDefaultMaxTokens {
			t.Errorf("max_tokens = %d, want %d", body.MaxTokens, anthropicDefaultMaxTokens)
		}
		io.WriteString(w, `{
			"content":[{"type":"text","text":"hi "},{"type":"text","text":"there"}],
			"stop_reason":"end_turn",
			"usage":{"input_tokens":30,"output_tokens":4,"cache_read_input_tokens":10,"cache_creation_input_tokens":6}
		}`)
	}))
	defer srv.Close()

	resp, err := newAnthropic(srv).Complete(context.Background(),
		[]modeltypes.ChatMessage{
			{Role: modeltypes.RoleSystem, Content: "be terse"},
			{Role: modeltypes.RoleUser, Content: "hi"},
		}, nil)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != "hi there" {
		t.Errorf("Text = %q, want %q", resp.Text, "hi there")
	}
	if resp.StopReason != modeltypes.StopEndTurn {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, modeltypes.StopEndTurn)
	}
	if resp.Usage.InputTokens != 30 || resp.Usage.OutputTokens != 4 ||
		resp.Usage.CachedInputTokens != 10 || resp.Usage.CacheWriteTokens != 6 {
		t.Errorf("Usage = %+v, want In=30 Out=4 Cached=10 CacheWrite=6", resp.Usage)
	}
}

func TestAnthropicToolCallTurn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{
			"content":[
				{"type":"text","text":"let me look"},
				{"type":"tool_use","id":"toolu_1","name":"fs","input":{"action":"read","params":{"path":"/y"}}}
			],
			"stop_reason":"tool_use",
			"usage":{"input_tokens":40,"output_tokens":9}
		}`)
	}))
	defer srv.Close()

	specs := []tool.Spec{{Name: "fs", Description: "filesystem", InputSchema: map[string]any{"type": "object"}}}
	resp, err := newAnthropic(srv).Complete(context.Background(),
		[]modeltypes.ChatMessage{{Role: modeltypes.RoleUser, Content: "read /y"}}, specs)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != "let me look" {
		t.Errorf("Text = %q, want %q", resp.Text, "let me look")
	}
	if resp.StopReason != modeltypes.StopToolUse {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, modeltypes.StopToolUse)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %d, want 1", len(resp.ToolCalls))
	}
	c := resp.ToolCalls[0]
	if c.ID != "toolu_1" || c.Surface != "fs" || c.Action != "read" {
		t.Errorf("call = %+v, want ID=toolu_1 Surface=fs Action=read", c)
	}
	if c.Params["path"] != "/y" {
		t.Errorf("params[path] = %v, want /y", c.Params["path"])
	}
}

func TestAnthropicRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`)
	}))
	defer srv.Close()

	_, err := newAnthropic(srv).Complete(context.Background(),
		[]modeltypes.ChatMessage{{Role: modeltypes.RoleUser, Content: "hi"}}, nil)
	if got := FaultOf(err); got != modeltypes.FaultRateLimit {
		t.Errorf("FaultOf = %q, want %q (err=%v)", got, modeltypes.FaultRateLimit, err)
	}
}

func TestAnthropicContextOverflow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: 200000 tokens > 199999 maximum"}}`)
	}))
	defer srv.Close()

	_, err := newAnthropic(srv).Complete(context.Background(),
		[]modeltypes.ChatMessage{{Role: modeltypes.RoleUser, Content: "hi"}}, nil)
	if got := FaultOf(err); got != modeltypes.FaultContextOverflow {
		t.Errorf("FaultOf = %q, want %q (err=%v)", got, modeltypes.FaultContextOverflow, err)
	}
}

func TestAnthropicMalformedToolArgs(t *testing.T) {
	// A tool_use input that is a valid JSON object always decodes, and a well-formed
	// HTTP body cannot carry an invalid JSON sub-value. The realistic malformed case
	// is a double-encoded input: a JSON STRING whose contents are not valid JSON
	// (some gateways forward args this way). decodeToolArgs unwraps the string and
	// faults on the bad inner blob.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{
			"content":[
				{"type":"tool_use","id":"toolu_1","name":"fs","input":"{not valid json"}
			],
			"stop_reason":"tool_use",
			"usage":{"input_tokens":5,"output_tokens":5}
		}`)
	}))
	defer srv.Close()

	_, err := newAnthropic(srv).Complete(context.Background(),
		[]modeltypes.ChatMessage{{Role: modeltypes.RoleUser, Content: "hi"}}, nil)
	if got := FaultOf(err); got != modeltypes.FaultMalformedToolCall {
		t.Errorf("FaultOf = %q, want %q (err=%v)", got, modeltypes.FaultMalformedToolCall, err)
	}
}
