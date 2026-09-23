package adapters

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sophdn/model-router/modeltypes"
)

// rtFunc adapts a function into an http.RoundTripper so a test can stand in for
// the transport without opening a socket.
type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// errBody is a response body whose Read always fails, to drive the read-response
// error path.
type errBody struct{}

func (errBody) Read([]byte) (int, error) { return 0, errors.New("read failed") }
func (errBody) Close() error             { return nil }

func TestOpenAICompatDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `not json at all`)
	}))
	defer srv.Close()
	_, err := newOAI(srv).Complete(context.Background(),
		[]modeltypes.ChatMessage{{Role: modeltypes.RoleUser, Content: "hi"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "decode response") {
		t.Fatalf("err = %v, want a decode-response error", err)
	}
}

func TestOpenAICompatNoChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"choices":[]}`)
	}))
	defer srv.Close()
	_, err := newOAI(srv).Complete(context.Background(),
		[]modeltypes.ChatMessage{{Role: modeltypes.RoleUser, Content: "hi"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "no choices") {
		t.Fatalf("err = %v, want a no-choices error", err)
	}
}

func TestOpenAICompatSendsAPIKey(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		io.WriteString(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()
	a := newOAI(srv)
	a.APIKey = "sk-test"
	if _, err := a.Complete(context.Background(),
		[]modeltypes.ChatMessage{{Role: modeltypes.RoleUser, Content: "hi"}}, nil); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if gotAuth != "Bearer sk-test" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer sk-test")
	}
}

func TestOpenAICompatTransportError(t *testing.T) {
	a := &OpenAICompat{
		BaseURL: "http://example.invalid/v1",
		ModelID: "m",
		HTTPClient: &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("dial tcp: refused")
		})},
	}
	_, err := a.Complete(context.Background(),
		[]modeltypes.ChatMessage{{Role: modeltypes.RoleUser, Content: "hi"}}, nil)
	if err == nil || FaultOf(err) != modeltypes.FaultNone {
		t.Fatalf("err = %v (fault %q), want a plain transport error", err, FaultOf(err))
	}
}

func TestOpenAICompatReadError(t *testing.T) {
	a := &OpenAICompat{
		BaseURL: "http://example.invalid/v1",
		ModelID: "m",
		HTTPClient: &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: errBody{}, Header: make(http.Header)}, nil
		})},
	}
	_, err := a.Complete(context.Background(),
		[]modeltypes.ChatMessage{{Role: modeltypes.RoleUser, Content: "hi"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "read response") {
		t.Fatalf("err = %v, want a read-response error", err)
	}
}

func TestOpenAICompatBuildRequestError(t *testing.T) {
	// A control character in the URL makes http.NewRequestWithContext fail before
	// any transport is touched.
	a := &OpenAICompat{BaseURL: "http://exa\x7fmple", ModelID: "m"}
	_, err := a.Complete(context.Background(),
		[]modeltypes.ChatMessage{{Role: modeltypes.RoleUser, Content: "hi"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "build request") {
		t.Fatalf("err = %v, want a build-request error", err)
	}
}

func TestAnthropicDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `not json`)
	}))
	defer srv.Close()
	_, err := newAnthropic(srv).Complete(context.Background(),
		[]modeltypes.ChatMessage{{Role: modeltypes.RoleUser, Content: "hi"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "decode response") {
		t.Fatalf("err = %v, want a decode-response error", err)
	}
}

func TestAnthropicGenericStatusIsFaultNone(t *testing.T) {
	// A 400 that does NOT name a context overflow, and a 500, both fall through to
	// the generic "unexpected status" (FaultNone) branch.
	for _, tc := range []struct {
		status int
		body   string
	}{
		{http.StatusBadRequest, `{"type":"error","error":{"type":"invalid_request_error","message":"bad tool schema"}}`},
		{http.StatusInternalServerError, `{"type":"error","error":{"message":"overloaded"}}`},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			io.WriteString(w, tc.body)
		}))
		_, err := newAnthropic(srv).Complete(context.Background(),
			[]modeltypes.ChatMessage{{Role: modeltypes.RoleUser, Content: "hi"}}, nil)
		srv.Close()
		if err == nil {
			t.Fatalf("status %d: want error", tc.status)
		}
		if got := FaultOf(err); got != modeltypes.FaultNone {
			t.Errorf("status %d: FaultOf = %q, want %q", tc.status, got, modeltypes.FaultNone)
		}
	}
}

func TestAnthropicTransportError(t *testing.T) {
	a := &Anthropic{
		APIKey:  "k",
		ModelID: "m",
		BaseURL: "http://example.invalid",
		HTTPClient: &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("dial tcp: refused")
		})},
	}
	_, err := a.Complete(context.Background(),
		[]modeltypes.ChatMessage{{Role: modeltypes.RoleUser, Content: "hi"}}, nil)
	if err == nil || FaultOf(err) != modeltypes.FaultNone {
		t.Fatalf("err = %v (fault %q), want a plain transport error", err, FaultOf(err))
	}
}

func TestAnthropicReadError(t *testing.T) {
	a := &Anthropic{
		APIKey:  "k",
		ModelID: "m",
		BaseURL: "http://example.invalid",
		HTTPClient: &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: errBody{}, Header: make(http.Header)}, nil
		})},
	}
	_, err := a.Complete(context.Background(),
		[]modeltypes.ChatMessage{{Role: modeltypes.RoleUser, Content: "hi"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "read response") {
		t.Fatalf("err = %v, want a read-response error", err)
	}
}

func TestAnthropicBuildRequestError(t *testing.T) {
	a := &Anthropic{APIKey: "k", ModelID: "m", BaseURL: "http://exa\x7fmple"}
	_, err := a.Complete(context.Background(),
		[]modeltypes.ChatMessage{{Role: modeltypes.RoleUser, Content: "hi"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "build request") {
		t.Fatalf("err = %v, want a build-request error", err)
	}
}

// TestAnthropicDefaultBaseURL confirms an empty BaseURL targets the public
// Messages API host. A stand-in transport captures the URL so no call leaves the
// process.
func TestAnthropicDefaultBaseURL(t *testing.T) {
	var gotURL string
	a := &Anthropic{
		APIKey:  "k",
		ModelID: "m",
		HTTPClient: &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
			gotURL = r.URL.String()
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(`{"content":[],"stop_reason":"end_turn"}`)),
				Header:     make(http.Header),
			}, nil
		})},
	}
	if _, err := a.Complete(context.Background(),
		[]modeltypes.ChatMessage{{Role: modeltypes.RoleUser, Content: "hi"}}, nil); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if gotURL != anthropicDefaultBaseURL+"/v1/messages" {
		t.Errorf("URL = %q, want %q", gotURL, anthropicDefaultBaseURL+"/v1/messages")
	}
}
