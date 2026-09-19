// SPDX-FileCopyrightText: 2026 Marcin Kaim
// SPDX-License-Identifier: GPL-3.0-only

package cli

import (
	"bytes"
	"encoding/json"
	"errors"
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
		{"", ""},
		{"   ", ""},
		{"v", ""},
		{"V", ""},
		{"vv1.0.0", "v1.0.0"},
		{"  V2.0.0-rc1+build.123  ", "2.0.0-rc1+build.123"},
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

	t.Run("failing writer text format", func(t *testing.T) {
		fw := &failingWriter{}
		if err := PrintVersionInfo(fw, false); err == nil {
			t.Errorf("expected write error in text mode, got nil")
		}
	})

	t.Run("failing writer json format", func(t *testing.T) {
		fw := &failingWriter{}
		if err := PrintVersionInfo(fw, true); err == nil {
			t.Errorf("expected write error in json mode, got nil")
		}
	})
}

func TestGetVersion(t *testing.T) {
	origVer := Version
	origOnce := resolveOnce
	defer func() {
		Version = origVer
		resolveOnce = origOnce
	}()

	Version = "v2.4.6"
	if got := GetVersion(); got != "2.4.6" {
		t.Errorf("GetVersion() = %q, want %q", got, "2.4.6")
	}
}

func TestResolveVersionMetadata_PreservesExplicitValues(t *testing.T) {
	origVer := Version
	origCommit := GitCommit
	origDate := BuildDate
	origOnce := resolveOnce
	defer func() {
		Version = origVer
		GitCommit = origCommit
		BuildDate = origDate
		resolveOnce = origOnce
	}()

	// Set explicit values (as if passed via -ldflags)
	Version = "5.0.0"
	GitCommit = "custom-commit-sha"
	BuildDate = "2026-09-19T12:00:00Z"

	doResolveVersionMetadata()

	if Version != "5.0.0" {
		t.Errorf("Version was overwritten: got %q, want 5.0.0", Version)
	}
	if GitCommit != "custom-commit-sha" {
		t.Errorf("GitCommit was overwritten: got %q, want custom-commit-sha", GitCommit)
	}
	if BuildDate != "2026-09-19T12:00:00Z" {
		t.Errorf("BuildDate was overwritten: got %q, want 2026-09-19T12:00:00Z", BuildDate)
	}
}

func TestResolveVersionMetadata_DynamicFallback(t *testing.T) {
	origVer := Version
	origCommit := GitCommit
	origDate := BuildDate
	origOnce := resolveOnce
	defer func() {
		Version = origVer
		GitCommit = origCommit
		BuildDate = origDate
		resolveOnce = origOnce
	}()

	// Reset to unpopulated state
	Version = "unknown"
	GitCommit = "none"
	BuildDate = "unknown"

	doResolveVersionMetadata()

	// In a git repository, dynamic resolution must populate a valid SemVer and commit
	if isUnsetVersion(Version) {
		t.Errorf("Version remained unset after dynamic resolution: %q", Version)
	}
	if GitCommit == "none" || GitCommit == "" {
		t.Errorf("GitCommit was not dynamically resolved: %q", GitCommit)
	}
	if BuildDate == "unknown" || BuildDate == "" {
		t.Errorf("BuildDate was not dynamically resolved: %q", BuildDate)
	}
}

func TestIsUnsetHelpers(t *testing.T) {
	if !isUnsetVersion("") || !isUnsetVersion("unknown") || !isUnsetVersion("none") {
		t.Errorf("isUnsetVersion failed on unset values")
	}
	if isUnsetVersion("1.0.0") || isUnsetVersion("2.0.0-rc1") {
		t.Errorf("isUnsetVersion falsely reported set version as unset")
	}

	if !isUnsetCommit("") || !isUnsetCommit("none") || !isUnsetCommit("unknown") {
		t.Errorf("isUnsetCommit failed on unset values")
	}
	if isUnsetCommit("d24df232cd01") {
		t.Errorf("isUnsetCommit falsely reported set commit as unset")
	}

	if !isUnsetDate("") || !isUnsetDate("unknown") || !isUnsetDate("none") {
		t.Errorf("isUnsetDate failed on unset values")
	}
	if isUnsetDate("2026-09-19T08:43:37Z") {
		t.Errorf("isUnsetDate falsely reported set date as unset")
	}
}

type failingWriter struct{}

func (f *failingWriter) Write(p []byte) (n int, err error) {
	return 0, errors.New("simulated write failure")
}
