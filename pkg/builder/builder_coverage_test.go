// SPDX-FileCopyrightText: 2026 Marcin Kaim
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"craftpack/pkg/fsutil"
)

func TestBuildContext_EdgePaths(t *testing.T) {
	// 1. Absolute SpecPath
	absSpec := "/tmp/test/craftpack.yml"
	bCtx1, err := NewBuildContext(BuildOptions{SpecPath: absSpec})
	if err != nil {
		t.Fatalf("NewBuildContext failed: %v", err)
	}
	if bCtx1.Options.WorkspaceDir != "/tmp/test" {
		t.Errorf("expected WorkspaceDir '/tmp/test', got '%s'", bCtx1.Options.WorkspaceDir)
	}

	// 2. Relative subpath SpecPath
	relSpec := "sub/folder/craftpack.yml"
	bCtx2, err := NewBuildContext(BuildOptions{SpecPath: relSpec})
	if err != nil {
		t.Fatalf("NewBuildContext failed: %v", err)
	}
	if !strings.HasSuffix(bCtx2.Options.WorkspaceDir, filepath.Join("sub", "folder")) {
		t.Errorf("expected WorkspaceDir to end with 'sub/folder', got '%s'", bCtx2.Options.WorkspaceDir)
	}

	// 3. DataDir before staging allocation
	bCtx3, _ := NewBuildContext(BuildOptions{})
	if bCtx3.DataDir() != "" {
		t.Errorf("expected empty DataDir before EnsureStagingDir, got '%s'", bCtx3.DataDir())
	}
}

func TestManifest_ExtendedCoverage(t *testing.T) {
	// 1. ReadManifest on non-existent file
	if _, err := ReadManifest("/nonexistent/path/checksums.sha256"); err == nil {
		t.Errorf("expected error reading non-existent manifest, got nil")
	}

	// 2. ReadManifest with comment lines, blank lines, and valid lines
	tmpDir, err := os.MkdirTemp("", "manifest-test-*")
	if err != nil {
		t.Fatalf("failed creating temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	manifestFile := filepath.Join(tmpDir, "checksums.sha256")
	content := "# This is a comment\n\n   \n" +
		strings.Repeat("a", 64) + "  pkg1.deb  (100 bytes)\n" +
		"# Another comment\n" +
		strings.Repeat("b", 64) + "  pkg2.deb  (200 bytes)\n"
	if err := os.WriteFile(manifestFile, []byte(content), fsutil.FileMode); err != nil {
		t.Fatalf("failed writing manifest file: %v", err)
	}

	entries, err := ReadManifest(manifestFile)
	if err != nil {
		t.Fatalf("ReadManifest failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Filename != "pkg1.deb" || entries[1].Filename != "pkg2.deb" {
		t.Errorf("unexpected entries: %+v", entries)
	}

	// 3. ReadManifest with malformed line
	badContent := strings.Repeat("a", 64) + "  pkg1.deb  (100 bytes)\nthis is bad\n"
	badFile := filepath.Join(tmpDir, "bad_checksums.sha256")
	if err := os.WriteFile(badFile, []byte(badContent), fsutil.FileMode); err != nil {
		t.Fatalf("failed writing bad manifest: %v", err)
	}
	if _, err := ReadManifest(badFile); err == nil {
		t.Errorf("expected error on malformed manifest line, got nil")
	}

	// 4. ParseManifestEntry with size overflow
	overflowLine := strings.Repeat("a", 64) + "  pkg.deb  (99999999999999999999999999999999999999999 bytes)"
	if _, err := ParseManifestEntry(overflowLine); err == nil {
		t.Errorf("expected error on size overflow, got nil")
	}

	// 5. WriteOrUpdateManifest error when parent directory is a regular file
	dummyFile := filepath.Join(tmpDir, "regular_file")
	if err := os.WriteFile(dummyFile, []byte("data"), fsutil.FileMode); err != nil {
		t.Fatalf("failed creating regular file: %v", err)
	}
	invalidManifestPath := filepath.Join(dummyFile, "checksums.sha256")
	if err := WriteOrUpdateManifest(invalidManifestPath, strings.Repeat("a", 64), "pkg.deb", 100); err == nil {
		t.Errorf("expected error when manifest dir is a file, got nil")
	}
}

func TestOrchestrator_NilContextAndEmptyVersion(t *testing.T) {
	orchestrator := NewOrchestrator()

	// Nil context
	if _, err := orchestrator.Build(context.Background(), nil); err == nil {
		t.Errorf("expected error with nil BuildContext, got nil")
	}

	// Empty version after normalization
	bCtx, _ := NewBuildContext(BuildOptions{
		Target:         "deb",
		PackageVersion: "   v   ",
	})
	if _, err := orchestrator.Build(context.Background(), bCtx); err == nil {
		t.Errorf("expected error when package version normalizes to empty, got nil")
	}

	// BuildWithOptions passing invalid options
	if _, err := orchestrator.BuildWithOptions(context.Background(), BuildOptions{
		Target:         "deb",
		PackageVersion: "v",
	}); err == nil {
		t.Errorf("expected error with invalid version in BuildWithOptions, got nil")
	}
}

func TestOrchestrator_DefaultOptionsAndSkippedStages(t *testing.T) {
	dir, err := os.MkdirTemp("", "craftpack-defaults-test-*")
	if err != nil {
		t.Fatalf("failed creating temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	// Minimal config with no man pages, no default config
	specContent := `
name: minimalapp
description: Minimal Application Test
maintainer: Marcin Kaim <marcin@example.com>
homepage: https://example.com/min
license: Apache-2.0
command: min
payload_dir: src
entrypoint: nested/deep/script.sh
targets:
  deb:
    section: utils
`
	if err := os.WriteFile(filepath.Join(dir, "craftpack.yml"), []byte(strings.TrimSpace(specContent)), fsutil.FileMode); err != nil {
		t.Fatalf("failed writing craftpack.yml: %v", err)
	}

	// Create nested directory in payload with an executable and regular file
	nestedDir := filepath.Join(dir, "src", "nested", "deep")
	if err := os.MkdirAll(nestedDir, fsutil.DirMode); err != nil {
		t.Fatalf("failed creating nested dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nestedDir, "script.sh"), []byte("#!/bin/sh\n"), fsutil.ExecMode); err != nil {
		t.Fatalf("failed creating script: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nestedDir, "read.txt"), []byte("text\n"), fsutil.FileMode); err != nil {
		t.Fatalf("failed creating read.txt: %v", err)
	}

	// Build with empty Architecture, zero BuildDate, empty OutputDir, empty SpecPath (defaults to craftpack.yml)
	orchestrator := NewOrchestrator()
	opts := BuildOptions{
		WorkspaceDir:   dir,
		SpecPath:       "", // should default to craftpack.yml
		PackageVersion: "1.0.0",
		Target:         "deb",
		Architecture:   "", // should default to host architecture
		BuildDate:      time.Time{}, // should default to time.Now()
		OutputDir:      "", // should default to ./dist
	}

	res, err := orchestrator.BuildWithOptions(context.Background(), opts)
	if err != nil {
		t.Fatalf("BuildWithOptions failed: %v", err)
	}
	if !res.Success {
		t.Errorf("expected success")
	}
	if res.Architecture == "" {
		t.Errorf("expected non-empty auto-detected architecture")
	}
	defer os.RemoveAll(filepath.Dir(res.PackageFile))

	// Verify file was written
	if _, err := os.Stat(res.PackageFile); err != nil {
		t.Errorf("package file not created: %v", err)
	}
}

func TestOrchestrator_DefaultConfigMissingFile(t *testing.T) {
	dir, err := os.MkdirTemp("", "craftpack-missing-conf-*")
	if err != nil {
		t.Fatalf("failed creating temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	// craftpack.yml referencing a missing config file
	specContent := `
name: badconfapp
description: Bad Conf Application
maintainer: Marcin Kaim <marcin@example.com>
homepage: https://example.com/bad
license: Apache-2.0
payload_dir: src
default_config:
  missing.conf: test.conf
targets:
  deb:
    section: utils
`
	if err := os.WriteFile(filepath.Join(dir, "craftpack.yml"), []byte(strings.TrimSpace(specContent)), fsutil.FileMode); err != nil {
		t.Fatalf("failed writing craftpack.yml: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "src"), fsutil.DirMode); err != nil {
		t.Fatalf("failed creating src: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "file.txt"), []byte("content"), fsutil.FileMode); err != nil {
		t.Fatalf("failed writing payload file: %v", err)
	}

	orchestrator := NewOrchestrator()
	opts := BuildOptions{
		WorkspaceDir:   dir,
		SpecPath:       "craftpack.yml",
		PackageVersion: "1.0.0",
		Target:         "deb",
	}

	// Schema validator will catch missing.conf in stage 1
	if _, err := orchestrator.BuildWithOptions(context.Background(), opts); err == nil {
		t.Errorf("expected error due to missing default_config file, got nil")
	}
}

func TestOrchestrator_CancelAtStages(t *testing.T) {
	stagesToTest := []Stage{
		Stage2StagingPayload,
		Stage3Launcher,
		Stage4Documentation,
		Stage5TargetMetadata,
		Stage6Archive,
		Stage7ManifestCleanup,
	}

	for _, targetStage := range stagesToTest {
		t.Run(string(targetStage), func(t *testing.T) {
			workspace, cleanup := setupMockWorkspace(t)
			defer cleanup()

			orchestrator := NewOrchestrator()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			opts := BuildOptions{
				WorkspaceDir:   workspace,
				SpecPath:       "craftpack.yml",
				PackageVersion: "1.0.0",
				Target:         "deb",
				OnStage: func(stage Stage, detail string) {
					if stage == targetStage {
						cancel()
					}
				},
			}

			_, err := orchestrator.BuildWithOptions(ctx, opts)
			if err == nil {
				t.Fatalf("expected error on cancellation at %s, got nil", targetStage)
			}
			if !strings.Contains(err.Error(), context.Canceled.Error()) {
				t.Errorf("expected context.Canceled error, got: %v", err)
			}
		})
	}
}

func TestOrchestrator_RawBuildContextDefaults(t *testing.T) {
	workspace, cleanup := setupMockWorkspace(t)
	defer cleanup()

	// Switch working directory to workspace so relative "." and "craftpack.yml" resolve naturally
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed getting wd: %v", err)
	}
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("failed chdir to workspace: %v", err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	orchestrator := NewOrchestrator()

	// Construct raw BuildContext without NewBuildContext to test orchestrator's defensive defaults
	rawCtx := &BuildContext{
		Options: BuildOptions{
			WorkspaceDir:   "", // Orchestrator sets to "."
			SpecPath:       "", // Orchestrator sets to filepath.Join(absWorkspace, "craftpack.yml")
			Target:         "deb",
			PackageVersion: "1.0.0",
			Architecture:   "", // Orchestrator sets to host
			BuildDate:      time.Time{}, // Orchestrator sets to time.Now()
			OutputDir:      "", // Orchestrator sets to "./dist"
		},
	}

	res, err := orchestrator.Build(context.Background(), rawCtx)
	if err != nil {
		t.Fatalf("orchestrator.Build with raw defaults failed: %v", err)
	}
	if !res.Success {
		t.Errorf("expected success")
	}
	if res.Architecture == "" {
		t.Errorf("expected auto-detected architecture")
	}
	defer os.RemoveAll(filepath.Dir(res.PackageFile))
}

func TestOrchestrator_MidStageFailures(t *testing.T) {
	orchestrator := NewOrchestrator()

	// 1. Man page deleted before Stage 4
	t.Run("man_page_missing_mid_stage", func(t *testing.T) {
		workspace, cleanup := setupMockWorkspace(t)
		defer cleanup()

		opts := BuildOptions{
			WorkspaceDir:   workspace,
			SpecPath:       "craftpack.yml",
			PackageVersion: "1.0.0",
			Target:         "deb",
			OnStage: func(stage Stage, detail string) {
				if stage == Stage4Documentation {
					// Delete the man page source file so generator fails
					_ = os.Remove(filepath.Join(workspace, "docs", "testapp.1.md"))
				}
			},
		}

		_, err := orchestrator.BuildWithOptions(context.Background(), opts)
		if err == nil {
			t.Fatalf("expected error on missing man page during stage 4, got nil")
		}
		if !strings.Contains(err.Error(), "stage 4") {
			t.Errorf("expected error from stage 4, got: %v", err)
		}
	})

	// 2. DefaultConfig deleted before Stage 5
	t.Run("default_config_missing_mid_stage", func(t *testing.T) {
		workspace, cleanup := setupMockWorkspace(t)
		defer cleanup()

		opts := BuildOptions{
			WorkspaceDir:   workspace,
			SpecPath:       "craftpack.yml",
			PackageVersion: "1.0.0",
			Target:         "deb",
			OnStage: func(stage Stage, detail string) {
				if stage == Stage5TargetMetadata {
					// Delete default config source file so copy fails
					_ = os.Remove(filepath.Join(workspace, "config", "testapp.conf"))
				}
			},
		}

		_, err := orchestrator.BuildWithOptions(context.Background(), opts)
		if err == nil {
			t.Fatalf("expected error on missing config during stage 5, got nil")
		}
		if !strings.Contains(err.Error(), "stage 5") {
			t.Errorf("expected error from stage 5, got: %v", err)
		}
	})
}

