// SPDX-FileCopyrightText: 2026 Marcin Kaim
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestExitCodesConstants(t *testing.T) {
	if ExitSuccess != 0 {
		t.Errorf("ExitSuccess = %d, want 0", ExitSuccess)
	}
	if ExitValidation != 1 {
		t.Errorf("ExitValidation = %d, want 1", ExitValidation)
	}
	if ExitUsage != 2 {
		t.Errorf("ExitUsage = %d, want 2", ExitUsage)
	}
	if ExitCannotExecute != 126 {
		t.Errorf("ExitCannotExecute = %d, want 126", ExitCannotExecute)
	}
	if ExitNotFound != 127 {
		t.Errorf("ExitNotFound = %d, want 127", ExitNotFound)
	}
	if ExitTerminated != 130 {
		t.Errorf("ExitTerminated = %d, want 130", ExitTerminated)
	}
}

func TestDetermineExitCode(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected int
	}{
		{
			name:     "nil error gives ExitSuccess (0)",
			err:      nil,
			expected: ExitSuccess,
		},
		{
			name:     "context.Canceled gives ExitTerminated (130)",
			err:      context.Canceled,
			expected: ExitTerminated,
		},
		{
			name:     "wrapped context.Canceled gives ExitTerminated (130)",
			err:      errors.Join(errors.New("prefix"), context.Canceled),
			expected: ExitTerminated,
		},
		{
			name:     "UsageError gives ExitUsage (2)",
			err:      NewUsageError("missing flag --target"),
			expected: ExitUsage,
		},
		{
			name:     "ValidationError gives ExitValidation (1)",
			err:      NewValidationError("schema invalid"),
			expected: ExitValidation,
		},
		{
			name:     "Generic error gives ExitValidation (1)",
			err:      errors.New("unexpected generic error"),
			expected: ExitValidation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetermineExitCode(tt.err)
			if got != tt.expected {
				t.Errorf("DetermineExitCode() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestCLIError(t *testing.T) {
	errUsage := NewUsageError("unknown flag: %s", "--foo")
	if errUsage.ExitCode() != ExitUsage {
		t.Errorf("errUsage.ExitCode() = %d, want %d", errUsage.ExitCode(), ExitUsage)
	}
	if errUsage.Error() != "unknown flag: --foo" {
		t.Errorf("errUsage.Error() = %q, want %q", errUsage.Error(), "unknown flag: --foo")
	}

	wrapped := &CLIError{
		Code: 42,
		Err:  errors.New("inner error"),
	}
	if wrapped.ExitCode() != 42 {
		t.Errorf("wrapped.ExitCode() = %d, want 42", wrapped.ExitCode())
	}
	if wrapped.Error() != "inner error" {
		t.Errorf("wrapped.Error() = %q, want %q", wrapped.Error(), "inner error")
	}
	if wrapped.Unwrap() == nil || wrapped.Unwrap().Error() != "inner error" {
		t.Errorf("wrapped.Unwrap() failed")
	}

	emptyErr := &CLIError{Code: 10}
	if emptyErr.Error() != "exit code 10" {
		t.Errorf("emptyErr.Error() = %q, want 'exit code 10'", emptyErr.Error())
	}
	if emptyErr.Unwrap() != nil {
		t.Errorf("emptyErr.Unwrap() should be nil, got: %v", emptyErr.Unwrap())
	}

	// NewValidationError with 0 arguments
	valErrZero := NewValidationError("plain validation failure")
	if valErrZero.ExitCode() != ExitValidation {
		t.Errorf("valErrZero.ExitCode() = %d, want %d", valErrZero.ExitCode(), ExitValidation)
	}
	if valErrZero.Error() != "plain validation failure" {
		t.Errorf("valErrZero.Error() = %q, want 'plain validation failure'", valErrZero.Error())
	}

	// NewValidationError with arguments
	valErrArgs := NewValidationError("field %s failed constraint %d", "version", 42)
	if valErrArgs.Error() != "field version failed constraint 42" {
		t.Errorf("valErrArgs.Error() = %q", valErrArgs.Error())
	}

	// NewUsageError with 0 arguments
	usageErrZero := NewUsageError("plain usage error")
	if usageErrZero.ExitCode() != ExitUsage {
		t.Errorf("usageErrZero.ExitCode() = %d, want %d", usageErrZero.ExitCode(), ExitUsage)
	}
	if usageErrZero.Error() != "plain usage error" {
		t.Errorf("usageErrZero.Error() = %q, want 'plain usage error'", usageErrZero.Error())
	}
}

type customCoder struct {
	code int
}

func (c customCoder) Error() string { return "custom error" }
func (c customCoder) ExitCode() int { return c.code }

func TestDetermineExitCode_DeepWrapAndCustomCoder(t *testing.T) {
	// Deep wrapping
	deepErr := fmt.Errorf("layer 2: %w", fmt.Errorf("layer 1: %w", NewUsageError("invalid parameter")))
	if got := DetermineExitCode(deepErr); got != ExitUsage {
		t.Errorf("DetermineExitCode(deepErr) = %d, want %d", got, ExitUsage)
	}

	// Custom ExitCoder
	cErr := customCoder{code: ExitCannotExecute}
	if got := DetermineExitCode(cErr); got != ExitCannotExecute {
		t.Errorf("DetermineExitCode(cErr) = %d, want %d", got, ExitCannotExecute)
	}
}
