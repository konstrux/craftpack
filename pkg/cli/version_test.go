// SPDX-FileCopyrightText: 2026 Marcin Kaim
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
)

func TestCleanVersion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"1.0.0", "1.0.0"},
		{"v1.0.0", "1.0.0"},
		{"V2.1.3", "2.1.3"},
		{"  v1.2.3  ", "1.2.3"},
	}

	for _, tt := range tests {
		got := CleanVersion(tt.input)
		if got != tt.want {
			t.Errorf("CleanVersion(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestGetVersionInfo(t *testing.T) {
	info := GetVersionInfo()

	if info.Version == "" {
		t.Errorf("expected non-empty Version")
	}
	if info.GoVersion != runtime.Version() {
		t.Errorf("GoVersion = %q, want %q", info.GoVersion, runtime.Version())
	}
	if info.OS != runtime.GOOS {
		t.Errorf("OS = %q, want %q", info.OS, runtime.GOOS)
	}
	if info.Arch != runtime.GOARCH {
		t.Errorf("Arch = %q, want %q", info.Arch, runtime.GOARCH)
	}
	if info.Platform != runtime.GOOS+"/"+runtime.GOARCH {
		t.Errorf("Platform = %q, want %q", info.Platform, runtime.GOOS+"/"+runtime.GOARCH)
	}
}

func TestPrintVersion(t *testing.T) {
	var buf bytes.Buffer
	PrintVersion(&buf)
	got := buf.String()

	if !strings.HasPrefix(got, "craftpack v") {
		t.Errorf("PrintVersion output unexpected: %q", got)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("PrintVersion should end with newline: %q", got)
	}
}

func TestPrintVersionInfo(t *testing.T) {
	t.Run("text format", func(t *testing.T) {
		var buf bytes.Buffer
		if err := PrintVersionInfo(&buf, false); err != nil {
			t.Fatalf("PrintVersionInfo text failed: %v", err)
		}
		got := buf.String()
		if !strings.Contains(got, "craftpack v") {
			t.Errorf("expected 'craftpack v' in text output: %q", got)
		}
		if !strings.Contains(got, "Git commit:") {
			t.Errorf("expected 'Git commit:' in text output: %q", got)
		}
		if !strings.Contains(got, "Go version:") {
			t.Errorf("expected 'Go version:' in text output: %q", got)
		}
		if !strings.Contains(got, "Platform:") {
			t.Errorf("expected 'Platform:' in text output: %q", got)
		}
	})

	t.Run("json format", func(t *testing.T) {
		var buf bytes.Buffer
		if err := PrintVersionInfo(&buf, true); err != nil {
			t.Fatalf("PrintVersionInfo json failed: %v", err)
		}

		var parsed VersionInfo
		if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
			t.Fatalf("Failed to parse JSON output: %v (data: %s)", err, buf.String())
		}
		if parsed.Version != CleanVersion(Version) {
			t.Errorf("JSON Version = %q, want %q", parsed.Version, CleanVersion(Version))
		}
		if parsed.GoVersion != runtime.Version() {
			t.Errorf("JSON GoVersion = %q, want %q", parsed.GoVersion, runtime.Version())
		}
	})
}
