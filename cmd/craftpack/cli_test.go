// SPDX-FileCopyrightText: 2026 Marcin Kaim
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"craftpack/pkg/cli"
	"craftpack/pkg/fsutil"
)

func createMockWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// 1. payload directory and entrypoint
	payloadDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(payloadDir, fsutil.DirMode); err != nil {
		t.Fatalf("failed to create payload dir: %v", err)
	}
	binFile := filepath.Join(payloadDir, "my-app")
	if err := os.WriteFile(binFile, []byte("#!/bin/sh\necho hello\n"), fsutil.ExecMode); err != nil {
		t.Fatalf("failed to create entrypoint binary: %v", err)
	}

	// 2. documentation
	docsDir := filepath.Join(dir, "docs")
	if err := os.MkdirAll(docsDir, fsutil.DirMode); err != nil {
		t.Fatalf("failed to create docs dir: %v", err)
	}
	manFile := filepath.Join(docsDir, "my-app.1.md")
	if err := os.WriteFile(manFile, []byte("# MY-APP\n## NAME\nmy-app - test\n"), fsutil.FileMode); err != nil {
		t.Fatalf("failed to create man page: %v", err)
	}

	// 3. configuration
	configDir := filepath.Join(dir, "config")
	if err := os.MkdirAll(configDir, fsutil.DirMode); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	confFile := filepath.Join(configDir, "default.conf")
	if err := os.WriteFile(confFile, []byte("key = value\n"), fsutil.FileMode); err != nil {
		t.Fatalf("failed to create default config: %v", err)
	}

	// 4. craftpack.yml
	specContent := `name: my-app
description: Standardized Mock CLI Application
maintainer: Test Maintainer <maintainer@example.com>
homepage: https://example.com/myapp
license: Apache-2.0
command: my-app
payload_dir: bin
entrypoint: my-app
man_pages:
  - source: docs/my-app.1.md
    section: 1
default_config:
  config/default.conf: my-app.conf
targets:
  deb:
    section: utils
    priority: optional
    dependencies:
      - libc6 (>= 2.31)
`
	specFile := filepath.Join(dir, "craftpack.yml")
	if err := os.WriteFile(specFile, []byte(specContent), fsutil.FileMode); err != nil {
		t.Fatalf("failed to write craftpack.yml: %v", err)
	}

	return dir
}

func TestCLI_HelpOutputs(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		wantInStdout string
	}{
		{"root --help", []string{"--help"}, "craftpack v"},
		{"root -h", []string{"-h"}, "USAGE:\n  craftpack <COMMAND> [OPTIONS]"},
		{"build --help", []string{"build", "--help"}, "craftpack build - Build system-compliant packages (.deb)"},
		{"validate --help", []string{"validate", "--help"}, "craftpack validate - Validate the syntax, schema, and paths"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := ExecuteContextWithStreams(context.Background(), tt.args, &stdout, &stderr)
			if code != cli.ExitSuccess {
				t.Fatalf("exit code = %d, want %d (stderr: %s)", code, cli.ExitSuccess, stderr.String())
			}
			if !strings.Contains(stdout.String(), tt.wantInStdout) {
				t.Errorf("expected stdout to contain %q, got:\n%s", tt.wantInStdout, stdout.String())
			}
		})
	}
}

func TestCLI_VersionOutputs(t *testing.T) {
	t.Run("-V flag", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := ExecuteContextWithStreams(context.Background(), []string{"-V"}, &stdout, &stderr)
		if code != cli.ExitSuccess {
			t.Fatalf("code = %d, want 0", code)
		}
		if !strings.HasPrefix(stdout.String(), "craftpack v") {
			t.Errorf("stdout = %q, want craftpack v...", stdout.String())
		}
	})

	t.Run("--version flag", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := ExecuteContextWithStreams(context.Background(), []string{"--version"}, &stdout, &stderr)
		if code != cli.ExitSuccess {
			t.Fatalf("code = %d, want 0", code)
		}
		if !strings.HasPrefix(stdout.String(), "craftpack v") {
			t.Errorf("stdout = %q, want craftpack v...", stdout.String())
		}
	})

	t.Run("--version-info text", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := ExecuteContextWithStreams(context.Background(), []string{"--version-info"}, &stdout, &stderr)
		if code != cli.ExitSuccess {
			t.Fatalf("code = %d, want 0", code)
		}
		if !strings.Contains(stdout.String(), "Git commit:") {
			t.Errorf("stdout missing Git commit: %s", stdout.String())
		}
	})

	t.Run("--version-info JSON", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := ExecuteContextWithStreams(context.Background(), []string{"--version-info", "--json"}, &stdout, &stderr)
		if code != cli.ExitSuccess {
			t.Fatalf("code = %d, want 0", code)
		}
		var parsed cli.VersionInfo
		if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
			t.Fatalf("invalid json: %v", err)
		}
		if parsed.Version == "" {
			t.Errorf("parsed version is empty")
		}
	})
}

func TestCLI_RootNoArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := ExecuteContextWithStreams(context.Background(), []string{}, &stdout, &stderr)
	if code != cli.ExitSuccess {
		t.Fatalf("code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "USAGE:") {
		t.Errorf("expected USAGE in stdout: %s", stdout.String())
	}
}

func TestCLI_UsageErrors_Exit2(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		errContains string
	}{
		{
			name:        "unknown subcommand",
			args:        []string{"nonexistent-cmd"},
			errContains: "unknown command",
		},
		{
			name:        "unknown global flag",
			args:        []string{"--unknown-flag"},
			errContains: "unknown flag",
		},
		{
			name:        "unknown build flag",
			args:        []string{"build", "--unknown"},
			errContains: "unknown flag",
		},
		{
			name:        "build missing all required flags",
			args:        []string{"build"},
			errContains: "missing mandatory flag",
		},
		{
			name:        "build missing package-version",
			args:        []string{"build", "--target", "deb"},
			errContains: "missing mandatory flag: --package-version",
		},
		{
			name:        "build missing target",
			args:        []string{"build", "--package-version", "1.0.0"},
			errContains: "missing mandatory flag: --target",
		},
		{
			name:        "build unsupported target",
			args:        []string{"build", "--target", "rpm", "--package-version", "1.0.0"},
			errContains: "unsupported packaging target \"rpm\"",
		},
		{
			name:        "build invalid SemVer format",
			args:        []string{"build", "--target", "deb", "--package-version", "invalid-version"},
			errContains: "invalid package version \"invalid-version\"",
		},
		{
			name:        "build extraneous positional arguments",
			args:        []string{"build", "--target", "deb", "--package-version", "1.0.0", "extra-arg"},
			errContains: "unknown command \"extra-arg\"",
		},
		{
			name:        "build double dash positional argument",
			args:        []string{"build", "--target", "deb", "--package-version", "1.0.0", "--", "extra-arg"},
			errContains: "unknown command \"extra-arg\"",
		},
		{
			name:        "invalid log level value",
			args:        []string{"--log-level=invalid-level"},
			errContains: "invalid log level",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := ExecuteContextWithStreams(context.Background(), tt.args, &stdout, &stderr)
			if code != cli.ExitUsage {
				t.Fatalf("code = %d, want %d (stderr: %s)", code, cli.ExitUsage, stderr.String())
			}
			if !strings.Contains(stderr.String(), tt.errContains) {
				t.Errorf("expected stderr to contain %q, got: %s", tt.errContains, stderr.String())
			}
			// STDOUT must remain clean on usage error
			if stdout.Len() > 0 {
				t.Errorf("stdout must be empty on usage error, got: %s", stdout.String())
			}
		})
	}
}

func TestCLI_ValidationErrors_Exit1(t *testing.T) {
	t.Run("build non-existent specification", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := ExecuteContextWithStreams(context.Background(), []string{
			"build",
			"--spec", "non-existent-craftpack.yml",
			"--target", "deb",
			"--package-version", "1.0.0",
		}, &stdout, &stderr)

		if code != cli.ExitValidation {
			t.Fatalf("code = %d, want %d", code, cli.ExitValidation)
		}
		if !strings.Contains(stderr.String(), "not found") && !strings.Contains(stderr.String(), "no such file") {
			t.Errorf("stderr expected not found, got: %s", stderr.String())
		}
		if stdout.Len() > 0 {
			t.Errorf("stdout must be empty, got: %s", stdout.String())
		}
	})

	t.Run("validate non-existent specification", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := ExecuteContextWithStreams(context.Background(), []string{
			"validate",
			"--spec", "non-existent-craftpack.yml",
		}, &stdout, &stderr)

		if code != cli.ExitValidation {
			t.Fatalf("code = %d, want %d", code, cli.ExitValidation)
		}
		if stdout.Len() > 0 {
			t.Errorf("stdout must be empty, got: %s", stdout.String())
		}
	})

	t.Run("validate invalid yaml syntax", func(t *testing.T) {
		dir := t.TempDir()
		badFile := filepath.Join(dir, "bad.yml")
		_ = os.WriteFile(badFile, []byte("name: [unclosed list"), 0644)

		var stdout, stderr bytes.Buffer
		code := ExecuteContextWithStreams(context.Background(), []string{
			"validate",
			"--spec", badFile,
		}, &stdout, &stderr)

		if code != cli.ExitValidation {
			t.Fatalf("code = %d, want %d", code, cli.ExitValidation)
		}
	})

	t.Run("validate strict mode unknown key", func(t *testing.T) {
		dir := t.TempDir()
		specContent := `name: testapp
description: Test
maintainer: Marcin <marcin@example.com>
homepage: https://example.com
license: MIT
command: testapp
payload_dir: .
entrypoint: main.go
unknown_property: true
targets:
  deb:
    section: utils
`
		specFile := filepath.Join(dir, "craftpack.yml")
		_ = os.WriteFile(specFile, []byte(specContent), 0644)

		var stdout, stderr bytes.Buffer
		code := ExecuteContextWithStreams(context.Background(), []string{
			"validate",
			"--spec", specFile,
			"--strict",
		}, &stdout, &stderr)

		if code != cli.ExitValidation {
			t.Fatalf("code = %d, want %d (strict mode with unknown key)", code, cli.ExitValidation)
		}
		if !strings.Contains(stderr.String(), "unknown configuration key") {
			t.Errorf("stderr expected unknown configuration key, got: %s", stderr.String())
		}
	})
}

func TestCLI_Validate_Success(t *testing.T) {
	dir := createMockWorkspace(t)
	specPath := filepath.Join(dir, "craftpack.yml")

	t.Run("standard validate", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := ExecuteContextWithStreams(context.Background(), []string{
			"validate",
			"--spec", specPath,
		}, &stdout, &stderr)

		if code != cli.ExitSuccess {
			t.Fatalf("code = %d, want %d (stderr: %s)", code, cli.ExitSuccess, stderr.String())
		}
		// Strict stream separation: STDOUT must be 0 bytes
		if stdout.Len() != 0 {
			t.Errorf("stdout must be empty, got: %s", stdout.String())
		}
		if !strings.Contains(stderr.String(), "Specification is valid") {
			t.Errorf("stderr missing confirmation: %s", stderr.String())
		}
	})

	t.Run("validate with JSON output", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := ExecuteContextWithStreams(context.Background(), []string{
			"validate",
			"--spec", specPath,
			"--json",
		}, &stdout, &stderr)

		if code != cli.ExitSuccess {
			t.Fatalf("code = %d, want %d (stderr: %s)", code, cli.ExitSuccess, stderr.String())
		}

		var payload struct {
			Valid   bool   `json:"valid"`
			Package string `json:"package"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
			t.Fatalf("invalid json on stdout: %v (raw: %s)", err, stdout.String())
		}
		if !payload.Valid || payload.Package != "my-app" {
			t.Errorf("unexpected json payload: %+v", payload)
		}
	})
}

func TestCLI_Build_DryRun(t *testing.T) {
	dir := createMockWorkspace(t)
	specPath := filepath.Join(dir, "craftpack.yml")
	distDir := filepath.Join(dir, "dist")

	t.Run("dry run without JSON", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := ExecuteContextWithStreams(context.Background(), []string{
			"build",
			"--spec", specPath,
			"--target", "deb",
			"--package-version", "v1.2.3", // Leading v should be normalized cleanly
			"--output-dir", distDir,
			"--dry-run",
		}, &stdout, &stderr)

		if code != cli.ExitSuccess {
			t.Fatalf("code = %d, want %d (stderr: %s)", code, cli.ExitSuccess, stderr.String())
		}
		// Strict stream separation
		if stdout.Len() != 0 {
			t.Errorf("stdout must be empty during dry-run without --json: %s", stdout.String())
		}
		if !strings.Contains(stderr.String(), "Dry-run build simulation completed successfully") {
			t.Errorf("expected dry-run success message in stderr: %s", stderr.String())
		}

		// Ensure nothing was written to distDir
		if _, err := os.Stat(distDir); !os.IsNotExist(err) {
			t.Errorf("dist directory should not exist after dry run")
		}
	})

	t.Run("dry run with JSON output", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := ExecuteContextWithStreams(context.Background(), []string{
			"build",
			"--spec", specPath,
			"--target", "deb",
			"--package-version", "1.2.3",
			"--dry-run",
			"--json",
		}, &stdout, &stderr)

		if code != cli.ExitSuccess {
			t.Fatalf("code = %d, want %d (stderr: %s)", code, cli.ExitSuccess, stderr.String())
		}

		var res struct {
			Success bool   `json:"success"`
			DryRun  bool   `json:"dry_run"`
			Version string `json:"version"`
			Target  string `json:"target"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
			t.Fatalf("failed unmarshaling json from stdout: %v (raw: %s)", err, stdout.String())
		}
		if !res.Success || !res.DryRun || res.Version != "1.2.3" || res.Target != "deb" {
			t.Errorf("unexpected build result: %+v", res)
		}
	})
}

func TestCLI_Build_FullPackage(t *testing.T) {
	dir := createMockWorkspace(t)
	specPath := filepath.Join(dir, "craftpack.yml")
	distDir := filepath.Join(dir, "dist")

	var stdout, stderr bytes.Buffer
	code := ExecuteContextWithStreams(context.Background(), []string{
		"build",
		"--spec", specPath,
		"--target", "deb",
		"--package-version", "1.0.0",
		"--output-dir", distDir,
		"-v", // Verbose debug logging
	}, &stdout, &stderr)

	if code != cli.ExitSuccess {
		t.Fatalf("code = %d, want %d (stderr: %s)", code, cli.ExitSuccess, stderr.String())
	}
	// STDOUT must be clean
	if stdout.Len() != 0 {
		t.Errorf("stdout must be empty, got: %s", stdout.String())
	}

	// Verify package file exists in distDir
	debPath := filepath.Join(distDir, "my-app_1.0.0_amd64.deb")
	if _, err := os.Stat(debPath); err != nil {
		// If on arm64, it will be arm64
		entries, _ := os.ReadDir(distDir)
		found := false
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".deb") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("no .deb package created in %s", distDir)
		}
	}

	// Verify checksums manifest
	manifestPath := filepath.Join(distDir, "checksums.sha256")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("failed reading manifest: %v", err)
	}
	if !strings.Contains(string(data), "my-app_1.0.0_") {
		t.Errorf("manifest content unexpected: %s", string(data))
	}
}

func TestCLI_ContextCancellation_Exit130(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	var stdout, stderr bytes.Buffer
	code := ExecuteContextWithStreams(ctx, []string{"build", "--target", "deb", "--package-version", "1.0.0"}, &stdout, &stderr)
	if code != cli.ExitTerminated {
		t.Errorf("code = %d, want %d (ExitTerminated)", code, cli.ExitTerminated)
	}
}

func TestCLI_POSIX_SyntaxVariants(t *testing.T) {
	dir := createMockWorkspace(t)
	specPath := filepath.Join(dir, "craftpack.yml")

	variants := [][]string{
		// Long option equals separated
		{"validate", "--spec=" + specPath},
		// Short option separated
		{"validate", "-s", specPath},
		// Short option compact
		{"validate", "-s" + specPath},
	}

	for _, args := range variants {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := ExecuteContextWithStreams(context.Background(), args, &stdout, &stderr)
			if code != cli.ExitSuccess {
				t.Fatalf("failed with code %d: %s", code, stderr.String())
			}
		})
	}
}

func TestCLI_PackageVersion_SemVerCornerCases(t *testing.T) {
	dir := createMockWorkspace(t)
	specPath := filepath.Join(dir, "craftpack.yml")

	validVersions := []string{
		"1.0.0",
		"0.0.1",
		"10.20.30",
		"v1.2.3",
		"V2.0.0",
		"1.0.0-alpha",
		"1.0.0-alpha.1",
		"1.0.0-0.3.7",
		"1.0.0-x.7.z.92",
		"1.0.0-beta+exp.sha.5114f85",
		"1.0.0+20130313144700",
	}

	for _, ver := range validVersions {
		t.Run("valid: "+ver, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := ExecuteContextWithStreams(context.Background(), []string{
				"build",
				"--spec", specPath,
				"--target", "deb",
				"--package-version", ver,
				"--dry-run",
			}, &stdout, &stderr)

			if code != cli.ExitSuccess {
				t.Errorf("expected ExitSuccess (0) for version %q, got %d (stderr: %s)", ver, code, stderr.String())
			}
		})
	}

	invalidVersions := []string{
		"1.0",
		"v1.2",
		"01.1.1",
		"1.02.1",
		"1.1.03",
		"1.0.0.",
		"1.0.0-",
		"1.0.0+",
		"-1.0.0",
		"v",
		"random-string",
		" ",
	}

	for _, ver := range invalidVersions {
		t.Run("invalid: "+ver, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := ExecuteContextWithStreams(context.Background(), []string{
				"build",
				"--spec", specPath,
				"--target", "deb",
				"--package-version", ver,
				"--dry-run",
			}, &stdout, &stderr)

			if code != cli.ExitUsage {
				t.Errorf("expected ExitUsage (2) for invalid version %q, got %d", ver, code)
			}
			if !strings.Contains(stderr.String(), "must comply strictly with SemVer 2.0.0") &&
				!strings.Contains(stderr.String(), "missing mandatory flag") {
				t.Errorf("stderr should explain SemVer failure for %q, got: %s", ver, stderr.String())
			}
		})
	}
}

func TestCLI_VerboseVersionOutput(t *testing.T) {
	variants := []struct {
		name         string
		args         []string
		wantInStdout string
	}{
		{"-V -v combined", []string{"-V", "-v"}, "Git commit:"},
		{"--version --verbose", []string{"--version", "--verbose"}, "Go version:"},
		{"short chained -Vv", []string{"-Vv"}, "Platform:"},
	}

	for _, tt := range variants {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := ExecuteContextWithStreams(context.Background(), tt.args, &stdout, &stderr)
			if code != cli.ExitSuccess {
				t.Fatalf("code = %d, want 0 (stderr: %s)", code, stderr.String())
			}
			if !strings.Contains(stdout.String(), tt.wantInStdout) {
				t.Errorf("stdout missing %q: %s", tt.wantInStdout, stdout.String())
			}
		})
	}

	t.Run("-Vv with --json", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := ExecuteContextWithStreams(context.Background(), []string{"-Vv", "--json"}, &stdout, &stderr)
		if code != cli.ExitSuccess {
			t.Fatalf("code = %d, want 0", code)
		}
		var parsed cli.VersionInfo
		if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
			t.Fatalf("invalid json: %v", err)
		}
		if parsed.Platform == "" || parsed.OS == "" {
			t.Errorf("parsed version info missing fields: %+v", parsed)
		}
	})
}

func TestCLI_GlobalFlags_PositionAgnostic(t *testing.T) {
	dir := createMockWorkspace(t)
	specPath := filepath.Join(dir, "craftpack.yml")

	t.Run("global flag before subcommand", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := ExecuteContextWithStreams(context.Background(), []string{"-v", "validate", "-s", specPath}, &stdout, &stderr)
		if code != cli.ExitSuccess {
			t.Fatalf("code = %d, want 0", code)
		}
		if !strings.Contains(stderr.String(), "[DEBUG]") {
			t.Errorf("expected DEBUG log in stderr: %s", stderr.String())
		}
	})

	t.Run("global flag after subcommand", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := ExecuteContextWithStreams(context.Background(), []string{"validate", "-s", specPath, "-v"}, &stdout, &stderr)
		if code != cli.ExitSuccess {
			t.Fatalf("code = %d, want 0", code)
		}
		if !strings.Contains(stderr.String(), "[DEBUG]") {
			t.Errorf("expected DEBUG log in stderr: %s", stderr.String())
		}
	})

	t.Run("LWW across positions: -q before and -v after -> DEBUG wins", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := ExecuteContextWithStreams(context.Background(), []string{"-q", "validate", "-s", specPath, "-v"}, &stdout, &stderr)
		if code != cli.ExitSuccess {
			t.Fatalf("code = %d, want 0", code)
		}
		if !strings.Contains(stderr.String(), "[DEBUG]") {
			t.Errorf("expected DEBUG log in stderr because -v was rightmost: %s", stderr.String())
		}
	})

	t.Run("LWW across positions: -v before and -q after -> ERROR wins", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := ExecuteContextWithStreams(context.Background(), []string{"-v", "validate", "-s", specPath, "-q"}, &stdout, &stderr)
		if code != cli.ExitSuccess {
			t.Fatalf("code = %d, want 0", code)
		}
		// Quiet suppresses INFO and DEBUG, so only errors would be logged (or nothing on success)
		if strings.Contains(stderr.String(), "[DEBUG]") || strings.Contains(stderr.String(), "[INFO ]") {
			t.Errorf("expected INFO and DEBUG suppressed in quiet mode: %s", stderr.String())
		}
	})
}

func TestCLI_EnvironmentVariables_Precedence(t *testing.T) {
	dir := createMockWorkspace(t)
	specPath := filepath.Join(dir, "craftpack.yml")

	origEnv := os.Getenv("CRAFTPACK_LOG_LEVEL")
	defer os.Setenv("CRAFTPACK_LOG_LEVEL", origEnv)

	t.Run("CRAFTPACK_LOG_LEVEL=debug activates debug logging", func(t *testing.T) {
		os.Setenv("CRAFTPACK_LOG_LEVEL", "debug")
		var stdout, stderr bytes.Buffer
		code := ExecuteContextWithStreams(context.Background(), []string{"validate", "-s", specPath}, &stdout, &stderr)
		if code != cli.ExitSuccess {
			t.Fatalf("code = %d, want 0", code)
		}
		if !strings.Contains(stderr.String(), "[DEBUG]") {
			t.Errorf("expected DEBUG log when CRAFTPACK_LOG_LEVEL=debug: %s", stderr.String())
		}
	})

	t.Run("CLI flag overrides CRAFTPACK_LOG_LEVEL", func(t *testing.T) {
		os.Setenv("CRAFTPACK_LOG_LEVEL", "error")
		var stdout, stderr bytes.Buffer
		code := ExecuteContextWithStreams(context.Background(), []string{"validate", "-s", specPath, "-v"}, &stdout, &stderr)
		if code != cli.ExitSuccess {
			t.Fatalf("code = %d, want 0", code)
		}
		// CLI -v overrides env error
		if !strings.Contains(stderr.String(), "[DEBUG]") {
			t.Errorf("expected CLI -v to override env error: %s", stderr.String())
		}
	})

	t.Run("invalid CRAFTPACK_LOG_LEVEL causes ExitUsage (2)", func(t *testing.T) {
		os.Setenv("CRAFTPACK_LOG_LEVEL", "totally-invalid-level")
		var stdout, stderr bytes.Buffer
		code := ExecuteContextWithStreams(context.Background(), []string{"validate", "-s", specPath}, &stdout, &stderr)
		if code != cli.ExitUsage {
			t.Fatalf("code = %d, want %d", code, cli.ExitUsage)
		}
		if !strings.Contains(stderr.String(), "invalid log level") {
			t.Errorf("expected invalid log level error: %s", stderr.String())
		}
	})
}

func TestCLI_Build_TargetOptions(t *testing.T) {
	dir := createMockWorkspace(t)
	specPath := filepath.Join(dir, "craftpack.yml")

	cases := []struct {
		target   string
		wantCode int
	}{
		{"DEB", cli.ExitSuccess},
		{"Deb", cli.ExitSuccess},
		{"  deb  ", cli.ExitSuccess},
		{"rpm", cli.ExitUsage},
		{"apk", cli.ExitUsage},
		{"", cli.ExitUsage},
	}

	for _, tc := range cases {
		t.Run("target: "+tc.target, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := ExecuteContextWithStreams(context.Background(), []string{
				"build",
				"--spec", specPath,
				"--target", tc.target,
				"--package-version", "1.0.0",
				"--dry-run",
			}, &stdout, &stderr)

			if code != tc.wantCode {
				t.Errorf("target %q: got code %d, want %d (stderr: %s)", tc.target, code, tc.wantCode, stderr.String())
			}
		})
	}
}

func TestCLI_Build_ArchOverride(t *testing.T) {
	dir := createMockWorkspace(t)
	specPath := filepath.Join(dir, "craftpack.yml")

	architectures := []string{"arm64", "all", "amd64"}

	for _, a := range architectures {
		t.Run("arch: "+a, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := ExecuteContextWithStreams(context.Background(), []string{
				"build",
				"--spec", specPath,
				"--target", "deb",
				"--package-version", "1.0.0",
				"--arch", a,
				"--dry-run",
				"--json",
			}, &stdout, &stderr)

			if code != cli.ExitSuccess {
				t.Fatalf("code = %d, want 0 (stderr: %s)", code, stderr.String())
			}

			var res struct {
				Architecture string `json:"architecture"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
				t.Fatalf("failed unmarshaling json: %v", err)
			}
			if res.Architecture != a {
				t.Errorf("res.Architecture = %q, want %q", res.Architecture, a)
			}
		})
	}
}

func TestCLI_Build_Strict(t *testing.T) {
	dir := createMockWorkspace(t)
	specPath := filepath.Join(dir, "craftpack.yml")

	// Append unknown root key to specification
	f, err := os.OpenFile(specPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("failed to open spec: %v", err)
	}
	_, _ = f.WriteString("experimental_future_key: true\n")
	_ = f.Close()

	// 1. Without --strict: forward tolerance permits unknown key (exit 0)
	t.Run("build without --strict succeeds", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := ExecuteContextWithStreams(context.Background(), []string{
			"build",
			"--spec", specPath,
			"--target", "deb",
			"--package-version", "1.0.0",
			"--dry-run",
		}, &stdout, &stderr)

		if code != cli.ExitSuccess {
			t.Fatalf("code = %d, want 0 without strict (stderr: %s)", code, stderr.String())
		}
	})

	// 2. With --strict: unknown key elevates to hard error (exit 1)
	t.Run("build with --strict fails", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := ExecuteContextWithStreams(context.Background(), []string{
			"build",
			"--spec", specPath,
			"--target", "deb",
			"--package-version", "1.0.0",
			"--dry-run",
			"--strict",
		}, &stdout, &stderr)

		if code != cli.ExitValidation {
			t.Fatalf("code = %d, want %d with strict (stderr: %s)", code, cli.ExitValidation, stderr.String())
		}
		if !strings.Contains(stderr.String(), "unknown configuration key") {
			t.Errorf("stderr missing unknown configuration key error: %s", stderr.String())
		}
	})
}

func TestCLI_Build_NestedOutputDir(t *testing.T) {
	dir := createMockWorkspace(t)
	specPath := filepath.Join(dir, "craftpack.yml")
	nestedDist := filepath.Join(dir, "artifacts", "deep", "dist")

	var stdout, stderr bytes.Buffer
	code := ExecuteContextWithStreams(context.Background(), []string{
		"build",
		"--spec", specPath,
		"--target", "deb",
		"--package-version", "1.0.0",
		"--output-dir", nestedDist,
	}, &stdout, &stderr)

	if code != cli.ExitSuccess {
		t.Fatalf("code = %d, want 0 (stderr: %s)", code, stderr.String())
	}

	manifestPath := filepath.Join(nestedDist, "checksums.sha256")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Errorf("checksums.sha256 was not created in nested output dir: %v", err)
	}
}

func TestCLI_StreamSeparation_PipingIntegrity(t *testing.T) {
	dir := createMockWorkspace(t)
	specPath := filepath.Join(dir, "craftpack.yml")

	t.Run("build json stream integrity", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := ExecuteContextWithStreams(context.Background(), []string{
			"build",
			"--spec", specPath,
			"--target", "deb",
			"--package-version", "1.0.0",
			"--dry-run",
			"--output", "json",
		}, &stdout, &stderr)

		if code != cli.ExitSuccess {
			t.Fatalf("code = %d, want 0", code)
		}

		// STDOUT must be strictly parseable by a streaming decoder without extraneous text
		dec := json.NewDecoder(&stdout)
		var val map[string]any
		if err := dec.Decode(&val); err != nil {
			t.Fatalf("STDOUT is not clean JSON: %v", err)
		}
		if dec.More() {
			t.Errorf("STDOUT contains trailing characters after JSON object")
		}

		// STDERR must contain diagnostic logs
		if !strings.Contains(stderr.String(), "[INFO ]") {
			t.Errorf("STDERR missing diagnostic logs: %s", stderr.String())
		}
	})

	t.Run("validate json stream integrity", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := ExecuteContextWithStreams(context.Background(), []string{
			"validate",
			"--spec", specPath,
			"--output", "json",
		}, &stdout, &stderr)

		if code != cli.ExitSuccess {
			t.Fatalf("code = %d, want 0", code)
		}

		dec := json.NewDecoder(&stdout)
		var val map[string]any
		if err := dec.Decode(&val); err != nil {
			t.Fatalf("STDOUT is not clean JSON: %v", err)
		}
		if dec.More() {
			t.Errorf("STDOUT contains trailing characters after JSON object")
		}
	})
}

func TestCLI_ExecuteContext_Direct(t *testing.T) {
	code := ExecuteContext(context.Background(), []string{"--version"})
	if code != cli.ExitSuccess {
		t.Errorf("ExecuteContext(--version) = %d, want %d", code, cli.ExitSuccess)
	}
}

func TestCLI_MissingAndInvalidLogLevel_Exit2(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		errContains string
	}{
		{
			name:        "missing log-level argument at end",
			args:        []string{"--log-level"},
			errContains: "flag needs an argument: --log-level",
		},
		{
			name:        "empty log-level with equals",
			args:        []string{"--log-level="},
			errContains: "invalid log level",
		},
		{
			name:        "unknown log-level value",
			args:        []string{"--log-level", "superdebug"},
			errContains: "invalid log level \"superdebug\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := ExecuteContextWithStreams(context.Background(), tt.args, &stdout, &stderr)
			if code != cli.ExitUsage {
				t.Fatalf("code = %d, want %d (stderr: %s)", code, cli.ExitUsage, stderr.String())
			}
			if !strings.Contains(stderr.String(), tt.errContains) {
				t.Errorf("expected stderr to contain %q, got: %s", tt.errContains, stderr.String())
			}
			if stdout.Len() > 0 {
				t.Errorf("stdout should be empty on error, got: %s", stdout.String())
			}
		})
	}
}

func TestCLI_UnsupportedOutputFormat_Exit2(t *testing.T) {
	formats := []string{"xml", "yaml", "csv", "text/plain"}
	for _, fmtStr := range formats {
		t.Run("output "+fmtStr, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := ExecuteContextWithStreams(context.Background(), []string{"--output", fmtStr}, &stdout, &stderr)
			if code != cli.ExitUsage {
				t.Fatalf("code = %d, want %d (stderr: %s)", code, cli.ExitUsage, stderr.String())
			}
			if !strings.Contains(stderr.String(), "unsupported output format") {
				t.Errorf("expected unsupported output format error, got: %s", stderr.String())
			}
		})
	}
}

func TestCLI_WhitespaceAndDefaultOptions(t *testing.T) {
	dir := createMockWorkspace(t)
	specPath := filepath.Join(dir, "craftpack.yml")

	t.Run("build with whitespace spec and output-dir defaults cleanly", func(t *testing.T) {
		// Change working directory during test to the mock workspace
		origWd, err := os.Getwd()
		if err != nil {
			t.Fatalf("failed getting wd: %v", err)
		}
		if err := os.Chdir(dir); err != nil {
			t.Fatalf("failed chdir to mock dir: %v", err)
		}
		defer func() { _ = os.Chdir(origWd) }()

		var stdout, stderr bytes.Buffer
		code := ExecuteContextWithStreams(context.Background(), []string{
			"build",
			"--spec", "   ", // whitespace defaults to craftpack.yml
			"--target", "deb",
			"--package-version", "1.0.0",
			"--output-dir", "   ", // whitespace defaults to ./dist
			"--arch", "   ", // whitespace defaults to host arch
			"--dry-run",
		}, &stdout, &stderr)

		if code != cli.ExitSuccess {
			t.Fatalf("code = %d, want 0 (stderr: %s)", code, stderr.String())
		}
	})

	t.Run("validate with whitespace spec defaults cleanly", func(t *testing.T) {
		origWd, err := os.Getwd()
		if err != nil {
			t.Fatalf("failed getting wd: %v", err)
		}
		if err := os.Chdir(dir); err != nil {
			t.Fatalf("failed chdir to mock dir: %v", err)
		}
		defer func() { _ = os.Chdir(origWd) }()

		var stdout, stderr bytes.Buffer
		code := ExecuteContextWithStreams(context.Background(), []string{
			"validate",
			"--spec", "  ",
		}, &stdout, &stderr)

		if code != cli.ExitSuccess {
			t.Fatalf("code = %d, want 0 (stderr: %s)", code, stderr.String())
		}
	})

	t.Run("validate with explicit specPath", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := ExecuteContextWithStreams(context.Background(), []string{
			"validate",
			"-s", specPath,
		}, &stdout, &stderr)

		if code != cli.ExitSuccess {
			t.Fatalf("code = %d, want 0", code)
		}
	})
}

func TestCLI_AttachedOptionPaths_NoLogInterference(t *testing.T) {
	dir := createMockWorkspace(t)
	specPath := filepath.Join(dir, "craftpack.yml")

	// Even if an attached option path contains 'v' or 'q', it must not activate verbose/quiet logging
	var stdout, stderr bytes.Buffer
	code := ExecuteContextWithStreams(context.Background(), []string{
		"validate",
		"-s" + specPath, // compact flag with path that might contain letters
	}, &stdout, &stderr)

	if code != cli.ExitSuccess {
		t.Fatalf("code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	// Level must default to INFO, meaning [DEBUG] is not present
	if strings.Contains(stderr.String(), "[DEBUG]") {
		t.Errorf("attached path incorrectly triggered DEBUG logging: %s", stderr.String())
	}
}

func TestCLI_HelpAndVersion_StrictStreamIsolation(t *testing.T) {
	t.Run("-V stdout only", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := ExecuteContextWithStreams(context.Background(), []string{"-V"}, &stdout, &stderr)
		if code != cli.ExitSuccess {
			t.Fatalf("code = %d, want 0", code)
		}
		if stderr.Len() != 0 {
			t.Errorf("stderr must be 0 bytes for -V, got: %q", stderr.String())
		}
		if !strings.HasPrefix(stdout.String(), "craftpack v") {
			t.Errorf("stdout unexpected: %q", stdout.String())
		}
	})

	t.Run("--version stdout only", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := ExecuteContextWithStreams(context.Background(), []string{"--version"}, &stdout, &stderr)
		if code != cli.ExitSuccess {
			t.Fatalf("code = %d, want 0", code)
		}
		if stderr.Len() != 0 {
			t.Errorf("stderr must be 0 bytes for --version, got: %q", stderr.String())
		}
		if !strings.HasPrefix(stdout.String(), "craftpack v") {
			t.Errorf("stdout unexpected: %q", stdout.String())
		}
	})

	t.Run("--version-info stdout only", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := ExecuteContextWithStreams(context.Background(), []string{"--version-info"}, &stdout, &stderr)
		if code != cli.ExitSuccess {
			t.Fatalf("code = %d, want 0", code)
		}
		if stderr.Len() != 0 {
			t.Errorf("stderr must be 0 bytes for --version-info, got: %q", stderr.String())
		}
		if !strings.Contains(stdout.String(), "Git commit:") {
			t.Errorf("stdout missing Git commit: %s", stdout.String())
		}
	})

	t.Run("--version-info --output json stdout only", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := ExecuteContextWithStreams(context.Background(), []string{"--version-info", "--output", "json"}, &stdout, &stderr)
		if code != cli.ExitSuccess {
			t.Fatalf("code = %d, want 0", code)
		}
		if stderr.Len() != 0 {
			t.Errorf("stderr must be 0 bytes, got: %q", stderr.String())
		}
		var parsed cli.VersionInfo
		if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
			t.Fatalf("stdout is not valid JSON: %v (raw: %s)", err, stdout.String())
		}
	})

	t.Run("subcommand --help stdout only", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := ExecuteContextWithStreams(context.Background(), []string{"build", "--help"}, &stdout, &stderr)
		if code != cli.ExitSuccess {
			t.Fatalf("code = %d, want 0", code)
		}
		if stderr.Len() != 0 {
			t.Errorf("stderr must be 0 bytes on --help, got: %q", stderr.String())
		}
		if !strings.Contains(stdout.String(), "USAGE:") {
			t.Errorf("stdout missing USAGE: %s", stdout.String())
		}
	})
}
