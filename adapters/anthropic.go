package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/sophdn/model-router/modeltypes"
	"github.com/sophdn/model-router/tool"
)

const (
	// anthropicDefaultBaseURL is the Messages API root used when BaseURL is empty.
	anthropicDefaultBaseURL = "https://api.anthropic.com"
	// anthropicDefaultMaxTokens is the output cap used when MaxTokens is unset.
	anthropicDefaultMaxTokens = 4096
	// anthropicVersion is the required API version header value.
	anthropicVersion = "2023-06-01"
)

// Anthropic drives the Anthropic Messages API. It POSTs {BaseURL}/v1/messages.
type Anthropic struct {
	// APIKey is sent as the "x-api-key" header (required by the API).
	APIKey string
	// ModelID is the model id sent in the request and reported by Model. It cannot
	// be named Model: the Adapter interface reserves that name for the accessor
	// method, and Go forbids a field and method sharing a name.
	ModelID string
	// BaseURL defaults to anthropicDefaultBaseURL when empty. No trailing slash.
	BaseURL string
	// MaxTokens is the output token cap; defaults to anthropicDefaultMaxTokens.
	MaxTokens int
	// HTTPClient is injectable for tests; nil means a default-timeout client.
	HTTPClient *http.Client
}

var _ modeltypes.Adapter = (*Anthropic)(nil)

// Model returns the configured model id.
func (a *Anthropic) Model() string { return a.ModelID }

// Available reports true for a constructed adapter (no live health probe).
func (a *Anthropic) Available() bool { return true }

// --- wire types ---

type antRequest struct {
	Model     string       `json:"model"`
	MaxTokens int          `json:"max_tokens"`
	System    string       `json:"system,omitempty"`
	Messages  []antMessage `json:"messages"`
	Tools     []antTool    `json:"tools,omitempty"`
}

type antMessage struct {
	Role string `json:"role"`
	// Content is polymorphic: a plain string, or a slice of content blocks.
	Content any `json:"content"`
}

type antTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
}

type antResponse struct {
	Content []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

type antErrorBody struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Complete runs one turn against the Anthropic Messages API.
func (a *Anthropic) Complete(ctx context.Context, messages []modeltypes.ChatMessage, tools []tool.Spec) (modeltypes.Response, error) {
	maxTokens := a.MaxTokens
	if maxTokens <= 0 {
		maxTokens = anthropicDefaultMaxTokens
	}
	reqBody := antRequest{
		Model:     a.ModelID,
		MaxTokens: maxTokens,
		System:    antSystem(messages),
		Messages:  antMessages(messages),
		Tools:     antTools(tools),
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return modeltypes.Response{}, fmt.Errorf("marshal request: %w", err)
	}

	base := a.BaseURL
	if base == "" {
		base = anthropicDefaultBaseURL
	}
	url := strings.TrimRight(base, "/") + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return modeltypes.Response{}, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("x-api-key", a.APIKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	resp, err := defaultClient(a.HTTPClient).Do(httpReq)
	if err != nil {
		return modeltypes.Response{}, classifyRequestErr(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return modeltypes.Response{}, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return modeltypes.Response{}, a.classifyHTTP(resp.StatusCode, body)
	}

	var parsed antResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return modeltypes.Response{}, fmt.Errorf("decode response: %w", err)
	}

	var text strings.Builder
	var calls []tool.Call
	for _, block := range parsed.Content {
		switch block.Type {
		case "text":
			text.WriteString(block.Text)
		case "tool_use":
			action, params, err := decodeToolArgs(block.Input)
			if err != nil {
				return modeltypes.Response{}, err
			}
			calls = append(calls, tool.Call{
				ID:      block.ID,
				Surface: block.Name,
				Action:  action,
				Params:  params,
			})
		}
	}

	return modeltypes.Response{
		Model:      a.ModelID,
		Text:       text.String(),
		ToolCalls:  calls,
		StopReason: antStopReason(parsed.StopReason),
		Usage: modeltypes.Usage{
			InputTokens:       parsed.Usage.InputTokens,
			OutputTokens:      parsed.Usage.OutputTokens,
			CachedInputTokens: parsed.Usage.CacheReadInputTokens,
			CacheWriteTokens:  parsed.Usage.CacheCreationInputTokens,
		},
	}, nil
}

// classifyHTTP maps a non-2xx status onto a fault or a plain error.
func (a *Anthropic) classifyHTTP(status int, body []byte) error {
	if status == http.StatusTooManyRequests {
		return faultErr(modeltypes.FaultRateLimit, fmt.Errorf("rate limited (429): %s", bodySnippet(body)))
	}
	if status == http.StatusBadRequest {
		var eb antErrorBody
		_ = json.Unmarshal(body, &eb)
		if anthropicContextOverflow(eb.Error.Message) {
			return faultErr(modeltypes.FaultContextOverflow, fmt.Errorf("context overflow (400): %s", bodySnippet(body)))
		}
	}
	return fmt.Errorf("anthropic: unexpected status %d: %s", status, bodySnippet(body))
}

// anthropicContextOverflow reports whether a 400 error message names a
// context-window overflow.
func anthropicContextOverflow(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "prompt is too long") || strings.Contains(m, "too many tokens")
}

// antStopReason maps an Anthropic stop_reason onto the modeltypes stop taxonomy.
func antStopReason(stop string) string {
	switch stop {
	case "tool_use":
		return modeltypes.StopToolUse
	case "max_tokens":
		return modeltypes.StopMaxTokens
	case "end_turn":
		return modeltypes.StopEndTurn
	default:
		return modeltypes.StopEndTurn
	}
}

// antSystem concatenates every RoleSystem message into the top-level system field.
func antSystem(messages []modeltypes.ChatMessage) string {
	var parts []string
	for _, m := range messages {
		if m.Role == modeltypes.RoleSystem && m.Content != "" {
			parts = append(parts, m.Content)
		}
	}
	return strings.Join(parts, "\n\n")
}

// antMessages maps the transcript onto Anthropic messages. System messages are
// hoisted out (see antSystem). A RoleAssistant with tool calls becomes a block
// array (optional text + tool_use blocks); a RoleTool becomes a user message
// carrying a tool_result block.
func antMessages(messages []modeltypes.ChatMessage) []antMessage {
	out := make([]antMessage, 0, len(messages))
	for _, m := range messages {
		switch m.Role {
		case modeltypes.RoleSystem:
			// hoisted into the top-level system field
		case modeltypes.RoleAssistant:
			if len(m.ToolCalls) == 0 {
				out = append(out, antMessage{Role: "assistant", Content: m.Content})
				continue
			}
			blocks := make([]any, 0, len(m.ToolCalls)+1)
			if m.Content != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": m.Content})
			}
			for _, c := range m.ToolCalls {
				blocks = append(blocks, map[string]any{
					"type":  "tool_use",
					"id":    c.ID,
					"name":  c.Surface,
					"input": json.RawMessage(encodeToolArgs(c)),
				})
			}
			out = append(out, antMessage{Role: "assistant", Content: blocks})
		case modeltypes.RoleTool:
			out = append(out, antMessage{
				Role: "user",
				Content: []any{map[string]any{
					"type":        "tool_result",
					"tool_use_id": m.ToolCallID,
					"content":     m.Content,
				}},
			})
		default:
			// user and any other role: plain string content.
			out = append(out, antMessage{Role: m.Role, Content: m.Content})
		}
	}
	return out
}

// antTools maps the offered specs onto Anthropic tool definitions.
func antTools(specs []tool.Spec) []antTool {
	if len(specs) == 0 {
		return nil
	}
	out := make([]antTool, 0, len(specs))
	for _, s := range specs {
		out = append(out, antTool{
			Name:        s.Name,
			Description: s.Description,
			InputSchema: s.InputSchema,
		})
	}
	return out
}
