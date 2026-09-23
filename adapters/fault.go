// Package adapters holds real modeltypes.Adapter implementations that make
// network calls to a live model provider: OpenAICompat for any OpenAI-compatible
// endpoint (a local llama.cpp server, for example) and Anthropic for the Messages
// API. The router core stays sans-IO; this package is the only one that opens a
// socket.
//
// Each adapter maps a provider transport failure onto the modeltypes.FaultKind
// taxonomy through a *FaultError, so the caller recovers the kind with FaultOf and
// hands it to router.EscalateForFault. Everything else — an ordinary non-2xx — is a
// plain error whose FaultOf is FaultNone (fatal to the turn).
//
// This package is written for this public repository rather than extracted from
// the private system it came out of, so a sync of the extracted core leaves it in
// place.
package adapters

import (
	"context"
	"errors"
	"fmt"

	"github.com/sophdn/model-router/modeltypes"
)

// FaultError tags an error with the recoverable modeltypes.FaultKind an adapter
// classified it as. The caller turns it back into a kind with FaultOf.
type FaultError struct {
	// Kind is the recoverable fault class this error was classified as.
	Kind modeltypes.FaultKind
	// Err is the underlying transport or decode error.
	Err error
}

// Error combines the fault kind with the wrapped message.
func (e *FaultError) Error() string {
	if e.Err == nil {
		return string(e.Kind)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Err.Error())
}

// Unwrap exposes the wrapped error to errors.Is / errors.As.
func (e *FaultError) Unwrap() error { return e.Err }

// Fault returns the classified fault kind.
func (e *FaultError) Fault() modeltypes.FaultKind { return e.Kind }

// FaultOf recovers the modeltypes.FaultKind a Complete error carries: the Kind of
// a *FaultError when the chain holds one, else FaultTimeout when the chain is a
// context deadline, else FaultNone (the caller treats FaultNone as fatal).
func FaultOf(err error) modeltypes.FaultKind {
	if err == nil {
		return modeltypes.FaultNone
	}
	var fe *FaultError
	if errors.As(err, &fe) {
		return fe.Kind
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return modeltypes.FaultTimeout
	}
	return modeltypes.FaultNone
}

// faultErr builds a *FaultError of the given kind wrapping msg.
func faultErr(kind modeltypes.FaultKind, err error) *FaultError {
	return &FaultError{Kind: kind, Err: err}
}
