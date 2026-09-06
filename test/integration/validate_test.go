// SPDX-FileCopyrightText: 2026 Marcin Kaim
// SPDX-License-Identifier: Apache-2.0

package integration_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestIntegration_Validate_ValidFixtures(t *testing.T) {
	bin := getCraftpackBinary(t)
	rootDir, _ := filepath.Abs("../..")

	fixtures := []string{
		"test/fixtures/valid-minimal/craftpack.yml",
		"test/fixtures/valid-full/craftpack.yml",
	}

	for _, f := range fixtures {
		t.Run(f, func(t *testing.T) {
			specPath := filepath.Join(rootDir, f)
			cmd := exec.Command(bin, "validate", "--spec", specPath)

			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			err := cmd.Run()
			if err != nil {
				t.Fatalf("validation failed unexpectedly for %s: %v (stderr: %s)", f, err, stderr.String())
			}

			// STDOUT must be 0 bytes for standard validate
			if stdout.Len() != 0 {
				t.Errorf("STDOUT must be clean, got: %s", stdout.String())
			}

			if !strings.Contains(stderr.String(), "Specification is valid") {
				t.Errorf("STDERR missing valid confirmation: %s", stderr.String())
			}
		})
	}
}

func TestIntegration_Validate_JSONOutput(t *testing.T) {
	bin := getCraftpackBinary(t)
	rootDir, _ := filepath.Abs("../..")
	specPath := filepath.Join(rootDir, "test/fixtures/valid-full/craftpack.yml")

	cmd := exec.Command(bin, "validate", "--spec", specPath, "--json")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		t.Fatalf("validate --json failed: %v (stderr: %s)", err, stderr.String())
	}

	var res struct {
		Valid    bool     `json:"valid"`
		Spec     string   `json:"spec"`
		Package  string   `json:"package"`
		Warnings []string `json:"warnings"`
	}

	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("failed unmarshaling STDOUT JSON: %v (raw: %s)", err, stdout.String())
	}

	if !res.Valid || res.Package != "full-app" {
		t.Errorf("unexpected json result: %+v", res)
	}
}

func TestIntegration_Validate_InvalidFixtures(t *testing.T) {
	bin := getCraftpackBinary(t)
	rootDir, _ := filepath.Abs("../..")

	invalidCases := []struct {
		name        string
		relPath     string
		errContains string
	}{
		{
			name:        "invalid schema",
			relPath:     "test/fixtures/invalid-schema/craftpack.yml",
			errContains: "validation",
		},
		{
			name:        "invalid traversal",
			relPath:     "test/fixtures/invalid-traversal/craftpack.yml",
			errContains: "traverse",
		},
	}

	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			specPath := filepath.Join(rootDir, tc.relPath)
			cmd := exec.Command(bin, "validate", "--spec", specPath)

			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			err := cmd.Run()
			if err == nil {
				t.Fatalf("expected validation failure for %s, but succeeded", tc.relPath)
			}

			exitErr, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatalf("unexpected error type: %v", err)
			}
			if exitErr.ExitCode() != 1 {
				t.Errorf("exit code = %d, want 1 (ExitValidation)", exitErr.ExitCode())
			}

			if stdout.Len() != 0 {
				t.Errorf("STDOUT must be clean on validation failure, got: %s", stdout.String())
			}

			if !strings.Contains(stderr.String(), tc.errContains) {
				t.Errorf("STDERR missing expected error string %q: %s", tc.errContains, stderr.String())
			}
		})
	}
}

func TestIntegration_Validate_StrictMode(t *testing.T) {
	bin := getCraftpackBinary(t)
	dir := t.TempDir()

	appDir := filepath.Join(dir, "app")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("failed to create app dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "test.sh"), []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("failed to write entrypoint: %v", err)
	}

	specContent := `name: testapp
description: Application for testing strict validation
maintainer: Tester <tester@example.com>
homepage: https://example.com/test
license: MIT
command: testapp
payload_dir: app
entrypoint: test.sh
unknown_future_field: 42
targets:
  deb:
    section: utils
`
	specFile := filepath.Join(dir, "craftpack.yml")
	if err := os.WriteFile(specFile, []byte(specContent), 0644); err != nil {
		t.Fatalf("failed to write spec: %v", err)
	}

	// 1. Without --strict: succeeds with warning (forward tolerance)
	t.Run("without strict succeeds", func(t *testing.T) {
		cmd := exec.Command(bin, "validate", "--spec", specFile)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err := cmd.Run()
		if err != nil {
			t.Fatalf("expected success without --strict: %v (stderr: %s)", err, stderr.String())
		}
		if !strings.Contains(stderr.String(), "[WARN ]") {
			t.Errorf("expected [WARN ] in stderr: %s", stderr.String())
		}
	})

	// 2. With --strict: unknown key elevates to error (exit 1)
	t.Run("with strict fails", func(t *testing.T) {
		cmd := exec.Command(bin, "validate", "--spec", specFile, "--strict")
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err := cmd.Run()
		if err == nil {
			t.Fatalf("expected failure with --strict")
		}
		exitErr, ok := err.(*exec.ExitError)
		if !ok || exitErr.ExitCode() != 1 {
			t.Errorf("expected exit code 1, got %v", err)
		}
		if !strings.Contains(stderr.String(), "unknown configuration key") {
			t.Errorf("stderr missing unknown configuration key: %s", stderr.String())
		}
	})
}

func TestIntegration_Flags_LastFlagWins(t *testing.T) {
	bin := getCraftpackBinary(t)
	rootDir, _ := filepath.Abs("../..")
	specPath := filepath.Join(rootDir, "test/fixtures/valid-minimal/craftpack.yml")

	cases := []struct {
		name       string
		args       []string
		wantDebug  bool
		wantSilent bool
	}{
		{
			name:      "--quiet then --verbose -> DEBUG wins",
			args:      []string{"validate", "--spec", specPath, "--quiet", "--verbose"},
			wantDebug: true,
		},
		{
			name:       "--verbose then --quiet -> ERROR wins",
			args:       []string{"validate", "--spec", specPath, "--verbose", "--quiet"},
			wantSilent: true,
		},
		{
			name:      "-q then -v -> DEBUG wins",
			args:      []string{"validate", "--spec", specPath, "-q", "-v"},
			wantDebug: true,
		},
		{
			name:       "-v then -q -> ERROR wins",
			args:       []string{"validate", "--spec", specPath, "-v", "-q"},
			wantSilent: true,
		},
		{
			name:      "--log-level=warn then -v -> DEBUG wins",
			args:      []string{"validate", "--spec", specPath, "--log-level=warn", "-v"},
			wantDebug: true,
		},
		{
			name:       "--log-level=info then -q -> ERROR wins",
			args:       []string{"validate", "--spec", specPath, "--log-level=info", "-q"},
			wantSilent: true,
		},
		{
			name:      "-q then --log-level=debug -> DEBUG wins",
			args:      []string{"validate", "--spec", specPath, "-q", "--log-level=debug"},
			wantDebug: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(bin, tc.args...)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			err := cmd.Run()
			if err != nil {
				t.Fatalf("command failed: %v (stderr: %s)", err, stderr.String())
			}

			if tc.wantDebug && !strings.Contains(stderr.String(), "[DEBUG]") {
				t.Errorf("expected [DEBUG] in stderr: %s", stderr.String())
			}
			if tc.wantSilent && (strings.Contains(stderr.String(), "[DEBUG]") || strings.Contains(stderr.String(), "[INFO ]")) {
				t.Errorf("expected quiet mode to mute [DEBUG] and [INFO ]: %s", stderr.String())
			}
		})
	}
}

func TestIntegration_Flags_ExitCode2_SyntaxAndUsage(t *testing.T) {
	bin := getCraftpackBinary(t)

	usageCases := []struct {
		name        string
		args        []string
		errContains string
	}{
		{
			name:        "missing all mandatory build flags",
			args:        []string{"build"},
			errContains: "missing mandatory flag",
		},
		{
			name:        "missing package-version flag",
			args:        []string{"build", "--target", "deb"},
			errContains: "missing mandatory flag: --package-version",
		},
		{
			name:        "missing target flag",
			args:        []string{"build", "--package-version", "1.0.0"},
			errContains: "missing mandatory flag: --target",
		},
		{
			name:        "unsupported target format",
			args:        []string{"build", "--target", "rpm", "--package-version", "1.0.0"},
			errContains: "unsupported packaging target",
		},
		{
			name:        "invalid SemVer version string",
			args:        []string{"build", "--target", "deb", "--package-version", "bad-version"},
			errContains: "must comply strictly with SemVer 2.0.0",
		},
		{
			name:        "unknown global flag",
			args:        []string{"--unknown-flag"},
			errContains: "unknown flag",
		},
		{
			name:        "unknown subcommand",
			args:        []string{"invalid-subcommand"},
			errContains: "unknown command",
		},
		{
			name:        "extraneous positional argument",
			args:        []string{"build", "--target", "deb", "--package-version", "1.0.0", "extra"},
			errContains: "unknown command \"extra\"",
		},
		{
			name:        "double-dash followed by argument",
			args:        []string{"build", "--target", "deb", "--package-version", "1.0.0", "--", "extra"},
			errContains: "unknown command \"extra\"",
		},
		{
			name:        "invalid log level argument",
			args:        []string{"--log-level=invalid_value"},
			errContains: "invalid log level",
		},
	}

	for _, tc := range usageCases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(bin, tc.args...)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			err := cmd.Run()
			if err == nil {
				t.Fatalf("expected command to exit with 2, but succeeded")
			}

			exitErr, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatalf("unexpected error type: %v", err)
			}
			if exitErr.ExitCode() != 2 {
				t.Errorf("exit code = %d, want 2 (ExitUsage)", exitErr.ExitCode())
			}

			if stdout.Len() != 0 {
				t.Errorf("STDOUT must be clean on syntax error, got: %s", stdout.String())
			}

			if !strings.Contains(stderr.String(), tc.errContains) {
				t.Errorf("STDERR missing expected text %q: %s", tc.errContains, stderr.String())
			}
		})
	}
}

func TestIntegration_HelpAndVersionFlags(t *testing.T) {
	bin := getCraftpackBinary(t)

	t.Run("root --help outputs standard visual template", func(t *testing.T) {
		cmd := exec.Command(bin, "--help")
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err := cmd.Run()
		if err != nil {
			t.Fatalf("failed running --help: %v", err)
		}
		if stderr.Len() != 0 {
			t.Errorf("STDERR should be empty on --help, got: %s", stderr.String())
		}
		out := stdout.String()
		if !strings.Contains(out, "craftpack v") {
			t.Errorf("help missing version header: %s", out)
		}
		if !strings.Contains(out, "USAGE:\n  craftpack <COMMAND> [OPTIONS]") {
			t.Errorf("help missing USAGE block: %s", out)
		}
		if !strings.Contains(out, "COMMANDS:") || !strings.Contains(out, "build") || !strings.Contains(out, "validate") {
			t.Errorf("help missing COMMANDS block: %s", out)
		}
		if !strings.Contains(out, "GLOBAL OPTIONS:") {
			t.Errorf("help missing GLOBAL OPTIONS: %s", out)
		}
	})

	t.Run("-V outputs single-line version on STDOUT", func(t *testing.T) {
		cmd := exec.Command(bin, "-V")
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err := cmd.Run()
		if err != nil {
			t.Fatalf("failed running -V: %v", err)
		}
		if stderr.Len() != 0 {
			t.Errorf("STDERR must be empty on -V")
		}
		if !strings.HasPrefix(stdout.String(), "craftpack v") {
			t.Errorf("stdout = %q, want craftpack v...", stdout.String())
		}
	})

	t.Run("--version-info outputs detailed metadata on STDOUT", func(t *testing.T) {
		cmd := exec.Command(bin, "--version-info")
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err := cmd.Run()
		if err != nil {
			t.Fatalf("failed running --version-info: %v", err)
		}
		out := stdout.String()
		if !strings.Contains(out, "Git commit:") || !strings.Contains(out, "Go version:") || !strings.Contains(out, "Platform:") {
			t.Errorf("version info missing fields: %s", out)
		}
	})

	t.Run("--version-info --json outputs valid JSON metadata on STDOUT", func(t *testing.T) {
		cmd := exec.Command(bin, "--version-info", "--json")
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err := cmd.Run()
		if err != nil {
			t.Fatalf("failed running --version-info --json: %v", err)
		}

		var info map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
			t.Fatalf("failed parsing json on stdout: %v", err)
		}
		if info["version"] == nil || info["platform"] == nil || info["go_version"] == nil {
			t.Errorf("json missing expected keys: %+v", info)
		}
	})
}

func TestIntegration_Validate_JSONOutput_OnFailure(t *testing.T) {
	bin := getCraftpackBinary(t)
	dir := t.TempDir()
	nonExistentSpec := filepath.Join(dir, "missing.yml")

	cmd := exec.Command(bin, "validate", "--spec", nonExistentSpec, "--json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		t.Fatalf("expected validate to fail on missing spec")
	}

	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 1 {
		t.Errorf("exit code = %v, want 1 (ExitValidation)", err)
	}

	// STDOUT must remain 0 bytes on failure to preserve clean stream separation
	if stdout.Len() != 0 {
		t.Errorf("STDOUT must be empty on failure, got: %s", stdout.String())
	}

	if !strings.Contains(stderr.String(), "validation failed") && !strings.Contains(stderr.String(), "no such file") {
		t.Errorf("stderr missing failure error: %s", stderr.String())
	}
}

func TestIntegration_Validate_MissingSpec(t *testing.T) {
	bin := getCraftpackBinary(t)
	dir := t.TempDir()
	missingSpec := filepath.Join(dir, "does-not-exist.yml")

	cmd := exec.Command(bin, "validate", "--spec", missingSpec)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		t.Fatalf("expected validate to fail for nonexistent spec file")
	}

	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 1 {
		t.Errorf("exit code = %v, want 1 (ExitValidation)", err)
	}

	if stdout.Len() != 0 {
		t.Errorf("STDOUT must be 0 bytes on validation error, got: %s", stdout.String())
	}

	if !strings.Contains(stderr.String(), "validation failed") && !strings.Contains(stderr.String(), "no such file") {
		t.Errorf("stderr missing expected error message: %s", stderr.String())
	}
}

func TestIntegration_Validate_DestructiveHookRejection(t *testing.T) {
	bin := getCraftpackBinary(t)
	dir := t.TempDir()

	payloadDir := filepath.Join(dir, "bin")
	_ = os.MkdirAll(payloadDir, 0755)
	_ = os.WriteFile(filepath.Join(payloadDir, "app"), []byte("#!/bin/sh\n"), 0755)

	spec := `name: destruct-app
description: Application containing forbidden destructive hook
maintainer: Tester <test@example.com>
homepage: https://example.com/destruct
license: MIT
command: app
payload_dir: bin
entrypoint: app
preinstall: "rm -rf /*"
targets:
  deb:
    section: utils
`
	specPath := filepath.Join(dir, "craftpack.yml")
	_ = os.WriteFile(specPath, []byte(spec), 0644)

	cmd := exec.Command(bin, "validate", "--spec", specPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		t.Fatalf("expected validate to fail on destructive hook, but succeeded")
	}

	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 1 {
		t.Errorf("exit code = %v, want 1 (ExitValidation)", err)
	}

	if stdout.Len() != 0 {
		t.Errorf("STDOUT must be clean, got %d bytes", stdout.Len())
	}

	if !strings.Contains(stderr.String(), "destructive command") {
		t.Errorf("stderr missing destructive command message: %s", stderr.String())
	}
}

func TestIntegration_Validate_DefaultSpecInference(t *testing.T) {
	bin := getCraftpackBinary(t)
	dir := t.TempDir()

	payloadDir := filepath.Join(dir, "bin")
	_ = os.MkdirAll(payloadDir, 0755)
	_ = os.WriteFile(filepath.Join(payloadDir, "app"), []byte("#!/bin/sh\n"), 0755)

	spec := `name: default-val-app
description: Application testing default spec inference in validate
maintainer: Tester <test@example.com>
homepage: https://example.com/val
license: MIT
command: app
payload_dir: bin
entrypoint: app
targets:
  deb:
    section: utils
`
	specPath := filepath.Join(dir, "craftpack.yml")
	_ = os.WriteFile(specPath, []byte(spec), 0644)

	// Execute without --spec in working directory dir
	cmd := exec.Command(bin, "validate")
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("validate with default inference failed: %v\nSTDERR:\n%s", err, stderr.String())
	}

	if stdout.Len() != 0 {
		t.Errorf("STDOUT must be clean (0 bytes), got: %s", stdout.String())
	}

	if !strings.Contains(stderr.String(), "Specification is valid") {
		t.Errorf("STDERR missing success message: %s", stderr.String())
	}
}

func TestIntegration_Validate_ForwardTolerance_UnknownTargetAndOptions(t *testing.T) {
	bin := getCraftpackBinary(t)
	dir := t.TempDir()

	payloadDir := filepath.Join(dir, "bin")
	_ = os.MkdirAll(payloadDir, 0755)
	_ = os.WriteFile(filepath.Join(payloadDir, "app"), []byte("#!/bin/sh\n"), 0755)

	spec := `name: target-ft-app
description: Application testing unknown targets and options forward tolerance
maintainer: Tester <test@example.com>
homepage: https://example.com/ft
license: MIT
command: app
payload_dir: bin
entrypoint: app
targets:
  deb:
    section: utils
    custom_compressor: lzma
  rpm:
    section: utils
`
	specPath := filepath.Join(dir, "craftpack.yml")
	_ = os.WriteFile(specPath, []byte(spec), 0644)

	// 1. Without --strict: succeeds with warnings
	t.Run("without strict succeeds with warnings", func(t *testing.T) {
		cmd := exec.Command(bin, "validate", "--spec", specPath)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			t.Fatalf("expected success without --strict: %v (stderr: %s)", err, stderr.String())
		}
		if !strings.Contains(stderr.String(), "unrecognized packaging target 'rpm'") {
			t.Errorf("stderr missing unrecognized target warning: %s", stderr.String())
		}
		if !strings.Contains(stderr.String(), "unrecognized deb target option 'custom_compressor'") {
			t.Errorf("stderr missing unrecognized deb option warning: %s", stderr.String())
		}
	})

	// 2. With --strict: fails with exit code 1
	t.Run("with strict fails", func(t *testing.T) {
		cmd := exec.Command(bin, "validate", "--spec", specPath, "--strict")
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err := cmd.Run()
		if err == nil {
			t.Fatalf("expected failure with --strict")
		}
		exitErr, ok := err.(*exec.ExitError)
		if !ok || exitErr.ExitCode() != 1 {
			t.Errorf("exit code = %v, want 1 (ExitValidation)", err)
		}
		if !strings.Contains(stderr.String(), "unknown packaging target 'rpm'") {
			t.Errorf("stderr missing unknown target error: %s", stderr.String())
		}
	})
}

func TestIntegration_Validate_CraftpackYamlExtension(t *testing.T) {
	bin := getCraftpackBinary(t)
	dir := t.TempDir()

	payloadDir := filepath.Join(dir, "bin")
	_ = os.MkdirAll(payloadDir, 0755)
	_ = os.WriteFile(filepath.Join(payloadDir, "app"), []byte("#!/bin/sh\n"), 0755)

	spec := `name: yaml-val-app
description: Application testing craftpack.yaml with .yaml extension in validate
maintainer: Tester <test@example.com>
homepage: https://example.com/val
license: MIT
command: app
payload_dir: bin
entrypoint: app
targets:
  deb:
    section: utils
`
	specPath := filepath.Join(dir, "craftpack.yaml")
	_ = os.WriteFile(specPath, []byte(spec), 0644)

	cmd := exec.Command(bin, "validate", "--spec", specPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("validate failed with craftpack.yaml: %v\nSTDERR:\n%s", err, stderr.String())
	}

	if !strings.Contains(stderr.String(), "Specification is valid") {
		t.Errorf("stderr missing valid message: %s", stderr.String())
	}
}


