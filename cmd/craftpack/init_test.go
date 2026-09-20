// SPDX-FileCopyrightText: 2026 Marcin Kaim
// SPDX-License-Identifier: GPL-3.0-only

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
	"craftpack/pkg/template"
)

func TestInit_Default_CreatesCraftpackYml(t *testing.T) {
	tmpDir := t.TempDir()
	outSpec := filepath.Join(tmpDir, "craftpack.yml")

	// Set CRAFTPACK_TEMPLATES_DIR to repo's templates directory for isolation
	rootDir, _ := filepath.Abs("../..")
	t.Setenv("CRAFTPACK_TEMPLATES_DIR", filepath.Join(rootDir, "templates"))

	var stdout, stderr bytes.Buffer
	code := ExecuteContextWithStreams(context.Background(), []string{"init", "-o", outSpec}, &stdout, &stderr)
	if code != cli.ExitSuccess {
		t.Fatalf("expected ExitSuccess (0), got %d. STDERR:\n%s", code, stderr.String())
	}

	data, err := os.ReadFile(outSpec)
	if err != nil {
		t.Fatalf("failed reading created spec: %v", err)
	}

	if !strings.Contains(string(data), "name: my-app") {
		t.Errorf("expected spec to contain 'name: my-app', got:\n%s", string(data))
	}
	if !strings.Contains(string(data), "targets:") {
		t.Errorf("expected spec to contain 'targets:', got:\n%s", string(data))
	}
	if !strings.Contains(stdout.String(), "Scaffolded 'deb' specification") {
		t.Errorf("stdout missing scaffolded message: %s", stdout.String())
	}
}

func TestInit_CollisionGuard_Rejection(t *testing.T) {
	tmpDir := t.TempDir()
	outSpec := filepath.Join(tmpDir, "craftpack.yml")
	_ = os.WriteFile(outSpec, []byte("existing content\n"), 0644)

	rootDir, _ := filepath.Abs("../..")
	t.Setenv("CRAFTPACK_TEMPLATES_DIR", filepath.Join(rootDir, "templates"))

	var stdout, stderr bytes.Buffer
	code := ExecuteContextWithStreams(context.Background(), []string{"init", "-o", outSpec}, &stdout, &stderr)
	if code != cli.ExitValidation {
		t.Fatalf("expected ExitValidation (1), got %d. STDERR:\n%s", code, stderr.String())
	}

	expectedErr := "craftpack: error: output file '" + outSpec + "' already exists. Use --force to overwrite."
	if !strings.Contains(stderr.String(), expectedErr) {
		t.Errorf("stderr missing expected error message.\nGot:\n%s\nWant containing:\n%s", stderr.String(), expectedErr)
	}

	// Verify original file content was preserved
	data, _ := os.ReadFile(outSpec)
	if string(data) != "existing content\n" {
		t.Errorf("existing file was modified: %s", string(data))
	}
}

func TestInit_ForceOverwrite(t *testing.T) {
	tmpDir := t.TempDir()
	outSpec := filepath.Join(tmpDir, "craftpack.yml")
	_ = os.WriteFile(outSpec, []byte("existing content\n"), 0644)

	rootDir, _ := filepath.Abs("../..")
	t.Setenv("CRAFTPACK_TEMPLATES_DIR", filepath.Join(rootDir, "templates"))

	var stdout, stderr bytes.Buffer
	code := ExecuteContextWithStreams(context.Background(), []string{"init", "-o", outSpec, "--force"}, &stdout, &stderr)
	if code != cli.ExitSuccess {
		t.Fatalf("expected ExitSuccess (0), got %d. STDERR:\n%s", code, stderr.String())
	}

	data, _ := os.ReadFile(outSpec)
	if !strings.Contains(string(data), "name: my-app") {
		t.Errorf("expected overwritten content with template, got: %s", string(data))
	}
}

func TestInit_StdoutStreaming(t *testing.T) {
	rootDir, _ := filepath.Abs("../..")
	t.Setenv("CRAFTPACK_TEMPLATES_DIR", filepath.Join(rootDir, "templates"))

	var stdout, stderr bytes.Buffer
	code := ExecuteContextWithStreams(context.Background(), []string{"init", "deb", "-o", "-"}, &stdout, &stderr)
	if code != cli.ExitSuccess {
		t.Fatalf("expected ExitSuccess (0), got %d. STDERR:\n%s", code, stderr.String())
	}

	outStr := stdout.String()
	if !strings.Contains(outStr, "name: my-app") {
		t.Errorf("stdout missing template content: %s", outStr)
	}
	// Must NOT contain informational logs on stdout
	if strings.Contains(outStr, "Scaffolded 'deb'") {
		t.Errorf("stdout unexpectedly contained informational log during streaming")
	}
}

func TestInit_List_PlainText(t *testing.T) {
	rootDir, _ := filepath.Abs("../..")
	t.Setenv("CRAFTPACK_TEMPLATES_DIR", filepath.Join(rootDir, "templates"))

	var stdout, stderr bytes.Buffer
	code := ExecuteContextWithStreams(context.Background(), []string{"init", "--list"}, &stdout, &stderr)
	if code != cli.ExitSuccess {
		t.Fatalf("expected ExitSuccess (0), got %d. STDERR:\n%s", code, stderr.String())
	}

	outStr := stdout.String()
	if !strings.Contains(outStr, "Available templates:") {
		t.Errorf("stdout missing 'Available templates:': %s", outStr)
	}
	if !strings.Contains(outStr, "deb") {
		t.Errorf("stdout missing 'deb' template: %s", outStr)
	}
	if !strings.Contains(outStr, "[env") {
		t.Errorf("stdout missing '[env' origin: %s", outStr)
	}
}

func TestInit_List_JSON(t *testing.T) {
	rootDir, _ := filepath.Abs("../..")
	t.Setenv("CRAFTPACK_TEMPLATES_DIR", filepath.Join(rootDir, "templates"))

	var stdout, stderr bytes.Buffer
	code := ExecuteContextWithStreams(context.Background(), []string{"init", "-l", "--json"}, &stdout, &stderr)
	if code != cli.ExitSuccess {
		t.Fatalf("expected ExitSuccess (0), got %d. STDERR:\n%s", code, stderr.String())
	}

	var list []template.Template
	if err := json.Unmarshal(stdout.Bytes(), &list); err != nil {
		t.Fatalf("failed unmarshaling json output: %v\nOutput was:\n%s", err, stdout.String())
	}

	if len(list) == 0 {
		t.Fatalf("expected non-empty template list in JSON")
	}

	foundDeb := false
	for _, item := range list {
		if item.Name == "deb" {
			foundDeb = true
			if item.Origin != template.OriginEnv {
				t.Errorf("origin = %s, want %s", item.Origin, template.OriginEnv)
			}
			break
		}
	}
	if !foundDeb {
		t.Errorf("template list missing 'deb': %+v", list)
	}
}

func TestInit_JSON_Scaffold(t *testing.T) {
	tmpDir := t.TempDir()
	outSpec := filepath.Join(tmpDir, "spec.yml")

	rootDir, _ := filepath.Abs("../..")
	t.Setenv("CRAFTPACK_TEMPLATES_DIR", filepath.Join(rootDir, "templates"))

	var stdout, stderr bytes.Buffer
	code := ExecuteContextWithStreams(context.Background(), []string{"init", "deb", "-o", outSpec, "--json"}, &stdout, &stderr)
	if code != cli.ExitSuccess {
		t.Fatalf("expected ExitSuccess (0), got %d. STDERR:\n%s", code, stderr.String())
	}

	var res struct {
		Template string `json:"template"`
		Origin   string `json:"origin"`
		Source   string `json:"source"`
		Output   string `json:"output"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("failed parsing JSON scaffold response: %v\nOutput: %s", err, stdout.String())
	}

	if res.Template != "deb" || res.Output != outSpec {
		t.Errorf("unexpected scaffold response: %+v", res)
	}
}

func TestInit_TemplateNotFound(t *testing.T) {
	rootDir, _ := filepath.Abs("../..")
	t.Setenv("CRAFTPACK_TEMPLATES_DIR", filepath.Join(rootDir, "templates"))

	var stdout, stderr bytes.Buffer
	code := ExecuteContextWithStreams(context.Background(), []string{"init", "nonexistent-template-xyz"}, &stdout, &stderr)
	if code != cli.ExitValidation {
		t.Fatalf("expected ExitValidation (1), got %d. STDERR:\n%s", code, stderr.String())
	}

	if !strings.Contains(stderr.String(), "not found") {
		t.Errorf("stderr missing 'not found' message: %s", stderr.String())
	}
}

func TestInit_Help_StdoutOnly(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := ExecuteContextWithStreams(context.Background(), []string{"init", "--help"}, &stdout, &stderr)
	if code != cli.ExitSuccess {
		t.Fatalf("expected ExitSuccess (0), got %d", code)
	}
	if stderr.Len() != 0 {
		t.Errorf("expected empty stderr for --help, got: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "craftpack init - Scaffold a new craftpack.yml") {
		t.Errorf("stdout missing help text: %s", stdout.String())
	}
}

func TestInit_QuietMode(t *testing.T) {
	tmpDir := t.TempDir()
	outSpec := filepath.Join(tmpDir, "quiet_spec.yml")

	rootDir, _ := filepath.Abs("../..")
	t.Setenv("CRAFTPACK_TEMPLATES_DIR", filepath.Join(rootDir, "templates"))

	var stdout, stderr bytes.Buffer
	code := ExecuteContextWithStreams(context.Background(), []string{"init", "-q", "-o", outSpec}, &stdout, &stderr)
	if code != cli.ExitSuccess {
		t.Fatalf("expected ExitSuccess (0), got %d. STDERR:\n%s", code, stderr.String())
	}

	if stdout.Len() != 0 {
		t.Errorf("expected empty stdout under -q, got: %s", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("expected empty stderr under -q, got: %s", stderr.String())
	}

	data, err := os.ReadFile(outSpec)
	if err != nil || !strings.Contains(string(data), "name: my-app") {
		t.Errorf("spec file not properly written")
	}
}

func TestInit_NestedDirectory_Creation(t *testing.T) {
	tmpDir := t.TempDir()
	outSpec := filepath.Join(tmpDir, "nested", "deep", "dir", "craftpack.yml")

	rootDir, _ := filepath.Abs("../..")
	t.Setenv("CRAFTPACK_TEMPLATES_DIR", filepath.Join(rootDir, "templates"))

	var stdout, stderr bytes.Buffer
	code := ExecuteContextWithStreams(context.Background(), []string{"init", "-o", outSpec}, &stdout, &stderr)
	if code != cli.ExitSuccess {
		t.Fatalf("expected ExitSuccess (0), got %d. STDERR:\n%s", code, stderr.String())
	}

	data, err := os.ReadFile(outSpec)
	if err != nil || !strings.Contains(string(data), "name: my-app") {
		t.Errorf("nested spec file was not created properly")
	}
}

func TestInit_OutputIsDirectory_Rejection(t *testing.T) {
	tmpDir := t.TempDir()
	outDir := filepath.Join(tmpDir, "target_dir")
	_ = os.MkdirAll(outDir, 0755)

	rootDir, _ := filepath.Abs("../..")
	t.Setenv("CRAFTPACK_TEMPLATES_DIR", filepath.Join(rootDir, "templates"))

	// 1. Without --force
	var stdout, stderr bytes.Buffer
	code := ExecuteContextWithStreams(context.Background(), []string{"init", "-o", outDir}, &stdout, &stderr)
	if code != cli.ExitValidation {
		t.Fatalf("expected ExitValidation (1), got %d. STDERR: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "is a directory") {
		t.Errorf("stderr missing 'is a directory' error: %s", stderr.String())
	}

	// 2. With --force (directory must still be rejected)
	stdout.Reset()
	stderr.Reset()
	code = ExecuteContextWithStreams(context.Background(), []string{"init", "-o", outDir, "--force"}, &stdout, &stderr)
	if code != cli.ExitValidation {
		t.Fatalf("expected ExitValidation (1), got %d. STDERR: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "is a directory") {
		t.Errorf("stderr missing 'is a directory' error: %s", stderr.String())
	}
}

func TestInit_Symlink_CollisionAndOverwrite(t *testing.T) {
	tmpDir := t.TempDir()
	realTarget := filepath.Join(tmpDir, "real_target.yml")
	_ = os.WriteFile(realTarget, []byte("original content\n"), 0644)

	symlinkFile := filepath.Join(tmpDir, "symlink.yml")
	_ = os.Symlink(realTarget, symlinkFile)

	brokenSymlink := filepath.Join(tmpDir, "broken_symlink.yml")
	_ = os.Symlink(filepath.Join(tmpDir, "nonexistent.yml"), brokenSymlink)

	rootDir, _ := filepath.Abs("../..")
	t.Setenv("CRAFTPACK_TEMPLATES_DIR", filepath.Join(rootDir, "templates"))

	// 1. Valid symlink without --force -> rejected
	var stdout, stderr bytes.Buffer
	code := ExecuteContextWithStreams(context.Background(), []string{"init", "-o", symlinkFile}, &stdout, &stderr)
	if code != cli.ExitValidation {
		t.Fatalf("expected ExitValidation (1) for existing symlink without --force, got %d", code)
	}

	// 2. Valid symlink with --force -> replaced with clean regular file
	stdout.Reset()
	stderr.Reset()
	code = ExecuteContextWithStreams(context.Background(), []string{"init", "-o", symlinkFile, "--force"}, &stdout, &stderr)
	if code != cli.ExitSuccess {
		t.Fatalf("expected ExitSuccess (0) for symlink with --force, got %d. STDERR: %s", code, stderr.String())
	}
	fi, err := os.Lstat(symlinkFile)
	if err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Errorf("expected symlink to be replaced with regular file")
	}

	// 3. Broken symlink without --force -> rejected
	stdout.Reset()
	stderr.Reset()
	code = ExecuteContextWithStreams(context.Background(), []string{"init", "-o", brokenSymlink}, &stdout, &stderr)
	if code != cli.ExitValidation {
		t.Fatalf("expected ExitValidation (1) for broken symlink without --force, got %d", code)
	}

	// 4. Broken symlink with --force -> replaced with regular file
	stdout.Reset()
	stderr.Reset()
	code = ExecuteContextWithStreams(context.Background(), []string{"init", "-o", brokenSymlink, "--force"}, &stdout, &stderr)
	if code != cli.ExitSuccess {
		t.Fatalf("expected ExitSuccess (0) for broken symlink with --force, got %d. STDERR: %s", code, stderr.String())
	}
	fi, err = os.Lstat(brokenSymlink)
	if err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Errorf("expected broken symlink to be replaced with regular file")
	}
}

func TestInit_ExtraneousPositionalArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := ExecuteContextWithStreams(context.Background(), []string{"init", "deb", "extraneous"}, &stdout, &stderr)
	if code != cli.ExitUsage {
		t.Fatalf("expected ExitUsage (2), got %d. STDERR: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "accepts at most 1 arg") {
		t.Errorf("stderr missing args error: %s", stderr.String())
	}
}

func TestInit_UnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := ExecuteContextWithStreams(context.Background(), []string{"init", "--unknown-flag-xyz"}, &stdout, &stderr)
	if code != cli.ExitUsage {
		t.Fatalf("expected ExitUsage (2), got %d. STDERR: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "unknown flag") {
		t.Errorf("stderr missing unknown flag error: %s", stderr.String())
	}
}

func TestInit_OutputFlagCustomAndJSON(t *testing.T) {
	tmpDir := t.TempDir()
	outSpec := filepath.Join(tmpDir, "custom-output.yml")

	rootDir, _ := filepath.Abs("../..")
	t.Setenv("CRAFTPACK_TEMPLATES_DIR", filepath.Join(rootDir, "templates"))

	var stdout, stderr bytes.Buffer
	code := ExecuteContextWithStreams(context.Background(), []string{"init", "deb", "--output", outSpec, "--json"}, &stdout, &stderr)
	if code != cli.ExitSuccess {
		t.Fatalf("expected ExitSuccess (0), got %d. STDERR: %s", code, stderr.String())
	}

	var res struct {
		Template string `json:"template"`
		Origin   string `json:"origin"`
		Source   string `json:"source"`
		Output   string `json:"output"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("expected valid JSON, got err: %v. Output: %s", err, stdout.String())
	}
	if res.Output != outSpec || res.Template != "deb" {
		t.Errorf("unexpected json result: %+v", res)
	}

	data, err := os.ReadFile(outSpec)
	if err != nil || !strings.Contains(string(data), "name: my-app") {
		t.Errorf("expected spec file to be created at custom output path")
	}
}

func TestInit_EmptyName_DefaultsToDeb(t *testing.T) {
	tmpDir := t.TempDir()
	outSpec := filepath.Join(tmpDir, "spec_empty_name.yml")

	rootDir, _ := filepath.Abs("../..")
	t.Setenv("CRAFTPACK_TEMPLATES_DIR", filepath.Join(rootDir, "templates"))

	var stdout, stderr bytes.Buffer
	code := ExecuteContextWithStreams(context.Background(), []string{"init", "", "-o", outSpec}, &stdout, &stderr)
	if code != cli.ExitSuccess {
		t.Fatalf("expected ExitSuccess (0), got %d. STDERR: %s", code, stderr.String())
	}

	data, err := os.ReadFile(outSpec)
	if err != nil || !strings.Contains(string(data), "name: my-app") {
		t.Errorf("expected deb template to be scaffolded when name is empty")
	}
}
