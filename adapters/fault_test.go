package adapters

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/sophdn/model-router/modeltypes"
)

func TestFaultOf(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want modeltypes.FaultKind
	}{
		{"nil is none", nil, modeltypes.FaultNone},
		{"plain error is none", errors.New("boom"), modeltypes.FaultNone},
		{
			"fault error yields its kind",
			faultErr(modeltypes.FaultRateLimit, errors.New("429")),
			modeltypes.FaultRateLimit,
		},
		{
			"wrapped fault error yields its kind",
			fmt.Errorf("outer: %w", faultErr(modeltypes.FaultContextOverflow, errors.New("400"))),
			modeltypes.FaultContextOverflow,
		},
		{
			"deadline exceeded yields timeout",
			fmt.Errorf("call: %w", context.DeadlineExceeded),
			modeltypes.FaultTimeout,
		},
		{"bare deadline exceeded yields timeout", context.DeadlineExceeded, modeltypes.FaultTimeout},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := FaultOf(c.err); got != c.want {
				t.Errorf("FaultOf = %q, want %q", got, c.want)
			}
		})
	}
}

func TestFaultErrorErrorString(t *testing.T) {
	fe := faultErr(modeltypes.FaultTimeout, errors.New("deadline"))
	if got, want := fe.Error(), "timeout: deadline"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	if got := fe.Fault(); got != modeltypes.FaultTimeout {
		t.Errorf("Fault() = %q, want %q", got, modeltypes.FaultTimeout)
	}
	// A *FaultError with no wrapped error prints just the kind.
	bare := &FaultError{Kind: modeltypes.FaultRateLimit}
	if got, want := bare.Error(), "rate_limit"; got != want {
		t.Errorf("bare Error() = %q, want %q", got, want)
	}
}

func TestDecodeToolArgsEnvelope(t *testing.T) {
	cases := []struct {
		name       string
		raw        string
		wantAction string
		wantParams map[string]any
		wantFault  modeltypes.FaultKind
	}{
		{"empty is empty", "", "", nil, modeltypes.FaultNone},
		{
			"envelope with action and params",
			`{"action":"read","params":{"path":"/x"}}`,
			"read",
			map[string]any{"path": "/x"},
			modeltypes.FaultNone,
		},
		{
			"bare object becomes params",
			`{"path":"/x","depth":2}`,
			"",
			map[string]any{"path": "/x", "depth": float64(2)},
			modeltypes.FaultNone,
		},
		{
			"invalid json is malformed",
			`{"path": }`,
			"",
			nil,
			modeltypes.FaultMalformedToolCall,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			action, params, err := decodeToolArgs([]byte(c.raw))
			if got := FaultOf(err); got != c.wantFault {
				t.Fatalf("FaultOf(err) = %q, want %q (err=%v)", got, c.wantFault, err)
			}
			if c.wantFault != modeltypes.FaultNone {
				return
			}
			if action != c.wantAction {
				t.Errorf("action = %q, want %q", action, c.wantAction)
			}
			if fmt.Sprint(params) != fmt.Sprint(c.wantParams) {
				t.Errorf("params = %v, want %v", params, c.wantParams)
			}
		})
	}
}
