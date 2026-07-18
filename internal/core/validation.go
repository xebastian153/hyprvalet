package core

import (
	"errors"
	"fmt"
)

// ValidationError marks a capability rejecting its arguments during its
// validation phase — before any side effect. The distinction matters to the
// reasoning loop: a validation failure is the MODEL's mistake (a bad argument
// it can correct on a retry with the error as feedback), while any other
// failure is the world's (a dead socket, a missing binary) and re-asking the
// model would be noise. Capabilities return Validationf from argument checks
// and plain errors from execution.
type ValidationError struct {
	Msg string
}

func (e *ValidationError) Error() string { return e.Msg }

// Validationf builds a corrective, side-effect-free argument rejection.
func Validationf(format string, a ...any) error {
	return &ValidationError{Msg: fmt.Sprintf(format, a...)}
}

// IsValidation reports whether err is a capability argument rejection — the
// only kind of failure worth feeding back to the model for a retry.
func IsValidation(err error) bool {
	var v *ValidationError
	return errors.As(err, &v)
}
