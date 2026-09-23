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

// OpenAICompat drives any OpenAI-compatible chat-completions endpoint — a local
// llama.cpp server, an OpenAI-shaped gateway, or the OpenAI API itself. It POSTs
// {BaseURL}/chat/completions.
type OpenAICompat struct {
	// BaseURL is the API root including any version segment, e.g.
	// "http://localhost:8081/v1". No trailing slash.
	BaseURL string
	// ModelID is the model id sent in the request and reported by Model. It cannot
	// be named Model: the Adapter interface reserves that name for the accessor
	// method, and Go forbids a field and method sharing a name.
	ModelID string
	// APIKey is optional; sent as "Authorization: Bearer <key>" when non-empty.
	APIKey string
	// HTTPClient is injectable for tests; nil means a default-timeout client.
	HTTPClient *http.Client
}

var _ modeltypes.Adapter = (*OpenAICompat)(nil)

// Model returns the configured model id.
func (a *OpenAICompat) Model() string { return a.ModelID }

// Available reports true for a constructed adapter (no live health probe).
func (a *OpenAICompat) Available() bool { return true }

// --- wire types ---

type oaiRequest struct {
	Model    string       `json:"model"`
	Messages []oaiMessage `json:"messages"`
	Tools    []oaiTool    `json:"tools,omitempty"`
}

type oaiMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCalls  []oaiToolCallReq `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type oaiToolCallReq struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Function oaiFunctionCall `json:"function"`
}

type oaiFunctionCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type oaiTool struct {
	Type     string          `json:"type"`
	Function oaiFunctionSpec `json:"function"`
}

type oaiFunctionSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type oaiResponse struct {
	Choices []struct {
		Message struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Function struct {
					Name string `json:"name"`
					// Arguments is a JSON-encoded STRING per the OpenAI spec.
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

type oaiErrorBody struct {
	Error struct {
		Message string `json:"message"`
		Code    string `json:"code"`
		Type    string `json:"type"`
	} `json:"error"`
}

// Complete runs one turn against the OpenAI-compatible endpoint.
func (a *OpenAICompat) Complete(ctx context.Context, messages []modeltypes.ChatMessage, tools []tool.Spec) (modeltypes.Response, error) {
	reqBody := oaiRequest{
		Model:    a.ModelID,
		Messages: oaiMessages(messages),
		Tools:    oaiTools(tools),
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return modeltypes.Response{}, fmt.Errorf("marshal request: %w", err)
	}

	url := strings.TrimRight(a.BaseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return modeltypes.Response{}, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if a.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+a.APIKey)
	}

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

	var parsed oaiResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return modeltypes.Response{}, fmt.Errorf("decode response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return modeltypes.Response{}, fmt.Errorf("response carried no choices")
	}
	choice := parsed.Choices[0]

	var calls []tool.Call
	for _, tc := range choice.Message.ToolCalls {
		action, params, err := decodeToolArgs([]byte(tc.Function.Arguments))
		if err != nil {
			return modeltypes.Response{}, err
		}
		calls = append(calls, tool.Call{
			ID:      tc.ID,
			Surface: tc.Function.Name,
			Action:  action,
			Params:  params,
		})
	}

	return modeltypes.Response{
		Model:      a.ModelID,
		Text:       choice.Message.Content,
		ToolCalls:  calls,
		StopReason: oaiStopReason(choice.FinishReason),
		Usage: modeltypes.Usage{
			InputTokens:  parsed.Usage.PromptTokens,
			OutputTokens: parsed.Usage.CompletionTokens,
		},
	}, nil
}

// classifyHTTP maps a non-2xx status onto a fault or a plain error.
func (a *OpenAICompat) classifyHTTP(status int, body []byte) error {
	if status == http.StatusTooManyRequests {
		return faultErr(modeltypes.FaultRateLimit, fmt.Errorf("rate limited (429): %s", bodySnippet(body)))
	}
	if status == http.StatusBadRequest {
		var eb oaiErrorBody
		_ = json.Unmarshal(body, &eb)
		if openAIContextOverflow(eb.Error.Code, eb.Error.Message) {
			return faultErr(modeltypes.FaultContextOverflow, fmt.Errorf("context overflow (400): %s", bodySnippet(body)))
		}
	}
	return fmt.Errorf("openai-compat: unexpected status %d: %s", status, bodySnippet(body))
}

// openAIContextOverflow reports whether a 400 error body names a context-window
// overflow.
func openAIContextOverflow(code, msg string) bool {
	if code == "context_length_exceeded" {
		return true
	}
	m := strings.ToLower(msg)
	return strings.Contains(m, "context length") || strings.Contains(m, "maximum context")
}

// oaiStopReason maps an OpenAI finish_reason onto the modeltypes stop taxonomy.
func oaiStopReason(finish string) string {
	switch finish {
	case "tool_calls":
		return modeltypes.StopToolUse
	case "length":
		return modeltypes.StopMaxTokens
	case "stop":
		return modeltypes.StopEndTurn
	default:
		return modeltypes.StopEndTurn
	}
}

// oaiMessages maps the transcript onto OpenAI chat messages.
func oaiMessages(messages []modeltypes.ChatMessage) []oaiMessage {
	out := make([]oaiMessage, 0, len(messages))
	for _, m := range messages {
		switch m.Role {
		case modeltypes.RoleAssistant:
			msg := oaiMessage{Role: "assistant", Content: m.Content}
			for _, c := range m.ToolCalls {
				msg.ToolCalls = append(msg.ToolCalls, oaiToolCallReq{
					ID:   c.ID,
					Type: "function",
					Function: oaiFunctionCall{
						Name:      c.Surface,
						Arguments: encodeToolArgs(c),
					},
				})
			}
			out = append(out, msg)
		case modeltypes.RoleTool:
			out = append(out, oaiMessage{
				Role:       "tool",
				Content:    m.Content,
				ToolCallID: m.ToolCallID,
			})
		default:
			// system, user, and any other role pass through as {role, content}.
			out = append(out, oaiMessage{Role: m.Role, Content: m.Content})
		}
	}
	return out
}

// oaiTools maps the offered specs onto OpenAI function tools.
func oaiTools(specs []tool.Spec) []oaiTool {
	if len(specs) == 0 {
		return nil
	}
	out := make([]oaiTool, 0, len(specs))
	for _, s := range specs {
		out = append(out, oaiTool{
			Type: "function",
			Function: oaiFunctionSpec{
				Name:        s.Name,
				Description: s.Description,
				Parameters:  s.InputSchema,
			},
		})
	}
	return out
}
