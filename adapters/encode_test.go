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

func TestAdapterAccessors(t *testing.T) {
	oai := &OpenAICompat{ModelID: "local-model"}
	if oai.Model() != "local-model" {
		t.Errorf("OpenAICompat.Model() = %q, want local-model", oai.Model())
	}
	if !oai.Available() {
		t.Error("OpenAICompat.Available() = false, want true")
	}
	ant := &Anthropic{ModelID: "claude-x"}
	if ant.Model() != "claude-x" {
		t.Errorf("Anthropic.Model() = %q, want claude-x", ant.Model())
	}
	if !ant.Available() {
		t.Error("Anthropic.Available() = false, want true")
	}
}

// fullTranscript is a transcript exercising every role the encoders map: a system
// prompt, a user turn, an assistant turn that both speaks and calls a tool, and the
// tool result answering that call.
func fullTranscript() []modeltypes.ChatMessage {
	return []modeltypes.ChatMessage{
		{Role: modeltypes.RoleSystem, Content: "be terse"},
		{Role: modeltypes.RoleUser, Content: "read /x"},
		{
			Role:    modeltypes.RoleAssistant,
			Content: "looking now",
			ToolCalls: []tool.Call{{
				ID:      "call_1",
				Surface: "fs",
				Action:  "read",
				Params:  map[string]any{"path": "/x"},
			}},
		},
		{Role: modeltypes.RoleTool, ToolCallID: "call_1", Name: "fs", Content: "file body"},
	}
}

func offeredTools() []tool.Spec {
	return []tool.Spec{{Name: "fs", Description: "filesystem", InputSchema: map[string]any{"type": "object"}}}
}

// TestOpenAICompatRequestEncoding drives a full transcript through Complete and
// decodes the request the adapter POSTed, verifying the OpenAI wire shape: the
// assistant tool call becomes a tool_calls entry with an {action,params} arguments
// blob, and the tool result becomes a role=tool message keyed by tool_call_id.
func TestOpenAICompatRequestEncoding(t *testing.T) {
	var got oaiRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("decode captured request: %v", err)
		}
		io.WriteString(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()

	if _, err := newOAI(srv).Complete(context.Background(), fullTranscript(), offeredTools()); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if got.Model != "test-model" {
		t.Errorf("request model = %q, want test-model", got.Model)
	}
	if len(got.Tools) != 1 || got.Tools[0].Function.Name != "fs" {
		t.Errorf("tools = %+v, want one fs tool", got.Tools)
	}
	if len(got.Messages) != 4 {
		t.Fatalf("messages = %d, want 4", len(got.Messages))
	}
	// system + user pass through as {role, content}.
	if got.Messages[0].Role != "system" || got.Messages[0].Content != "be terse" {
		t.Errorf("system message = %+v", got.Messages[0])
	}
	if got.Messages[1].Role != "user" || got.Messages[1].Content != "read /x" {
		t.Errorf("user message = %+v", got.Messages[1])
	}
	// assistant carries its prose and the tool call.
	asst := got.Messages[2]
	if asst.Role != "assistant" || asst.Content != "looking now" {
		t.Errorf("assistant message = %+v", asst)
	}
	if len(asst.ToolCalls) != 1 {
		t.Fatalf("assistant tool_calls = %d, want 1", len(asst.ToolCalls))
	}
	tc := asst.ToolCalls[0]
	if tc.ID != "call_1" || tc.Type != "function" || tc.Function.Name != "fs" {
		t.Errorf("tool call = %+v", tc)
	}
	action, params, err := decodeToolArgs(tc.Function.Arguments)
	if err != nil {
		t.Fatalf("re-decode arguments: %v", err)
	}
	if action != "read" || params["path"] != "/x" {
		t.Errorf("encoded arguments decoded to action=%q params=%v, want read /x", action, params)
	}
	// tool result becomes a role=tool message keyed by the call id.
	tr := got.Messages[3]
	if tr.Role != "tool" || tr.ToolCallID != "call_1" || tr.Content != "file body" {
		t.Errorf("tool result message = %+v", tr)
	}
}

// TestAnthropicRequestEncoding drives the same transcript through the Anthropic
// adapter and verifies its distinct wire shape: system messages hoisted to the
// top-level field, the assistant turn as a text+tool_use block array, and the tool
// result as a user message carrying a tool_result block.
func TestAnthropicRequestEncoding(t *testing.T) {
	var got antRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("decode captured request: %v", err)
		}
		io.WriteString(w, `{"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`)
	}))
	defer srv.Close()

	if _, err := newAnthropic(srv).Complete(context.Background(), fullTranscript(), offeredTools()); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if got.System != "be terse" {
		t.Errorf("system = %q, want hoisted 'be terse'", got.System)
	}
	if len(got.Tools) != 1 || got.Tools[0].Name != "fs" {
		t.Errorf("tools = %+v, want one fs tool", got.Tools)
	}
	// System is hoisted out, so only user + assistant + tool-result remain.
	if len(got.Messages) != 3 {
		t.Fatalf("messages = %d, want 3 (system hoisted)", len(got.Messages))
	}
	if got.Messages[0].Role != "user" {
		t.Errorf("message[0] role = %q, want user", got.Messages[0].Role)
	}
	// assistant becomes a block array: a text block then a tool_use block.
	asst := got.Messages[1]
	if asst.Role != "assistant" {
		t.Errorf("assistant role = %q", asst.Role)
	}
	blocks, ok := asst.Content.([]any)
	if !ok || len(blocks) != 2 {
		t.Fatalf("assistant content = %v, want a 2-block array", asst.Content)
	}
	textBlock := blocks[0].(map[string]any)
	if textBlock["type"] != "text" || textBlock["text"] != "looking now" {
		t.Errorf("text block = %v", textBlock)
	}
	useBlock := blocks[1].(map[string]any)
	if useBlock["type"] != "tool_use" || useBlock["name"] != "fs" || useBlock["id"] != "call_1" {
		t.Errorf("tool_use block = %v", useBlock)
	}
	// tool result becomes a user message carrying a tool_result block.
	tr := got.Messages[2]
	if tr.Role != "user" {
		t.Errorf("tool-result role = %q, want user", tr.Role)
	}
	trBlocks, ok := tr.Content.([]any)
	if !ok || len(trBlocks) != 1 {
		t.Fatalf("tool-result content = %v, want a 1-block array", tr.Content)
	}
	resBlock := trBlocks[0].(map[string]any)
	if resBlock["type"] != "tool_result" || resBlock["tool_use_id"] != "call_1" || resBlock["content"] != "file body" {
		t.Errorf("tool_result block = %v", resBlock)
	}
}

// TestAnthropicAssistantWithoutToolCalls confirms a plain assistant turn (no tool
// calls) stays a bare string, not a block array.
func TestAnthropicAssistantWithoutToolCalls(t *testing.T) {
	var got antRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &got)
		io.WriteString(w, `{"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`)
	}))
	defer srv.Close()

	msgs := []modeltypes.ChatMessage{
		{Role: modeltypes.RoleUser, Content: "hi"},
		{Role: modeltypes.RoleAssistant, Content: "hello"},
	}
	if _, err := newAnthropic(srv).Complete(context.Background(), msgs, nil); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(got.Messages))
	}
	if s, ok := got.Messages[1].Content.(string); !ok || s != "hello" {
		t.Errorf("assistant content = %v, want the bare string 'hello'", got.Messages[1].Content)
	}
}

func TestOAIStopReason(t *testing.T) {
	cases := map[string]string{
		"tool_calls": modeltypes.StopToolUse,
		"length":     modeltypes.StopMaxTokens,
		"stop":       modeltypes.StopEndTurn,
		"":           modeltypes.StopEndTurn, // unknown collapses to end_turn
	}
	for finish, want := range cases {
		if got := oaiStopReason(finish); got != want {
			t.Errorf("oaiStopReason(%q) = %q, want %q", finish, got, want)
		}
	}
}

func TestAntStopReason(t *testing.T) {
	cases := map[string]string{
		"tool_use":   modeltypes.StopToolUse,
		"max_tokens": modeltypes.StopMaxTokens,
		"end_turn":   modeltypes.StopEndTurn,
		"":           modeltypes.StopEndTurn, // unknown collapses to end_turn
	}
	for stop, want := range cases {
		if got := antStopReason(stop); got != want {
			t.Errorf("antStopReason(%q) = %q, want %q", stop, got, want)
		}
	}
}

func TestOpenAIContextOverflowByMessage(t *testing.T) {
	// A 400 with no code but a message naming the context window is still a
	// context-overflow fault (the message-substring branch).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":{"message":"Requested tokens exceed the maximum context for this model"}}`)
	}))
	defer srv.Close()

	_, err := newOAI(srv).Complete(context.Background(),
		[]modeltypes.ChatMessage{{Role: modeltypes.RoleUser, Content: "hi"}}, nil)
	if got := FaultOf(err); got != modeltypes.FaultContextOverflow {
		t.Errorf("FaultOf = %q, want %q (err=%v)", got, modeltypes.FaultContextOverflow, err)
	}
}

// TestOpenAIMaxTokensStop confirms a length-capped generation surfaces as
// StopMaxTokens through a real Complete call, not just the mapper.
func TestOpenAIMaxTokensStop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"choices":[{"message":{"content":"cut o"},"finish_reason":"length"}]}`)
	}))
	defer srv.Close()

	resp, err := newOAI(srv).Complete(context.Background(),
		[]modeltypes.ChatMessage{{Role: modeltypes.RoleUser, Content: "write an essay"}}, nil)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.StopReason != modeltypes.StopMaxTokens {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, modeltypes.StopMaxTokens)
	}
}
