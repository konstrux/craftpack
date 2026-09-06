// SPDX-FileCopyrightText: 2026 Marcin Kaim
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"errors"
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
}
