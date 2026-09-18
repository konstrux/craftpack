// SPDX-FileCopyrightText: 2026 Marcin Kaim
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"craftpack/pkg/fsutil"
	"craftpack/pkg/spec"
	"craftpack/pkg/target/deb"
)

// Helper to create a comprehensive mock project workspace for testing.
func setupMockWorkspace(t *testing.T) (workspace string, clean func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "craftpack-builder-test-*")
	if err != nil {
		t.Fatalf("failed creating temp dir: %v", err)
	}

	// 1. craftpack.yml
	specContent := `
name: testapp
description: Test Application for Craftpack Builder
maintainer: Marcin Kaim <marcin@example.com>
homepage: https://example.com/testapp
license: Apache-2.0
command: testapp
payload_dir: build/out
entrypoint: app-bin
preinstall: scripts/preinst.sh
man_pages:
  - source: docs/testapp.1.md
    section: 1
    title: TESTAPP
default_config:
  config/testapp.conf: testapp.conf
targets:
  deb:
    section: utils
    priority: optional
    dependencies:
      - libc6 (>= 2.31)
`
	if err := os.WriteFile(filepath.Join(dir, "craftpack.yml"), []byte(strings.TrimSpace(specContent)), fsutil.FileMode); err != nil {
		t.Fatalf("failed writing craftpack.yml: %v", err)
	}

	// 2. build/out/app-bin (executable)
	payloadDir := filepath.Join(dir, "build", "out")
	if err := os.MkdirAll(payloadDir, fsutil.DirMode); err != nil {
		t.Fatalf("failed creating payload dir: %v", err)
	}
	binPath := filepath.Join(payloadDir, "app-bin")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\necho test\n"), fsutil.ExecMode); err != nil {
		t.Fatalf("failed creating app-bin: %v", err)
	}

	// Support file in payload
	assetPath := filepath.Join(payloadDir, "data.txt")
	if err := os.WriteFile(assetPath, []byte("asset content"), fsutil.FileMode); err != nil {
		t.Fatalf("failed creating data.txt: %v", err)
	}

	// 3. docs/testapp.1.md
	docsDir := filepath.Join(dir, "docs")
	if err := os.MkdirAll(docsDir, fsutil.DirMode); err != nil {
		t.Fatalf("failed creating docs dir: %v", err)
	}
	manPath := filepath.Join(docsDir, "testapp.1.md")
	manContent := `# NAME
testapp - manual test page

# SYNOPSIS
testapp [options]
`
	if err := os.WriteFile(manPath, []byte(manContent), fsutil.FileMode); err != nil {
		t.Fatalf("failed creating testapp.1.md: %v", err)
	}

	// 4. config/testapp.conf
	cfgDir := filepath.Join(dir, "config")
	if err := os.MkdirAll(cfgDir, fsutil.DirMode); err != nil {
		t.Fatalf("failed creating config dir: %v", err)
	}
	confPath := filepath.Join(cfgDir, "testapp.conf")
	if err := os.WriteFile(confPath, []byte("key=value\n"), fsutil.FileMode); err != nil {
		t.Fatalf("failed creating testapp.conf: %v", err)
	}

	// 5. scripts/preinst.sh
	scriptsDir := filepath.Join(dir, "scripts")
	if err := os.MkdirAll(scriptsDir, fsutil.DirMode); err != nil {
		t.Fatalf("failed creating scripts dir: %v", err)
	}
	scriptPath := filepath.Join(scriptsDir, "preinst.sh")
	if err := os.WriteFile(scriptPath, []byte("echo installing\n"), fsutil.ExecMode); err != nil {
		t.Fatalf("failed creating preinst.sh: %v", err)
	}

	return dir, func() {
		_ = os.RemoveAll(dir)
	}
}

func TestBuildContext_Lifecycle(t *testing.T) {
	opts := BuildOptions{
		SpecPath:       "custom-spec.yml",
		OutputDir:      "", // Should default to ./dist
		PackageVersion: "1.0.0",
		Target:         "deb",
	}

	bCtx, err := NewBuildContext(opts)
	if err != nil {
		t.Fatalf("NewBuildContext failed: %v", err)
	}

	if bCtx.Options.OutputDir != "./dist" {
		t.Errorf("expected OutputDir to default to './dist', got '%s'", bCtx.Options.OutputDir)
	}
	if bCtx.Options.SpecPath != "custom-spec.yml" {
		t.Errorf("expected SpecPath 'custom-spec.yml', got '%s'", bCtx.Options.SpecPath)
	}
	if bCtx.BuildDate.IsZero() {
		t.Errorf("expected non-zero default BuildDate")
	}

	// EnsureStagingDir
	stagingDir, err := bCtx.EnsureStagingDir()
	if err != nil {
		t.Fatalf("EnsureStagingDir failed: %v", err)
	}
	if stagingDir == "" {
		t.Fatalf("EnsureStagingDir returned empty path")
	}
	if fi, err := os.Stat(stagingDir); err != nil || !fi.IsDir() {
		t.Fatalf("staging directory does not exist on disk: %v", err)
	}

	// Idempotent EnsureStagingDir
	sameDir, err := bCtx.EnsureStagingDir()
	if err != nil || sameDir != stagingDir {
		t.Errorf("EnsureStagingDir not idempotent: %s != %s", sameDir, stagingDir)
	}

	// StagingDir & DataDir accessors
	if bCtx.StagingDir() != stagingDir {
		t.Errorf("StagingDir accessor returned '%s', expected '%s'", bCtx.StagingDir(), stagingDir)
	}
	expectedDataDir := filepath.Join(stagingDir, "data")
	if bCtx.DataDir() != expectedDataDir {
		t.Errorf("DataDir returned '%s', expected '%s'", bCtx.DataDir(), expectedDataDir)
	}

	// Stage notification callback
	var recordedStages []Stage
	var recordedDetails []string
	bCtx.Options.OnStage = func(stage Stage, detail string) {
		recordedStages = append(recordedStages, stage)
		recordedDetails = append(recordedDetails, detail)
	}

	bCtx.NotifyStage(Stage3Launcher, "Synthesizing test launcher")
	if bCtx.CurrentStage != Stage3Launcher {
		t.Errorf("expected CurrentStage '%s', got '%s'", Stage3Launcher, bCtx.CurrentStage)
	}
	if len(recordedStages) != 1 || recordedStages[0] != Stage3Launcher {
		t.Errorf("callback not fired as expected: %v", recordedStages)
	}

	// Cleanup removes directory
	if err := bCtx.Cleanup(); err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}
	if _, err := os.Stat(stagingDir); !os.IsNotExist(err) {
		t.Errorf("staging dir still exists after Cleanup()")
	}

	// Idempotent Cleanup
	if err := bCtx.Cleanup(); err != nil {
		t.Errorf("second Cleanup call returned error: %v", err)
	}
}

func TestBuildContext_KeepStagingDir(t *testing.T) {
	opts := BuildOptions{
		KeepStagingDir: true,
	}
	bCtx, err := NewBuildContext(opts)
	if err != nil {
		t.Fatalf("NewBuildContext failed: %v", err)
	}

	stagingDir, err := bCtx.EnsureStagingDir()
	if err != nil {
		t.Fatalf("EnsureStagingDir failed: %v", err)
	}
	defer os.RemoveAll(stagingDir)

	// Cleanup should be a no-op when KeepStagingDir is true
	if err := bCtx.Cleanup(); err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}
	if fi, err := os.Stat(stagingDir); err != nil || !fi.IsDir() {
		t.Errorf("staging directory was removed despite KeepStagingDir: true")
	}
}

func TestManifest_ComputeSHA256(t *testing.T) {
	tempFile, err := os.CreateTemp("", "test-sha256-*")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile.Name())

	content := []byte("hello craftpack build orchestrator\n")
	if _, err := tempFile.Write(content); err != nil {
		t.Fatalf("failed writing to temp file: %v", err)
	}
	tempFile.Close()

	hash, size, err := ComputeSHA256(tempFile.Name())
	if err != nil {
		t.Fatalf("ComputeSHA256 failed: %v", err)
	}

	expectedSum := sha256.Sum256(content)
	expectedHex := hex.EncodeToString(expectedSum[:])

	if hash != expectedHex {
		t.Errorf("expected hash '%s', got '%s'", expectedHex, hash)
	}
	if size != int64(len(content)) {
		t.Errorf("expected size %d, got %d", len(content), size)
	}

	// Non-existent file error
	if _, _, err := ComputeSHA256(tempFile.Name() + ".nonexistent"); err == nil {
		t.Errorf("expected error for non-existent file, got nil")
	}
}

func TestManifest_FormattingAndParsing(t *testing.T) {
	hash := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	filename := "craftpack_1.0.0_amd64.deb"
	size := int64(4096)

	entryStr := FormatManifestEntry(hash, filename, size)
	expectedFormat := fmt.Sprintf("%s  %s  (%d bytes)\n", hash, filename, size)
	if entryStr != expectedFormat {
		t.Errorf("FormatManifestEntry mismatch:\nwant: %q\ngot:  %q", expectedFormat, entryStr)
	}

	// Parse valid formatted entry
	parsed, err := ParseManifestEntry(entryStr)
	if err != nil {
		t.Fatalf("ParseManifestEntry failed: %v", err)
	}
	if parsed.Hash != hash {
		t.Errorf("expected hash '%s', got '%s'", hash, parsed.Hash)
	}
	if parsed.Filename != filename {
		t.Errorf("expected filename '%s', got '%s'", filename, parsed.Filename)
	}
	if parsed.Size != size {
		t.Errorf("expected size %d, got %d", size, parsed.Size)
	}

	// Parse fallback standard sha256sum format
	fallbackStr := fmt.Sprintf("%s  %s", hash, filename)
	parsedFallback, err := ParseManifestEntry(fallbackStr)
	if err != nil {
		t.Fatalf("ParseManifestEntry fallback failed: %v", err)
	}
	if parsedFallback.Hash != hash || parsedFallback.Filename != filename {
		t.Errorf("fallback parsing incorrect: %+v", parsedFallback)
	}

	// Parse malformed lines
	if _, err := ParseManifestEntry(""); err == nil {
		t.Errorf("expected error for empty line")
	}
	if _, err := ParseManifestEntry("invalid line format"); err == nil {
		t.Errorf("expected error for invalid line format")
	}
}

func TestManifest_WriteAndReadManifest(t *testing.T) {
	dir, err := os.MkdirTemp("", "craftpack-manifest-test-*")
	if err != nil {
		t.Fatalf("failed creating temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	manifestPath := filepath.Join(dir, "sub", ManifestFileName)

	hash1 := strings.Repeat("a", 64)
	file1 := "pkg1_1.0.0_amd64.deb"
	size1 := int64(1024)

	// 1. Initial write
	if err := WriteOrUpdateManifest(manifestPath, hash1, file1, size1); err != nil {
		t.Fatalf("initial WriteOrUpdateManifest failed: %v", err)
	}

	entries, err := ReadManifest(manifestPath)
	if err != nil {
		t.Fatalf("ReadManifest failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Filename != file1 || entries[0].Hash != hash1 || entries[0].Size != size1 {
		t.Errorf("entry mismatch: %+v", entries[0])
	}

	// 2. Append second file
	hash2 := strings.Repeat("b", 64)
	file2 := "pkg2_1.0.0_arm64.deb"
	size2 := int64(2048)

	if err := WriteOrUpdateManifest(manifestPath, hash2, file2, size2); err != nil {
		t.Fatalf("second WriteOrUpdateManifest failed: %v", err)
	}

	entries, err = ReadManifest(manifestPath)
	if err != nil {
		t.Fatalf("ReadManifest failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[1].Filename != file2 || entries[1].Hash != hash2 || entries[1].Size != size2 {
		t.Errorf("second entry mismatch: %+v", entries[1])
	}

	// 3. Update existing file1 in place with new hash and size
	newHash1 := strings.Repeat("c", 64)
	newSize1 := int64(3072)

	if err := WriteOrUpdateManifest(manifestPath, newHash1, file1, newSize1); err != nil {
		t.Fatalf("updating WriteOrUpdateManifest failed: %v", err)
	}

	entries, err = ReadManifest(manifestPath)
	if err != nil {
		t.Fatalf("ReadManifest failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected still 2 entries, got %d", len(entries))
	}
	if entries[0].Filename != file1 || entries[0].Hash != newHash1 || entries[0].Size != newSize1 {
		t.Errorf("updated entry mismatch: %+v", entries[0])
	}
	if entries[1].Filename != file2 || entries[1].Hash != hash2 || entries[1].Size != size2 {
		t.Errorf("unrelated entry altered: %+v", entries[1])
	}
}

func TestOrchestrator_FullBuildPipeline(t *testing.T) {
	workspace, cleanup := setupMockWorkspace(t)
	defer cleanup()

	outDir := filepath.Join(workspace, "dist")
	fixedDate := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

	var stagesObserved []Stage
	opts := BuildOptions{
		SpecPath:       "craftpack.yml",
		WorkspaceDir:   workspace,
		OutputDir:      outDir,
		PackageVersion: "v2.5.0", // Leading 'v' should be normalized to "2.5.0"
		Target:         "deb",
		Architecture:   "amd64",
		BuildDate:      fixedDate,
		Strict:         true,
		OnStage: func(stage Stage, detail string) {
			stagesObserved = append(stagesObserved, stage)
		},
	}

	orchestrator := NewOrchestrator()
	bCtx, err := NewBuildContext(opts)
	if err != nil {
		t.Fatalf("NewBuildContext failed: %v", err)
	}

	res, err := orchestrator.Build(context.Background(), bCtx)
	if err != nil {
		t.Fatalf("orchestrator.Build failed: %v", err)
	}

	// Verify all 7 stages observed
	expectedStages := []Stage{
		Stage1CLIValidation,
		Stage2StagingPayload,
		Stage3Launcher,
		Stage4Documentation,
		Stage5TargetMetadata,
		Stage6Archive,
		Stage7ManifestCleanup,
	}
	if len(stagesObserved) != len(expectedStages) {
		t.Errorf("expected %d stages observed, got %d: %v", len(expectedStages), len(stagesObserved), stagesObserved)
	}

	// Verify BuildResult fields
	if !res.Success {
		t.Errorf("expected res.Success = true")
	}
	if res.DryRun {
		t.Errorf("expected res.DryRun = false")
	}
	if res.PackageName != "testapp" {
		t.Errorf("expected PackageName 'testapp', got '%s'", res.PackageName)
	}
	expectedFilename := "testapp_2.5.0_amd64.deb"
	if res.Filename != expectedFilename {
		t.Errorf("expected Filename '%s', got '%s'", expectedFilename, res.Filename)
	}
	if res.Version != "2.5.0" {
		t.Errorf("expected Version '2.5.0', got '%s'", res.Version)
	}
	if res.Architecture != "amd64" {
		t.Errorf("expected Architecture 'amd64', got '%s'", res.Architecture)
	}
	if res.Target != "deb" {
		t.Errorf("expected Target 'deb', got '%s'", res.Target)
	}

	// Verify emitted package file exists
	fi, err := os.Stat(res.PackageFile)
	if err != nil {
		t.Fatalf("emitted package file does not exist: %v", err)
	}
	if fi.Size() == 0 {
		t.Fatalf("emitted package file is 0 bytes")
	}
	if res.SizeBytes != fi.Size() {
		t.Errorf("SizeBytes %d != actual file size %d", res.SizeBytes, fi.Size())
	}

	// Verify SHA-256 matches actual file
	actualHash, actualSize, err := ComputeSHA256(res.PackageFile)
	if err != nil {
		t.Fatalf("ComputeSHA256 failed: %v", err)
	}
	if res.SHA256 != actualHash {
		t.Errorf("res.SHA256 '%s' != actual '%s'", res.SHA256, actualHash)
	}
	if actualSize != fi.Size() {
		t.Errorf("actual size mismatch: %d != %d", actualSize, fi.Size())
	}

	// Verify manifest file
	manifestPath := filepath.Join(outDir, ManifestFileName)
	if res.ManifestPath != manifestPath {
		t.Errorf("ManifestPath '%s' != expected '%s'", res.ManifestPath, manifestPath)
	}
	manifestEntries, err := ReadManifest(manifestPath)
	if err != nil {
		t.Fatalf("failed reading generated manifest: %v", err)
	}
	if len(manifestEntries) != 1 {
		t.Fatalf("expected 1 manifest entry, got %d", len(manifestEntries))
	}
	if manifestEntries[0].Filename != expectedFilename || manifestEntries[0].Hash != actualHash || manifestEntries[0].Size != actualSize {
		t.Errorf("manifest entry content mismatch: %+v", manifestEntries[0])
	}

	// Verify staging workspace was cleaned up
	if bCtx.StagingDir() != "" {
		if _, err := os.Stat(bCtx.StagingDir()); !os.IsNotExist(err) {
			t.Errorf("ephemeral staging directory was not cleaned up!")
		}
	}

	// Verify .deb internal archive layout using pure-Go reader
	verifyDebArchiveStructure(t, res.PackageFile)
}

func TestOrchestrator_DryRun(t *testing.T) {
	workspace, cleanup := setupMockWorkspace(t)
	defer cleanup()

	outDir := filepath.Join(workspace, "dist")
	opts := BuildOptions{
		SpecPath:       "craftpack.yml",
		WorkspaceDir:   workspace,
		OutputDir:      outDir,
		PackageVersion: "1.0.0",
		Target:         "deb",
		Architecture:   "amd64",
		DryRun:         true,
	}

	orchestrator := NewOrchestrator()
	res, err := orchestrator.BuildWithOptions(context.Background(), opts)
	if err != nil {
		t.Fatalf("dry run build failed: %v", err)
	}

	if !res.Success {
		t.Errorf("expected res.Success = true")
	}
	if !res.DryRun {
		t.Errorf("expected res.DryRun = true")
	}
	if res.PackageFile != "" {
		t.Errorf("expected empty PackageFile in dry run, got '%s'", res.PackageFile)
	}
	if res.SHA256 != "" {
		t.Errorf("expected empty SHA256 in dry run, got '%s'", res.SHA256)
	}
	if res.ManifestPath != "" {
		t.Errorf("expected empty ManifestPath in dry run, got '%s'", res.ManifestPath)
	}

	// Verify that output directory was NOT created or has no .deb or manifest
	if _, err := os.Stat(outDir); err == nil {
		files, _ := os.ReadDir(outDir)
		for _, f := range files {
			if strings.HasSuffix(f.Name(), ".deb") || f.Name() == ManifestFileName {
				t.Errorf("unexpected file in outDir during dry run: %s", f.Name())
			}
		}
	}

	// Verify staged files were populated
	if len(res.StagedFiles) == 0 {
		t.Errorf("expected StagedFiles to be non-empty in dry run")
	}
}

func TestOrchestrator_ValidationFailures(t *testing.T) {
	workspace, cleanup := setupMockWorkspace(t)
	defer cleanup()

	orchestrator := NewOrchestrator()

	tests := []struct {
		name        string
		opts        BuildOptions
		expectedErr string
	}{
		{
			name: "missing target",
			opts: BuildOptions{
				WorkspaceDir:   workspace,
				SpecPath:       "craftpack.yml",
				PackageVersion: "1.0.0",
				Target:         "",
			},
			expectedErr: "packaging target is mandatory",
		},
		{
			name: "unsupported target",
			opts: BuildOptions{
				WorkspaceDir:   workspace,
				SpecPath:       "craftpack.yml",
				PackageVersion: "1.0.0",
				Target:         "rpm",
			},
			expectedErr: "packaging target not supported",
		},
		{
			name: "missing package version",
			opts: BuildOptions{
				WorkspaceDir:   workspace,
				SpecPath:       "craftpack.yml",
				PackageVersion: "",
				Target:         "deb",
			},
			expectedErr: "package version is mandatory",
		},
		{
			name: "invalid workspace directory",
			opts: BuildOptions{
				WorkspaceDir:   filepath.Join(workspace, "does-not-exist"),
				SpecPath:       "craftpack.yml",
				PackageVersion: "1.0.0",
				Target:         "deb",
			},
			expectedErr: "workspace directory",
		},
		{
			name: "non-existent spec file",
			opts: BuildOptions{
				WorkspaceDir:   workspace,
				SpecPath:       "missing.yml",
				PackageVersion: "1.0.0",
				Target:         "deb",
			},
			expectedErr: "specification file",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := orchestrator.BuildWithOptions(context.Background(), tc.opts)
			if err == nil {
				t.Fatalf("expected error containing '%s', got nil", tc.expectedErr)
			}
			if !strings.Contains(err.Error(), tc.expectedErr) {
				t.Errorf("expected error to contain '%s', got: %v", tc.expectedErr, err)
			}
		})
	}
}

func TestOrchestrator_ContextCancellation(t *testing.T) {
	workspace, cleanup := setupMockWorkspace(t)
	defer cleanup()

	orchestrator := NewOrchestrator()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	opts := BuildOptions{
		WorkspaceDir:   workspace,
		SpecPath:       "craftpack.yml",
		PackageVersion: "1.0.0",
		Target:         "deb",
	}

	_, err := orchestrator.BuildWithOptions(ctx, opts)
	if err == nil {
		t.Fatalf("expected context cancellation error, got nil")
	}
	if !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Errorf("expected context.Canceled error, got: %v", err)
	}
}

// Helper to inspect the produced .deb archive internal structure.
func verifyDebArchiveStructure(t *testing.T, debPath string) {
	t.Helper()

	f, err := os.Open(debPath)
	if err != nil {
		t.Fatalf("failed opening deb file: %v", err)
	}
	defer f.Close()

	archive, err := deb.ReadDeb(f)
	if err != nil {
		t.Fatalf("failed parsing .deb container: %v", err)
	}

	// 1. Check debian-binary
	if string(archive.DebianBinary) != deb.DebianBinaryContent {
		t.Errorf("debian-binary content '%s' != '%s'", string(archive.DebianBinary), deb.DebianBinaryContent)
	}

	// 2. Check control.tar.gz
	controlGz, err := gzip.NewReader(bytes.NewReader(archive.ControlTarGz))
	if err != nil {
		t.Fatalf("failed opening control gzip stream: %v", err)
	}
	controlTar := tar.NewReader(controlGz)
	controlFiles := make(map[string]bool)
	for {
		thdr, err := controlTar.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("error reading control tar entry: %v", err)
		}
		controlFiles[thdr.Name] = true
	}
	controlGz.Close()

	if !controlFiles["control"] {
		t.Errorf("control.tar.gz missing 'control' file")
	}
	if !controlFiles["conffiles"] {
		t.Errorf("control.tar.gz missing 'conffiles' file")
	}
	if !controlFiles["md5sums"] {
		t.Errorf("control.tar.gz missing 'md5sums' file")
	}
	if !controlFiles["preinst"] {
		t.Errorf("control.tar.gz missing 'preinst' hook")
	}

	// 3. Check data.tar.gz
	dataGz, err := gzip.NewReader(bytes.NewReader(archive.DataTarGz))
	if err != nil {
		t.Fatalf("failed opening data gzip stream: %v", err)
	}
	dataTar := tar.NewReader(dataGz)
	dataFiles := make(map[string]int64)
	dataContents := make(map[string][]byte)
	for {
		thdr, err := dataTar.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("error reading data tar entry: %v", err)
		}
		dataFiles[thdr.Name] = thdr.Mode
		if !thdr.FileInfo().IsDir() {
			buf := new(bytes.Buffer)
			if _, err := io.Copy(buf, dataTar); err != nil {
				t.Fatalf("error reading data file content for %s: %v", thdr.Name, err)
			}
			dataContents[thdr.Name] = buf.Bytes()
		}
	}
	dataGz.Close()

	// Verify staged paths exist with correct modes in default direct binary mode (wrapper=false)
	expectedFiles := map[string]int64{
		"usr/bin/testapp":                 0755,
		"usr/lib/testapp/data.txt":        0644,
		"usr/share/man/man1/testapp.1.gz": 0644,
		"etc/testapp/testapp.conf":        0644,
	}

	for fPath, expectedMode := range expectedFiles {
		mode, ok := dataFiles[fPath]
		if !ok {
			t.Errorf("data.tar.gz missing expected file '%s'", fPath)
		} else if mode != expectedMode {
			t.Errorf("file '%s' has mode %o, expected %o", fPath, mode, expectedMode)
		}
	}

	// In direct binary mode (wrapper=false), the entrypoint is placed directly at usr/bin/testapp
	// and usr/lib/testapp/app-bin must NOT exist.
	if _, ok := dataFiles["usr/lib/testapp/app-bin"]; ok {
		t.Errorf("data.tar.gz unexpectedly contains 'usr/lib/testapp/app-bin' in direct binary mode")
	}

	// Verify usr/bin/testapp contains the raw executable binary content, not a launcher script
	binContent, ok := dataContents["usr/bin/testapp"]
	if !ok {
		t.Fatalf("usr/bin/testapp content missing from data archive")
	}
	if !bytes.Equal(binContent, []byte("#!/bin/sh\necho test\n")) {
		t.Errorf("usr/bin/testapp binary content mismatch, got: %s", string(binContent))
	}
}

func TestOrchestrator_Build_Debian_ExplicitWrapperTrue(t *testing.T) {
	workspace, cleanup := setupMockWorkspace(t)
	defer cleanup()

	// Update craftpack.yml in workspace to set wrapper: true
	specPath := filepath.Join(workspace, "craftpack.yml")
	specContent, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("failed reading craftpack.yml: %v", err)
	}
	updatedSpec := strings.Replace(string(specContent), "priority: optional", "priority: optional\n    wrapper: true", 1)
	if err := os.WriteFile(specPath, []byte(updatedSpec), fsutil.FileMode); err != nil {
		t.Fatalf("failed updating craftpack.yml: %v", err)
	}

	outDir := filepath.Join(workspace, "dist-wrapper")
	opts := BuildOptions{
		SpecPath:       "craftpack.yml",
		WorkspaceDir:   workspace,
		OutputDir:      outDir,
		PackageVersion: "1.0.0",
		Target:         "deb",
		Architecture:   "amd64",
	}

	orchestrator := NewOrchestrator()
	res, err := orchestrator.BuildWithOptions(context.Background(), opts)
	if err != nil {
		t.Fatalf("build with wrapper: true failed: %v", err)
	}

	// Verify staged files list contains both usr/lib entrypoint and usr/bin launcher
	hasLibBin := false
	hasBinLauncher := false
	for _, sf := range res.StagedFiles {
		if sf == "usr/lib/testapp/app-bin" {
			hasLibBin = true
		}
		if sf == "usr/bin/testapp" {
			hasBinLauncher = true
		}
	}
	if !hasLibBin {
		t.Errorf("expected StagedFiles to contain 'usr/lib/testapp/app-bin' when wrapper=true, got: %v", res.StagedFiles)
	}
	if !hasBinLauncher {
		t.Errorf("expected StagedFiles to contain 'usr/bin/testapp' when wrapper=true, got: %v", res.StagedFiles)
	}

	// Unpack and inspect debian container
	f, err := os.Open(res.PackageFile)
	if err != nil {
		t.Fatalf("failed opening deb file: %v", err)
	}
	defer f.Close()

	archive, err := deb.ReadDeb(f)
	if err != nil {
		t.Fatalf("failed parsing deb container: %v", err)
	}

	dataGz, err := gzip.NewReader(bytes.NewReader(archive.DataTarGz))
	if err != nil {
		t.Fatalf("failed opening data gzip stream: %v", err)
	}
	dataTar := tar.NewReader(dataGz)
	dataContents := make(map[string][]byte)
	for {
		thdr, err := dataTar.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("error reading data tar entry: %v", err)
		}
		if !thdr.FileInfo().IsDir() {
			buf := new(bytes.Buffer)
			if _, err := io.Copy(buf, dataTar); err != nil {
				t.Fatalf("error reading data file %s: %v", thdr.Name, err)
			}
			dataContents[thdr.Name] = buf.Bytes()
		}
	}
	dataGz.Close()

	// In wrapper mode, usr/bin/testapp is a POSIX launcher script
	launcherContent, ok := dataContents["usr/bin/testapp"]
	if !ok {
		t.Fatalf("usr/bin/testapp missing in wrapper mode")
	}
	if !strings.Contains(string(launcherContent), `REAL_PAYLOAD="/usr/lib/testapp/app-bin"`) {
		t.Errorf("expected launcher script to anchor to /usr/lib/testapp/app-bin, got:\n%s", string(launcherContent))
	}

	// In wrapper mode, usr/lib/testapp/app-bin contains the actual binary
	payloadContent, ok := dataContents["usr/lib/testapp/app-bin"]
	if !ok {
		t.Fatalf("usr/lib/testapp/app-bin missing in wrapper mode")
	}
	if !bytes.Equal(payloadContent, []byte("#!/bin/sh\necho test\n")) {
		t.Errorf("payload content mismatch: %s", string(payloadContent))
	}
}

func TestOrchestrator_Build_Debian_SingleBinaryPayload_NoUsrLib(t *testing.T) {
	dir, err := os.MkdirTemp("", "craftpack-single-bin-*")
	if err != nil {
		t.Fatalf("failed creating temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	specContent := `
name: singletool
description: Single binary application
maintainer: Marcin Kaim <marcin@example.com>
homepage: https://example.com/singletool
license: Apache-2.0
command: singletool
payload_dir: dist/bin
entrypoint: singletool
targets:
  deb:
    section: utils
    priority: optional
`
	if err := os.WriteFile(filepath.Join(dir, "craftpack.yml"), []byte(strings.TrimSpace(specContent)), fsutil.FileMode); err != nil {
		t.Fatalf("failed writing craftpack.yml: %v", err)
	}

	payloadDir := filepath.Join(dir, "dist", "bin")
	if err := os.MkdirAll(payloadDir, fsutil.DirMode); err != nil {
		t.Fatalf("failed creating payload dir: %v", err)
	}
	binData := []byte("#!/bin/sh\necho single\n")
	if err := os.WriteFile(filepath.Join(payloadDir, "singletool"), binData, fsutil.ExecMode); err != nil {
		t.Fatalf("failed writing binary: %v", err)
	}

	outDir := filepath.Join(dir, "dist")
	opts := BuildOptions{
		SpecPath:       "craftpack.yml",
		WorkspaceDir:   dir,
		OutputDir:      outDir,
		PackageVersion: "1.0.0",
		Target:         "deb",
		Architecture:   "amd64",
	}

	orchestrator := NewOrchestrator()
	res, err := orchestrator.BuildWithOptions(context.Background(), opts)
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}

	// Verify StagedFiles contains only usr/bin/singletool
	if len(res.StagedFiles) != 1 || res.StagedFiles[0] != "usr/bin/singletool" {
		t.Errorf("expected only 'usr/bin/singletool' staged, got: %v", res.StagedFiles)
	}

	// Unpack and verify NO usr/lib directory or file exists in the package
	f, err := os.Open(res.PackageFile)
	if err != nil {
		t.Fatalf("failed opening package: %v", err)
	}
	defer f.Close()

	archive, err := deb.ReadDeb(f)
	if err != nil {
		t.Fatalf("failed parsing deb: %v", err)
	}

	dataGz, err := gzip.NewReader(bytes.NewReader(archive.DataTarGz))
	if err != nil {
		t.Fatalf("failed opening data gzip stream: %v", err)
	}
	dataTar := tar.NewReader(dataGz)
	for {
		thdr, err := dataTar.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("error reading tar entry: %v", err)
		}
		if strings.HasPrefix(thdr.Name, "usr/lib") {
			t.Errorf("unexpected entry under usr/lib in single binary mode: %s", thdr.Name)
		}
	}
	dataGz.Close()
}

func TestRequiresWrapper_Matrix(t *testing.T) {
	tests := []struct {
		name       string
		cfg        *spec.CraftpackConfig
		targetName string
		want       bool
	}{
		{"nil config", nil, "deb", false},
		{"nil deb target", &spec.CraftpackConfig{}, "deb", false},
		{"deb wrapper false", &spec.CraftpackConfig{Targets: spec.TargetConfigs{Deb: &spec.DebianTargetConfig{Wrapper: false}}}, "deb", false},
		{"deb wrapper true", &spec.CraftpackConfig{Targets: spec.TargetConfigs{Deb: &spec.DebianTargetConfig{Wrapper: true}}}, "deb", true},
		{"deb uppercase DEB", &spec.CraftpackConfig{Targets: spec.TargetConfigs{Deb: &spec.DebianTargetConfig{Wrapper: true}}}, "DEB", true},
		{"deb mixed case Deb", &spec.CraftpackConfig{Targets: spec.TargetConfigs{Deb: &spec.DebianTargetConfig{Wrapper: true}}}, "Deb", true},
		{"rpm target with wrapper true", &spec.CraftpackConfig{Targets: spec.TargetConfigs{Deb: &spec.DebianTargetConfig{Wrapper: true}}}, "rpm", false},
		{"empty target name", &spec.CraftpackConfig{Targets: spec.TargetConfigs{Deb: &spec.DebianTargetConfig{Wrapper: true}}}, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := requiresWrapper(tt.cfg, tt.targetName)
			if got != tt.want {
				t.Errorf("requiresWrapper() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOrchestrator_Build_DirectBinary_DeeplyNestedEntrypoint_NoUsrLib(t *testing.T) {
	dir := t.TempDir()

	specContent := `
name: deepapp
description: Deeply nested entrypoint application
maintainer: Tester <tester@example.com>
homepage: https://example.com/deep
license: Apache-2.0
command: deepapp
payload_dir: payload
entrypoint: a/b/c/d/mytool
targets:
  deb:
    section: utils
    priority: optional
    wrapper: false
`
	if err := os.WriteFile(filepath.Join(dir, "craftpack.yml"), []byte(strings.TrimSpace(specContent)), fsutil.FileMode); err != nil {
		t.Fatalf("failed writing craftpack.yml: %v", err)
	}

	toolDir := filepath.Join(dir, "payload", "a", "b", "c", "d")
	if err := os.MkdirAll(toolDir, 0755); err != nil {
		t.Fatalf("failed creating nested payload dir: %v", err)
	}
	binData := []byte("#!/bin/sh\necho deep\n")
	if err := os.WriteFile(filepath.Join(toolDir, "mytool"), binData, 0755); err != nil {
		t.Fatalf("failed writing binary: %v", err)
	}

	outDir := filepath.Join(dir, "dist")
	opts := BuildOptions{
		SpecPath:       "craftpack.yml",
		WorkspaceDir:   dir,
		OutputDir:      outDir,
		PackageVersion: "1.0.0",
		Target:         "deb",
		Architecture:   "amd64",
	}

	orchestrator := NewOrchestrator()
	res, err := orchestrator.BuildWithOptions(context.Background(), opts)
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}

	// Verify only usr/bin/deepapp is staged
	if len(res.StagedFiles) != 1 || res.StagedFiles[0] != "usr/bin/deepapp" {
		t.Errorf("expected StagedFiles = ['usr/bin/deepapp'], got: %v", res.StagedFiles)
	}

	// Unpack and verify no usr/lib in package
	f, err := os.Open(res.PackageFile)
	if err != nil {
		t.Fatalf("failed opening package: %v", err)
	}
	defer f.Close()

	archive, err := deb.ReadDeb(f)
	if err != nil {
		t.Fatalf("failed parsing deb: %v", err)
	}

	dataGz, err := gzip.NewReader(bytes.NewReader(archive.DataTarGz))
	if err != nil {
		t.Fatalf("failed opening data gzip reader: %v", err)
	}
	dataTar := tar.NewReader(dataGz)
	for {
		thdr, err := dataTar.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar read error: %v", err)
		}
		if strings.HasPrefix(thdr.Name, "usr/lib") {
			t.Errorf("unexpected usr/lib entry in deeply nested single binary mode: %s", thdr.Name)
		}
	}
	dataGz.Close()
}

func TestOrchestrator_Build_DirectBinary_DeeplyNestedEntrypoint_WithAuxiliary(t *testing.T) {
	dir := t.TempDir()

	specContent := `
name: deepmulti
description: Deeply nested entrypoint with auxiliary assets
maintainer: Tester <tester@example.com>
homepage: https://example.com/deepmulti
license: Apache-2.0
command: deepmulti
payload_dir: payload
entrypoint: a/b/c/runner
targets:
  deb:
    section: utils
    priority: optional
    wrapper: false
`
	if err := os.WriteFile(filepath.Join(dir, "craftpack.yml"), []byte(strings.TrimSpace(specContent)), fsutil.FileMode); err != nil {
		t.Fatalf("failed writing craftpack.yml: %v", err)
	}

	binDir := filepath.Join(dir, "payload", "a", "b", "c")
	sharedDir := filepath.Join(dir, "payload", "a", "b")
	assetsDir := filepath.Join(dir, "payload", "assets")
	for _, d := range []string{binDir, sharedDir, assetsDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("failed creating dir: %v", err)
		}
	}

	runnerData := []byte("#!/bin/sh\necho runner\n")
	if err := os.WriteFile(filepath.Join(binDir, "runner"), runnerData, 0755); err != nil {
		t.Fatalf("failed writing runner: %v", err)
	}
	sharedData := []byte("shared-lib-data")
	if err := os.WriteFile(filepath.Join(sharedDir, "libshared.so"), sharedData, 0644); err != nil {
		t.Fatalf("failed writing shared lib: %v", err)
	}
	assetData := []byte("icon-data")
	if err := os.WriteFile(filepath.Join(assetsDir, "logo.png"), assetData, 0644); err != nil {
		t.Fatalf("failed writing asset: %v", err)
	}

	outDir := filepath.Join(dir, "dist")
	opts := BuildOptions{
		SpecPath:       "craftpack.yml",
		WorkspaceDir:   dir,
		OutputDir:      outDir,
		PackageVersion: "1.0.0",
		Target:         "deb",
		Architecture:   "amd64",
	}

	orchestrator := NewOrchestrator()
	res, err := orchestrator.BuildWithOptions(context.Background(), opts)
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}

	// Verify StagedFiles
	stagedSet := make(map[string]bool)
	for _, sf := range res.StagedFiles {
		stagedSet[sf] = true
	}
	if !stagedSet["usr/bin/deepmulti"] {
		t.Errorf("missing usr/bin/deepmulti in StagedFiles")
	}
	if !stagedSet["usr/lib/deepmulti/a/b/libshared.so"] {
		t.Errorf("missing usr/lib/deepmulti/a/b/libshared.so in StagedFiles")
	}
	if !stagedSet["usr/lib/deepmulti/assets/logo.png"] {
		t.Errorf("missing usr/lib/deepmulti/assets/logo.png in StagedFiles")
	}
	if stagedSet["usr/lib/deepmulti/a/b/c/runner"] {
		t.Errorf("unexpected entrypoint duplicate in usr/lib/deepmulti/a/b/c/runner")
	}

	// Verify archive contents
	f, err := os.Open(res.PackageFile)
	if err != nil {
		t.Fatalf("failed opening deb: %v", err)
	}
	defer f.Close()

	archive, err := deb.ReadDeb(f)
	if err != nil {
		t.Fatalf("failed reading deb: %v", err)
	}

	dataGz, err := gzip.NewReader(bytes.NewReader(archive.DataTarGz))
	if err != nil {
		t.Fatalf("failed reading gzip: %v", err)
	}
	dataTar := tar.NewReader(dataGz)
	archiveFiles := make(map[string][]byte)
	for {
		thdr, err := dataTar.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar next error: %v", err)
		}
		if !thdr.FileInfo().IsDir() {
			buf := new(bytes.Buffer)
			if _, err := io.Copy(buf, dataTar); err != nil {
				t.Fatalf("failed reading tar data: %v", err)
			}
			archiveFiles[thdr.Name] = buf.Bytes()
		}
	}
	dataGz.Close()

	// Direct binary at usr/bin/deepmulti
	if !bytes.Equal(archiveFiles["usr/bin/deepmulti"], runnerData) {
		t.Errorf("usr/bin/deepmulti content mismatch")
	}
	// Auxiliary files in usr/lib
	if !bytes.Equal(archiveFiles["usr/lib/deepmulti/a/b/libshared.so"], sharedData) {
		t.Errorf("usr/lib/deepmulti/a/b/libshared.so content mismatch")
	}
	if !bytes.Equal(archiveFiles["usr/lib/deepmulti/assets/logo.png"], assetData) {
		t.Errorf("usr/lib/deepmulti/assets/logo.png content mismatch")
	}
	// No entrypoint in usr/lib
	if _, ok := archiveFiles["usr/lib/deepmulti/a/b/c/runner"]; ok {
		t.Errorf("entrypoint duplicate found in usr/lib/deepmulti/a/b/c/runner")
	}
}

func TestOrchestrator_Build_DirectBinary_PermissionNormalization(t *testing.T) {
	dir := t.TempDir()

	specContent := `
name: permtool
description: Permission normalization test
maintainer: Tester <tester@example.com>
homepage: https://example.com/perm
license: Apache-2.0
command: permtool
payload_dir: payload
entrypoint: permtool
targets:
  deb:
    section: utils
    priority: optional
    wrapper: false
`
	if err := os.WriteFile(filepath.Join(dir, "craftpack.yml"), []byte(strings.TrimSpace(specContent)), fsutil.FileMode); err != nil {
		t.Fatalf("failed writing craftpack.yml: %v", err)
	}

	payloadDir := filepath.Join(dir, "payload")
	if err := os.MkdirAll(payloadDir, 0755); err != nil {
		t.Fatalf("failed creating payload dir: %v", err)
	}
	// Write file with mode 0644 (non-executable on host filesystem)
	if err := os.WriteFile(filepath.Join(payloadDir, "permtool"), []byte("#!/bin/sh\necho perm\n"), 0644); err != nil {
		t.Fatalf("failed writing file: %v", err)
	}

	outDir := filepath.Join(dir, "dist")
	opts := BuildOptions{
		SpecPath:       "craftpack.yml",
		WorkspaceDir:   dir,
		OutputDir:      outDir,
		PackageVersion: "1.0.0",
		Target:         "deb",
		Architecture:   "amd64",
	}

	orchestrator := NewOrchestrator()
	res, err := orchestrator.BuildWithOptions(context.Background(), opts)
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}

	// Verify usr/bin/permtool has mode 0755 in the data tarball
	f, err := os.Open(res.PackageFile)
	if err != nil {
		t.Fatalf("failed opening package: %v", err)
	}
	defer f.Close()

	archive, err := deb.ReadDeb(f)
	if err != nil {
		t.Fatalf("failed reading deb: %v", err)
	}
	dataGz, err := gzip.NewReader(bytes.NewReader(archive.DataTarGz))
	if err != nil {
		t.Fatalf("failed opening gzip: %v", err)
	}
	dataTar := tar.NewReader(dataGz)
	foundBin := false
	for {
		thdr, err := dataTar.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar error: %v", err)
		}
		if thdr.Name == "usr/bin/permtool" {
			foundBin = true
			if thdr.Mode != 0755 {
				t.Errorf("expected mode 0755 for usr/bin/permtool, got %o", thdr.Mode)
			}
		}
	}
	dataGz.Close()
	if !foundBin {
		t.Fatalf("usr/bin/permtool missing from data archive")
	}
}

func TestOrchestrator_Build_DirectBinary_DifferentCommandAndName(t *testing.T) {
	dir := t.TempDir()

	specContent := `
name: suite-package
description: App with distinct command and name
maintainer: Tester <tester@example.com>
homepage: https://example.com/suite
license: Apache-2.0
command: suite-cli
payload_dir: payload
entrypoint: bin/exec
targets:
  deb:
    section: utils
    priority: optional
    wrapper: false
`
	if err := os.WriteFile(filepath.Join(dir, "craftpack.yml"), []byte(strings.TrimSpace(specContent)), fsutil.FileMode); err != nil {
		t.Fatalf("failed writing craftpack.yml: %v", err)
	}

	payloadDir := filepath.Join(dir, "payload", "bin")
	if err := os.MkdirAll(payloadDir, 0755); err != nil {
		t.Fatalf("failed creating payload dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(payloadDir, "exec"), []byte("#!/bin/sh\necho exec\n"), 0755); err != nil {
		t.Fatalf("failed writing exec: %v", err)
	}

	libDir := filepath.Join(dir, "payload", "lib")
	if err := os.MkdirAll(libDir, 0755); err != nil {
		t.Fatalf("failed creating lib dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(libDir, "plugin.so"), []byte("plugin"), 0644); err != nil {
		t.Fatalf("failed writing plugin: %v", err)
	}

	outDir := filepath.Join(dir, "dist")
	opts := BuildOptions{
		SpecPath:       "craftpack.yml",
		WorkspaceDir:   dir,
		OutputDir:      outDir,
		PackageVersion: "1.0.0",
		Target:         "deb",
		Architecture:   "amd64",
	}

	orchestrator := NewOrchestrator()
	res, err := orchestrator.BuildWithOptions(context.Background(), opts)
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}

	// 1. Entrypoint staged at usr/bin/suite-cli (using command)
	stagedSet := make(map[string]bool)
	for _, sf := range res.StagedFiles {
		stagedSet[sf] = true
	}
	if !stagedSet["usr/bin/suite-cli"] {
		t.Errorf("missing usr/bin/suite-cli in StagedFiles: %v", res.StagedFiles)
	}
	if stagedSet["usr/bin/suite-package"] {
		t.Errorf("unexpected usr/bin/suite-package in StagedFiles")
	}

	// 2. Auxiliary asset staged at usr/lib/suite-package/lib/plugin.so (using name)
	if !stagedSet["usr/lib/suite-package/lib/plugin.so"] {
		t.Errorf("missing usr/lib/suite-package/lib/plugin.so in StagedFiles: %v", res.StagedFiles)
	}
}

func TestOrchestrator_Build_DryRun_WrapperComparison(t *testing.T) {
	dir := t.TempDir()

	payloadDir := filepath.Join(dir, "payload")
	if err := os.MkdirAll(payloadDir, 0755); err != nil {
		t.Fatalf("failed creating payload dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(payloadDir, "runner"), []byte("#!/bin/sh\necho r\n"), 0755); err != nil {
		t.Fatalf("failed writing runner: %v", err)
	}

	baseSpec := `
name: drytool
description: Dry run comparison test
maintainer: Tester <tester@example.com>
homepage: https://example.com/dry
license: Apache-2.0
command: drytool
payload_dir: payload
entrypoint: runner
targets:
  deb:
    section: utils
    priority: optional
`
	// 1. DryRun with wrapper: false (default)
	specFalse := filepath.Join(dir, "craftpack-false.yml")
	if err := os.WriteFile(specFalse, []byte(strings.TrimSpace(baseSpec)+"\n    wrapper: false\n"), 0644); err != nil {
		t.Fatalf("failed writing spec: %v", err)
	}

	orchestrator := NewOrchestrator()
	resFalse, err := orchestrator.BuildWithOptions(context.Background(), BuildOptions{
		SpecPath:       specFalse,
		WorkspaceDir:   dir,
		OutputDir:      filepath.Join(dir, "dist"),
		PackageVersion: "1.0.0",
		Target:         "deb",
		Architecture:   "amd64",
		DryRun:         true,
	})
	if err != nil {
		t.Fatalf("dry-run build false failed: %v", err)
	}
	if !resFalse.DryRun {
		t.Errorf("expected DryRun=true")
	}
	if len(resFalse.StagedFiles) != 1 || resFalse.StagedFiles[0] != "usr/bin/drytool" {
		t.Errorf("wrapper: false dry-run expected StagedFiles = ['usr/bin/drytool'], got: %v", resFalse.StagedFiles)
	}

	// 2. DryRun with wrapper: true
	specTrue := filepath.Join(dir, "craftpack-true.yml")
	if err := os.WriteFile(specTrue, []byte(strings.TrimSpace(baseSpec)+"\n    wrapper: true\n"), 0644); err != nil {
		t.Fatalf("failed writing spec: %v", err)
	}

	resTrue, err := orchestrator.BuildWithOptions(context.Background(), BuildOptions{
		SpecPath:       specTrue,
		WorkspaceDir:   dir,
		OutputDir:      filepath.Join(dir, "dist"),
		PackageVersion: "1.0.0",
		Target:         "deb",
		Architecture:   "amd64",
		DryRun:         true,
	})
	if err != nil {
		t.Fatalf("dry-run build true failed: %v", err)
	}
	if !resTrue.DryRun {
		t.Errorf("expected DryRun=true")
	}
	hasBin := false
	hasLib := false
	for _, sf := range resTrue.StagedFiles {
		if sf == "usr/bin/drytool" {
			hasBin = true
		}
		if sf == "usr/lib/drytool/runner" {
			hasLib = true
		}
	}
	if !hasBin || !hasLib {
		t.Errorf("wrapper: true dry-run expected both launcher and lib entrypoint, got: %v", resTrue.StagedFiles)
	}
}

func TestOrchestrator_Stage3_NotificationLogging(t *testing.T) {
	dir := t.TempDir()

	payloadDir := filepath.Join(dir, "payload")
	if err := os.MkdirAll(payloadDir, 0755); err != nil {
		t.Fatalf("failed creating payload dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(payloadDir, "runner"), []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("failed writing runner: %v", err)
	}

	baseSpec := `
name: notifapp
description: Stage 3 notification test
maintainer: Tester <tester@example.com>
homepage: https://example.com/notif
license: Apache-2.0
command: notifapp
payload_dir: payload
entrypoint: runner
targets:
  deb:
    section: utils
    priority: optional
`
	// 1. Direct mode notifications
	var stage3DetailsFalse string
	specFalse := filepath.Join(dir, "craftpack-false.yml")
	_ = os.WriteFile(specFalse, []byte(strings.TrimSpace(baseSpec)+"\n    wrapper: false\n"), 0644)

	orchestrator := NewOrchestrator()
	_, err := orchestrator.BuildWithOptions(context.Background(), BuildOptions{
		SpecPath:       specFalse,
		WorkspaceDir:   dir,
		OutputDir:      filepath.Join(dir, "dist"),
		PackageVersion: "1.0.0",
		Target:         "deb",
		Architecture:   "amd64",
		DryRun:         true,
		OnStage: func(stage Stage, detail string) {
			if stage == Stage3Launcher {
				stage3DetailsFalse = detail
			}
		},
	})
	if err != nil {
		t.Fatalf("build false failed: %v", err)
	}
	if !strings.Contains(stage3DetailsFalse, "Direct binary placement enabled") {
		t.Errorf("expected Stage 3 bypass detail for wrapper=false, got: %q", stage3DetailsFalse)
	}

	// 2. Wrapper mode notifications
	var stage3DetailsTrue string
	specTrue := filepath.Join(dir, "craftpack-true.yml")
	_ = os.WriteFile(specTrue, []byte(strings.TrimSpace(baseSpec)+"\n    wrapper: true\n"), 0644)

	_, err = orchestrator.BuildWithOptions(context.Background(), BuildOptions{
		SpecPath:       specTrue,
		WorkspaceDir:   dir,
		OutputDir:      filepath.Join(dir, "dist"),
		PackageVersion: "1.0.0",
		Target:         "deb",
		Architecture:   "amd64",
		DryRun:         true,
		OnStage: func(stage Stage, detail string) {
			if stage == Stage3Launcher {
				stage3DetailsTrue = detail
			}
		},
	})
	if err != nil {
		t.Fatalf("build true failed: %v", err)
	}
	if !strings.Contains(stage3DetailsTrue, "Synthesizing proxy launcher script") {
		t.Errorf("expected Stage 3 synthesis detail for wrapper=true, got: %q", stage3DetailsTrue)
	}
}

func TestBuilder_ScaffoldedPassiveStub_DirectAndWrapper(t *testing.T) {
	for _, wrap := range []bool{false, true} {
		t.Run(fmt.Sprintf("wrapper_%v", wrap), func(t *testing.T) {
			dir := t.TempDir()
			outDir := filepath.Join(dir, "out")

			// 1. Scaffold workspace with passive entrypoint stub
			payloadDir := filepath.Join(dir, "scaffold_payload", "bin")
			if err := os.MkdirAll(payloadDir, 0755); err != nil {
				t.Fatalf("failed creating payload dir: %v", err)
			}
			stubPath := filepath.Join(payloadDir, "stub-app")
			stubContent := []byte("#!/bin/sh\nexit 0\n")
			if err := os.WriteFile(stubPath, stubContent, 0755); err != nil {
				t.Fatalf("failed writing passive stub: %v", err)
			}

			spec := fmt.Sprintf(`name: stub-app
description: Day-Zero scaffolded application
maintainer: Developer <dev@example.org>
homepage: https://example.org/stub
license: Apache-2.0
command: stub-app
payload_dir: scaffold_payload
entrypoint: bin/stub-app
targets:
  deb:
    section: utils
    priority: optional
    wrapper: %v
`, wrap)
			specFile := filepath.Join(dir, "craftpack.yml")
			if err := os.WriteFile(specFile, []byte(spec), 0644); err != nil {
				t.Fatalf("failed writing spec: %v", err)
			}

			// 2. Build via Orchestrator
			orchestrator := NewOrchestrator()
			res, err := orchestrator.BuildWithOptions(context.Background(), BuildOptions{
				SpecPath:       specFile,
				WorkspaceDir:   dir,
				OutputDir:      outDir,
				PackageVersion: "0.1.0",
				Target:         "deb",
				Architecture:   "amd64",
			})
			if err != nil {
				t.Fatalf("build failed for wrapper=%v: %v", wrap, err)
			}
			if !res.Success {
				t.Errorf("expected build success, got false")
			}

			// 3. Verify staged files and package
			if _, err := os.Stat(res.PackageFile); err != nil {
				t.Errorf("expected package file %s to exist: %v", res.PackageFile, err)
			}

			hasBin := false
			hasLib := false
			for _, sf := range res.StagedFiles {
				if sf == "usr/bin/stub-app" {
					hasBin = true
				}
				if strings.HasPrefix(sf, "usr/lib/") {
					hasLib = true
				}
			}

			if !hasBin {
				t.Errorf("staged files missing usr/bin/stub-app: %v", res.StagedFiles)
			}
			if wrap {
				if !hasLib {
					t.Errorf("wrapper=true expected files under usr/lib, got: %v", res.StagedFiles)
				}
			} else {
				if hasLib {
					t.Errorf("wrapper=false unexpectedly staged files under usr/lib: %v", res.StagedFiles)
				}
			}
		})
	}
}



