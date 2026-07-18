package core

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsValidation(t *testing.T) {
	v := Validationf("arg %q must be >= 1", "workspace")
	if !IsValidation(v) {
		t.Fatal("Validationf must be recognized as a validation error")
	}
	if !IsValidation(fmt.Errorf("wrapped: %w", v)) {
		t.Fatal("a wrapped validation error must still be recognized")
	}
	if IsValidation(errors.New("hyprctl: connection refused")) {
		t.Fatal("a runtime error must not read as a validation error")
	}
	if IsValidation(nil) {
		t.Fatal("nil is not a validation error")
	}
}
