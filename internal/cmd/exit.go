package cmd

import (
	"context"
	"errors"

	"github.com/fulmenhq/gofulmen/foundry"
)

type commandError struct {
	code int
	err  error
}

func (e *commandError) Error() string          { return e.err.Error() }
func (e *commandError) Unwrap() error          { return e.err }
func commandFailure(code int, err error) error { return &commandError{code, err} }

// ExitCode uses the Fulmen catalog for command and verification failures.
func ExitCode(err error) int {
	if err == nil {
		return foundry.ExitSuccess
	}
	if errors.Is(err, context.Canceled) {
		return foundry.ExitTestInterrupted
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return foundry.ExitOperationTimeout
	}
	var e *commandError
	if errors.As(err, &e) {
		return e.code
	}
	return foundry.ExitFailure
}
