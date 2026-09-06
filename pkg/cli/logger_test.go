// SPDX-FileCopyrightText: 2026 Marcin Kaim
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input       string
		expected    slog.Level
		expectError bool
	}{
		{"trace", LevelTrace, false},
		{"TRACE", LevelTrace, false},
		{"debug", slog.LevelDebug, false},
		{"DEBUG", slog.LevelDebug, false},
		{"info", slog.LevelInfo, false},
		{"INFO", slog.LevelInfo, false},
		{"warn", slog.LevelWarn, false},
		{"warning", slog.LevelWarn, false},
		{"WARN", slog.LevelWarn, false},
		{"error", slog.LevelError, false},
		{"ERROR", slog.LevelError, false},
		{"invalid", slog.LevelInfo, true},
		{"", slog.LevelInfo, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseLogLevel(tt.input)
			if tt.expectError && err == nil {
				t.Errorf("ParseLogLevel(%q) expected error, got nil", tt.input)
			}
			if !tt.expectError && err != nil {
				t.Errorf("ParseLogLevel(%q) unexpected error: %v", tt.input, err)
			}
			if !tt.expectError && got != tt.expected {
				t.Errorf("ParseLogLevel(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestResolveLogLevel_LWW(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		env         string
		expected    slog.Level
		expectError bool
	}{
		{
			name:     "default info when no flags or env",
			args:     []string{},
			env:      "",
			expected: slog.LevelInfo,
		},
		{
			name:     "env variable fallback",
			args:     []string{},
			env:      "debug",
			expected: slog.LevelDebug,
		},
		{
			name:        "invalid env variable returns error",
			args:        []string{},
			env:         "unknown_level",
			expectError: true,
		},
		{
			name:     "single -v flag produces DEBUG",
			args:     []string{"-v"},
			env:      "error",
			expected: slog.LevelDebug,
		},
		{
			name:     "--verbose flag produces DEBUG",
			args:     []string{"--verbose"},
			expected: slog.LevelDebug,
		},
		{
			name:     "double -v (-vv) produces TRACE",
			args:     []string{"-vv"},
			expected: LevelTrace,
		},
		{
			name:     "two -v flags produces TRACE",
			args:     []string{"-v", "-v"},
			expected: LevelTrace,
		},
		{
			name:     "-q flag produces ERROR",
			args:     []string{"-q"},
			expected: slog.LevelError,
		},
		{
			name:     "--quiet flag produces ERROR",
			args:     []string{"--quiet"},
			expected: slog.LevelError,
		},
		{
			name:     "--silent flag produces ERROR",
			args:     []string{"--silent"},
			expected: slog.LevelError,
		},
		{
			name:     "LWW: --verbose then --quiet -> ERROR",
			args:     []string{"--verbose", "--quiet"},
			expected: slog.LevelError,
		},
		{
			name:     "LWW: --quiet then --verbose -> DEBUG",
			args:     []string{"--quiet", "--verbose"},
			expected: slog.LevelDebug,
		},
		{
			name:     "LWW: -q then -v -> DEBUG",
			args:     []string{"-q", "-v"},
			expected: slog.LevelDebug,
		},
		{
			name:     "LWW: -v then -q -> ERROR",
			args:     []string{"-v", "-q"},
			expected: slog.LevelError,
		},
		{
			name:     "LWW: chained -vq -> ERROR",
			args:     []string{"-vq"},
			expected: slog.LevelError,
		},
		{
			name:     "LWW: chained -qv -> DEBUG",
			args:     []string{"-qv"},
			expected: slog.LevelDebug,
		},
		{
			name:     "LWW: -q then -vv -> TRACE",
			args:     []string{"-q", "-vv"},
			expected: LevelTrace,
		},
		{
			name:     "--log-level explicit value",
			args:     []string{"--log-level", "warn"},
			expected: slog.LevelWarn,
		},
		{
			name:     "--log-level=trace syntax",
			args:     []string{"--log-level=trace"},
			expected: LevelTrace,
		},
		{
			name:     "LWW: -v then --log-level=warn -> WARN",
			args:     []string{"-v", "--log-level=warn"},
			expected: slog.LevelWarn,
		},
		{
			name:     "LWW: --log-level=warn then -v -> DEBUG",
			args:     []string{"--log-level=warn", "-v"},
			expected: slog.LevelDebug,
		},
		{
			name:        "invalid --log-level returns error",
			args:        []string{"--log-level=foobar"},
			expectError: true,
		},
		{
			name:     "LWW: chained -vvq -> ERROR",
			args:     []string{"-vvq"},
			expected: slog.LevelError,
		},
		{
			name:     "LWW: chained -qvv -> TRACE",
			args:     []string{"-qvv"},
			expected: LevelTrace,
		},
		{
			name:     "LWW: chained -vqv -> DEBUG",
			args:     []string{"-vqv"},
			expected: slog.LevelDebug,
		},
		{
			name:     "LWW: CLI -v overrides env error",
			args:     []string{"-v"},
			env:      "error",
			expected: slog.LevelDebug,
		},
		{
			name:     "LWW: CLI -q overrides env trace",
			args:     []string{"-q"},
			env:      "trace",
			expected: slog.LevelError,
		},
		{
			name:     "LWW: CLI --log-level overrides env",
			args:     []string{"--log-level=warn"},
			env:      "debug",
			expected: slog.LevelWarn,
		},
		{
			name:     "env case-insensitive and whitespace trimmed",
			args:     []string{},
			env:      "  TRACE  ",
			expected: LevelTrace,
		},
		{
			name:     "flags after double dash are ignored",
			args:     []string{"--", "-v", "-q"},
			expected: slog.LevelInfo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveLogLevel(tt.args, tt.env)
			if tt.expectError && err == nil {
				t.Errorf("ResolveLogLevel() expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("ResolveLogLevel() unexpected error: %v", err)
			}
			if !tt.expectError && got != tt.expected {
				t.Errorf("ResolveLogLevel() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestColorEnabled(t *testing.T) {
	origNoColor := os.Getenv("NO_COLOR")
	origTerm := os.Getenv("TERM")
	defer func() {
		os.Setenv("NO_COLOR", origNoColor)
		os.Setenv("TERM", origTerm)
	}()

	var buf bytes.Buffer

	// Buffer is not a TTY
	if ColorEnabled(&buf) {
		t.Errorf("ColorEnabled(buffer) should be false")
	}

	// NO_COLOR set
	os.Setenv("NO_COLOR", "1")
	if ColorEnabled(os.Stderr) {
		t.Errorf("ColorEnabled with NO_COLOR=1 should be false")
	}

	// TERM=dumb
	os.Unsetenv("NO_COLOR")
	os.Setenv("TERM", "dumb")
	if ColorEnabled(os.Stderr) {
		t.Errorf("ColorEnabled with TERM=dumb should be false")
	}
}

func TestCLIHandler_Formatting(t *testing.T) {
	var buf bytes.Buffer
	handler := NewCLIHandler(&buf, CLIHandlerOptions{
		Level: slog.LevelInfo,
		Color: false,
	})

	ctx := context.Background()

	// 1. Info record
	rInfo := slog.NewRecord(time.Now(), slog.LevelInfo, "Building target", 0)
	rInfo.AddAttrs(slog.String("target", "deb"), slog.Int("count", 42))
	if err := handler.Handle(ctx, rInfo); err != nil {
		t.Fatalf("Handle info failed: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "[INFO ] Building target target=deb count=42\n") {
		t.Errorf("Handler output unexpected: %q", got)
	}

	// 2. Filtered record (debug when level is info)
	if handler.Enabled(ctx, slog.LevelDebug) {
		t.Errorf("Debug should not be enabled when level is info")
	}

	// 3. WithAttrs
	buf.Reset()
	attrHandler := handler.WithAttrs([]slog.Attr{slog.String("pkg", "craftpack")})
	rWarn := slog.NewRecord(time.Now(), slog.LevelWarn, "Deprecation notice", 0)
	if err := attrHandler.Handle(ctx, rWarn); err != nil {
		t.Fatalf("Handle warn failed: %v", err)
	}
	got = buf.String()
	if !strings.Contains(got, "[WARN ] Deprecation notice pkg=craftpack\n") {
		t.Errorf("Handler output unexpected: %q", got)
	}

	// 4. WithGroup
	buf.Reset()
	groupHandler := handler.WithGroup("meta")
	rGroup := slog.NewRecord(time.Now(), slog.LevelError, "Failure", 0)
	rGroup.AddAttrs(slog.String("code", "E01"))
	if err := groupHandler.Handle(ctx, rGroup); err != nil {
		t.Fatalf("Handle error failed: %v", err)
	}
	got = buf.String()
	if !strings.Contains(got, "[ERROR] Failure meta.code=E01\n") {
		t.Errorf("Handler output unexpected: %q", got)
	}
}

func TestCLIHandler_Color(t *testing.T) {
	var buf bytes.Buffer
	handler := NewCLIHandler(&buf, CLIHandlerOptions{
		Level: LevelTrace,
		Color: true,
	})

	ctx := context.Background()
	rTrace := slog.NewRecord(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), LevelTrace, "Tracing detail", 0)
	if err := handler.Handle(ctx, rTrace); err != nil {
		t.Fatalf("Handle trace failed: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, ansiMagenta+"[TRACE]"+ansiReset) {
		t.Errorf("Expected ANSI color for TRACE, got: %q", got)
	}
	if !strings.Contains(got, "12:00:00.000") {
		t.Errorf("Expected timestamp in trace output, got: %q", got)
	}
}

func TestSetupLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := SetupLogger(&buf, slog.LevelInfo, false)
	if logger == nil {
		t.Fatalf("SetupLogger returned nil")
	}

	slog.Info("Global log message", "key", "val")
	got := buf.String()
	if !strings.Contains(got, "[INFO ] Global log message key=val\n") {
		t.Errorf("SetupLogger output unexpected: %q", got)
	}
}

func TestCLIHandler_QuotedAttributes(t *testing.T) {
	var buf bytes.Buffer
	handler := NewCLIHandler(&buf, CLIHandlerOptions{
		Level: slog.LevelInfo,
		Color: false,
	})

	ctx := context.Background()
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "Quoting test", 0)
	r.AddAttrs(
		slog.String("simple", "hello"),
		slog.String("with_spaces", "value with spaces"),
		slog.String("with_quotes", `value "with" quotes`),
	)

	if err := handler.Handle(ctx, r); err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, `simple=hello`) {
		t.Errorf("expected simple=hello: %q", got)
	}
	if !strings.Contains(got, `with_spaces="value with spaces"`) {
		t.Errorf("expected with_spaces quoted: %q", got)
	}
	if !strings.Contains(got, `with_quotes="value \"with\" quotes"`) {
		t.Errorf("expected with_quotes escaped: %q", got)
	}
}

func TestCLIHandler_LevelTraceFiltering(t *testing.T) {
	var buf bytes.Buffer

	// When handler is at LevelDebug, LevelTrace records should be suppressed
	handlerDebug := NewCLIHandler(&buf, CLIHandlerOptions{
		Level: slog.LevelDebug,
		Color: false,
	})
	if handlerDebug.Enabled(context.Background(), LevelTrace) {
		t.Errorf("LevelTrace should not be enabled on Debug handler")
	}

	// When handler is at LevelTrace, LevelTrace records should be enabled
	handlerTrace := NewCLIHandler(&buf, CLIHandlerOptions{
		Level: LevelTrace,
		Color: false,
	})
	if !handlerTrace.Enabled(context.Background(), LevelTrace) {
		t.Errorf("LevelTrace should be enabled on Trace handler")
	}
}
