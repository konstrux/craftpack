// SPDX-FileCopyrightText: 2026 Marcin Kaim
// SPDX-License-Identifier: Apache-2.0

package integration_test

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// setupSelfPackagingWorkspace copies repository assets needed for self-packaging
// into an isolated workspace and compiles the fresh craftpack binary into dist/payload/bin/craftpack.
func setupSelfPackagingWorkspace(t *testing.T) (string, string) {
	t.Helper()

	rootDir, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("failed resolving root directory: %v", err)
	}

	wsDir := t.TempDir()

	// 1. Copy craftpack.yml
	specData, err := os.ReadFile(filepath.Join(rootDir, "craftpack.yml"))
	if err != nil {
		t.Fatalf("failed reading root craftpack.yml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, "craftpack.yml"), specData, 0644); err != nil {
		t.Fatalf("failed copying craftpack.yml: %v", err)
	}

	// 2. Copy docs/manual.md
	if err := os.MkdirAll(filepath.Join(wsDir, "docs"), 0755); err != nil {
		t.Fatalf("failed creating docs dir: %v", err)
	}
	manData, err := os.ReadFile(filepath.Join(rootDir, "docs", "manual.md"))
	if err != nil {
		t.Fatalf("failed reading docs/manual.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, "docs", "manual.md"), manData, 0644); err != nil {
		t.Fatalf("failed copying manual.md: %v", err)
	}

	// 3. Copy config/craftpack.default.yml
	if err := os.MkdirAll(filepath.Join(wsDir, "config"), 0755); err != nil {
		t.Fatalf("failed creating config dir: %v", err)
	}
	cfgData, err := os.ReadFile(filepath.Join(rootDir, "config", "craftpack.default.yml"))
	if err != nil {
		t.Fatalf("failed reading config/craftpack.default.yml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, "config", "craftpack.default.yml"), cfgData, 0644); err != nil {
		t.Fatalf("failed copying craftpack.default.yml: %v", err)
	}

	// 4. Compile binary N directly into dist/payload/bin/craftpack
	payloadBinDir := filepath.Join(wsDir, "dist", "payload", "bin")
	if err := os.MkdirAll(payloadBinDir, 0755); err != nil {
		t.Fatalf("failed creating payload bin directory: %v", err)
	}
	compiledBinary := filepath.Join(payloadBinDir, "craftpack")

	cmdBuild := exec.Command("go", "build", "-ldflags=-s -w -X main.version=1.0.0", "-o", compiledBinary, "./cmd/craftpack")
	cmdBuild.Dir = rootDir
	cmdBuild.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmdBuild.CombinedOutput(); err != nil {
		t.Fatalf("failed compiling craftpack into payload: %v\nOutput:\n%s", err, string(out))
	}

	return wsDir, compiledBinary
}

// TestSelfPackaging_EndToEnd tests the complete N -> N self-packaging pipeline per CR-2026-001.
func TestSelfPackaging_EndToEnd(t *testing.T) {
	wsDir, payloadBin := setupSelfPackagingWorkspace(t)
	outDir := filepath.Join(wsDir, "dist")

	// -------------------------------------------------------------------------
	// Stage A: Validate specification using freshly compiled binary N
	// -------------------------------------------------------------------------
	valCmd := exec.Command(payloadBin, "validate", "--spec", "craftpack.yml", "--strict", "--json")
	valCmd.Dir = wsDir
	var valStdout, valStderr bytes.Buffer
	valCmd.Stdout = &valStdout
	valCmd.Stderr = &valStderr

	if err := valCmd.Run(); err != nil {
		t.Fatalf("self-package validation failed: %v\nSTDERR:\n%s", err, valStderr.String())
	}

	var valResult struct {
		Valid    bool     `json:"valid"`
		Package  string   `json:"package"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal(valStdout.Bytes(), &valResult); err != nil {
		t.Fatalf("failed parsing validation JSON output: %v\nRaw:\n%s", err, valStdout.String())
	}
	if !valResult.Valid {
		t.Errorf("expected validation to be valid, got false")
	}
	if valResult.Package != "craftpack" {
		t.Errorf("expected package name 'craftpack', got %q", valResult.Package)
	}
	if len(valResult.Warnings) != 0 {
		t.Errorf("expected 0 warnings in strict validation, got: %v", valResult.Warnings)
	}

	// -------------------------------------------------------------------------
	// Stage B: Self-Package using freshly compiled binary N
	// -------------------------------------------------------------------------
	buildCmd := exec.Command(payloadBin,
		"build",
		"--spec", "craftpack.yml",
		"--target", "deb",
		"--package-version", "1.0.0",
		"--output-dir", outDir,
		"-v",
	)
	buildCmd.Dir = wsDir
	var buildStdout, buildStderr bytes.Buffer
	buildCmd.Stdout = &buildStdout
	buildCmd.Stderr = &buildStderr

	if err := buildCmd.Run(); err != nil {
		t.Fatalf("self-packaging build failed: %v\nSTDERR:\n%s", err, buildStderr.String())
	}

	// Strict stream separation: STDOUT must be empty
	if buildStdout.Len() != 0 {
		t.Errorf("build STDOUT must be empty, got: %s", buildStdout.String())
	}

	// Verify all 7 lifecycle stages logged in STDERR
	stderrLog := buildStderr.String()
	for stageNum := 1; stageNum <= 7; stageNum++ {
		stagePattern := fmt.Sprintf("Stage %d:", stageNum)
		if !strings.Contains(stderrLog, stagePattern) {
			t.Errorf("missing %s in build logs:\n%s", stagePattern, stderrLog)
		}
	}
	if !strings.Contains(stderrLog, "Build completed successfully") {
		t.Errorf("missing success confirmation in build logs")
	}

	// -------------------------------------------------------------------------
	// Stage C: Inspect generated .deb package archive
	// -------------------------------------------------------------------------
	expectedArch := runtime.GOARCH
	expectedDebName := fmt.Sprintf("craftpack_1.0.0_%s.deb", expectedArch)
	debPath := filepath.Join(outDir, expectedDebName)

	debInfo, err := os.Stat(debPath)
	if err != nil {
		t.Fatalf("generated package %s not found: %v", debPath, err)
	}
	if debInfo.Size() == 0 {
		t.Fatalf("generated package %s is empty (0 bytes)", debPath)
	}

	// Unpack and inspect archive contents
	unpacked := unpackDeb(t, debPath)

	// 1. debian-binary
	if unpacked.DebianBinary != "2.0\n" {
		t.Errorf("debian-binary = %q, want \"2.0\\n\"", unpacked.DebianBinary)
	}

	// 2. DEBIAN/control
	controlData, ok := unpacked.ControlFiles["control"]
	if !ok {
		t.Fatalf("DEBIAN/control missing in control archive")
	}
	controlStr := string(controlData)
	assertControlField(t, controlStr, "Package", "craftpack")
	assertControlField(t, controlStr, "Version", "1.0.0")
	assertControlField(t, controlStr, "Architecture", expectedArch)
	assertControlField(t, controlStr, "Maintainer", "Marcin Kaim <9829098+marcinkaim@users.noreply.github.com>")
	assertControlField(t, controlStr, "Homepage", "https://github.com/marcinkaim/craftpack")
	assertControlField(t, controlStr, "Section", "utils")
	assertControlField(t, controlStr, "Priority", "optional")
	assertControlField(t, controlStr, "Depends", "libc6 (>= 2.31)")
	if !strings.Contains(controlStr, "Description: Standardized Linux packaging factory") {
		t.Errorf("control file missing expected description:\n%s", controlStr)
	}

	// 3. DEBIAN/conffiles
	conffilesData, ok := unpacked.ControlFiles["conffiles"]
	if !ok {
		t.Fatalf("DEBIAN/conffiles missing in control archive")
	}
	conffilesStr := string(conffilesData)
	if !strings.Contains(conffilesStr, "/etc/craftpack/craftpack.yml") {
		t.Errorf("conffiles missing /etc/craftpack/craftpack.yml:\n%s", conffilesStr)
	}

	// 4. DEBIAN/md5sums
	md5Data, ok := unpacked.ControlFiles["md5sums"]
	if !ok {
		t.Fatalf("DEBIAN/md5sums missing in control archive")
	}
	if len(md5Data) == 0 {
		t.Errorf("DEBIAN/md5sums is empty")
	}

	// 5. data.tar.gz - Proxy Launcher
	launcherData, ok := unpacked.DataFiles["/usr/bin/craftpack"]
	if !ok {
		t.Fatalf("proxy launcher /usr/bin/craftpack missing from data archive")
	}
	launcherHdr := unpacked.DataHeaders["/usr/bin/craftpack"]
	if launcherHdr != nil && launcherHdr.FileInfo().Mode().Perm() != 0755 {
		t.Errorf("launcher permissions = %o, want 0755", launcherHdr.FileInfo().Mode().Perm())
	}
	launcherStr := string(launcherData)
	if !strings.HasPrefix(launcherStr, "#!/bin/sh") {
		t.Errorf("launcher script must start with #!/bin/sh, got:\n%s", launcherStr)
	}
	if !strings.Contains(launcherStr, `REAL_PAYLOAD="/usr/lib/craftpack/bin/craftpack"`) {
		t.Errorf("launcher script target incorrect:\n%s", launcherStr)
	}
	if !strings.Contains(launcherStr, `exec "$REAL_PAYLOAD" "$@"`) {
		t.Errorf("launcher script missing POSIX exec delegation:\n%s", launcherStr)
	}

	// 6. data.tar.gz - Payload Binary
	payloadData, ok := unpacked.DataFiles["/usr/lib/craftpack/bin/craftpack"]
	if !ok {
		t.Fatalf("payload binary /usr/lib/craftpack/bin/craftpack missing from data archive")
	}
	payloadHdr := unpacked.DataHeaders["/usr/lib/craftpack/bin/craftpack"]
	if payloadHdr != nil && payloadHdr.FileInfo().Mode().Perm() != 0755 {
		t.Errorf("payload binary permissions = %o, want 0755", payloadHdr.FileInfo().Mode().Perm())
	}
	origPayloadData, err := os.ReadFile(payloadBin)
	if err != nil {
		t.Fatalf("failed reading original payload binary: %v", err)
	}
	if !bytes.Equal(payloadData, origPayloadData) {
		t.Errorf("packaged payload binary does not match compiled binary (%d vs %d bytes)", len(payloadData), len(origPayloadData))
	}

	// 7. data.tar.gz - Manual Page
	manGzData, ok := unpacked.DataFiles["/usr/share/man/man1/craftpack.1.gz"]
	if !ok {
		t.Fatalf("manual page /usr/share/man/man1/craftpack.1.gz missing from data archive")
	}
	manHdr := unpacked.DataHeaders["/usr/share/man/man1/craftpack.1.gz"]
	if manHdr != nil && manHdr.FileInfo().Mode().Perm() != 0644 {
		t.Errorf("manual page permissions = %o, want 0644", manHdr.FileInfo().Mode().Perm())
	}
	gzReader, err := gzip.NewReader(bytes.NewReader(manGzData))
	if err != nil {
		t.Fatalf("failed decompressing manual page: %v", err)
	}
	roffBytes, err := io.ReadAll(gzReader)
	_ = gzReader.Close()
	if err != nil {
		t.Fatalf("failed reading decompressed roff manual page: %v", err)
	}
	roffStr := string(roffBytes)
	if !strings.Contains(roffStr, ".TH CRAFTPACK(1)") {
		t.Errorf("manual page missing .TH CRAFTPACK(1)")
	}
	if !strings.Contains(roffStr, ".SH NAME") || !strings.Contains(roffStr, ".SH DESCRIPTION") {
		t.Errorf("manual page missing essential section headers")
	}

	// 8. data.tar.gz - Default Configuration
	confData, ok := unpacked.DataFiles["/etc/craftpack/craftpack.yml"]
	if !ok {
		t.Fatalf("default config /etc/craftpack/craftpack.yml missing from data archive")
	}
	confHdr := unpacked.DataHeaders["/etc/craftpack/craftpack.yml"]
	if confHdr != nil && confHdr.FileInfo().Mode().Perm() != 0644 {
		t.Errorf("default config permissions = %o, want 0644", confHdr.FileInfo().Mode().Perm())
	}
	expectedCfgData, _ := os.ReadFile(filepath.Join(wsDir, "config", "craftpack.default.yml"))
	if !bytes.Equal(confData, expectedCfgData) {
		t.Errorf("deployed config does not match config/craftpack.default.yml")
	}

	// -------------------------------------------------------------------------
	// Stage D: Verify Release Manifest (checksums.sha256)
	// -------------------------------------------------------------------------
	manifestPath := filepath.Join(outDir, "checksums.sha256")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("release manifest checksums.sha256 missing: %v", err)
	}

	debBytes, err := os.ReadFile(debPath)
	if err != nil {
		t.Fatalf("failed reading deb file: %v", err)
	}
	hash := sha256.Sum256(debBytes)
	expectedHex := hex.EncodeToString(hash[:])

	manifestStr := string(manifestData)
	if !strings.Contains(manifestStr, expectedHex) {
		t.Errorf("manifest missing expected SHA-256 hash %s:\n%s", expectedHex, manifestStr)
	}
	if !strings.Contains(manifestStr, expectedDebName) {
		t.Errorf("manifest missing expected deb filename %s:\n%s", expectedDebName, manifestStr)
	}
	expectedSizeTag := fmt.Sprintf("(%d bytes)", len(debBytes))
	if !strings.Contains(manifestStr, expectedSizeTag) {
		t.Errorf("manifest missing expected size tag %s:\n%s", expectedSizeTag, manifestStr)
	}
}

// TestSelfPackaging_ReproducibleBuild asserts bit-for-bit reproducibility using SOURCE_DATE_EPOCH.
func TestSelfPackaging_ReproducibleBuild(t *testing.T) {
	wsDir, payloadBin := setupSelfPackagingWorkspace(t)
	outDir1 := filepath.Join(wsDir, "dist1")
	outDir2 := filepath.Join(wsDir, "dist2")

	epoch := "1700000000"

	runBuild := func(outDir string) string {
		cmd := exec.Command(payloadBin,
			"build",
			"--spec", "craftpack.yml",
			"--target", "deb",
			"--package-version", "1.0.0",
			"--output-dir", outDir,
		)
		cmd.Dir = wsDir
		cmd.Env = append(os.Environ(), "SOURCE_DATE_EPOCH="+epoch)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("build failed for %s: %v\nOutput:\n%s", outDir, err, string(out))
		}

		debName := fmt.Sprintf("craftpack_1.0.0_%s.deb", runtime.GOARCH)
		debPath := filepath.Join(outDir, debName)
		data, err := os.ReadFile(debPath)
		if err != nil {
			t.Fatalf("failed reading %s: %v", debPath, err)
		}
		sum := sha256.Sum256(data)
		return hex.EncodeToString(sum[:])
	}

	hash1 := runBuild(outDir1)
	hash2 := runBuild(outDir2)

	if hash1 != hash2 {
		t.Errorf("deterministic builds produced different hashes under SOURCE_DATE_EPOCH=%s:\nRun 1: %s\nRun 2: %s", epoch, hash1, hash2)
	}
}

// TestSelfPackaging_PackagedBinaryExecution verifies that the packaged payload binary
// extracted from data.tar.gz executes successfully with full version and telemetry support.
func TestSelfPackaging_PackagedBinaryExecution(t *testing.T) {
	wsDir, payloadBin := setupSelfPackagingWorkspace(t)
	outDir := filepath.Join(wsDir, "dist")

	buildCmd := exec.Command(payloadBin,
		"build",
		"--spec", "craftpack.yml",
		"--target", "deb",
		"--package-version", "1.0.0",
		"--output-dir", outDir,
	)
	buildCmd.Dir = wsDir
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\nOutput:\n%s", err, string(out))
	}

	debName := fmt.Sprintf("craftpack_1.0.0_%s.deb", runtime.GOARCH)
	debPath := filepath.Join(outDir, debName)
	unpacked := unpackDeb(t, debPath)

	extractedBinData, ok := unpacked.DataFiles["/usr/lib/craftpack/bin/craftpack"]
	if !ok {
		t.Fatalf("payload binary missing from package")
	}

	// Write extracted binary to temporary location
	extractDir := t.TempDir()
	extractedBinPath := filepath.Join(extractDir, "craftpack")
	if err := os.WriteFile(extractedBinPath, extractedBinData, 0755); err != nil {
		t.Fatalf("failed writing extracted binary: %v", err)
	}

	// 1. Run --version
	verCmd := exec.Command(extractedBinPath, "--version")
	verOut, err := verCmd.Output()
	if err != nil {
		t.Fatalf("extracted binary --version failed: %v", err)
	}
	if !strings.Contains(string(verOut), "1.0.0") {
		t.Errorf("expected version 1.0.0, got: %s", string(verOut))
	}

	// 2. Run --version-info --json
	infoCmd := exec.Command(extractedBinPath, "--version-info", "--json")
	infoOut, err := infoCmd.Output()
	if err != nil {
		t.Fatalf("extracted binary --version-info --json failed: %v", err)
	}
	var telemetry struct {
		Version   string `json:"version"`
		GoVersion string `json:"go_version"`
		Arch      string `json:"arch"`
		OS        string `json:"os"`
	}
	if err := json.Unmarshal(infoOut, &telemetry); err != nil {
		t.Fatalf("failed parsing telemetry JSON: %v\nRaw:\n%s", err, string(infoOut))
	}
	if telemetry.Version != "1.0.0" {
		t.Errorf("telemetry version = %s, want 1.0.0", telemetry.Version)
	}
	if telemetry.Arch != runtime.GOARCH {
		t.Errorf("telemetry arch = %s, want %s", telemetry.Arch, runtime.GOARCH)
	}
	if telemetry.OS != runtime.GOOS {
		t.Errorf("telemetry os = %s, want %s", telemetry.OS, runtime.GOOS)
	}
}

// assertControlField asserts that a key-value header is present in Debian control content.
func assertControlField(t *testing.T, controlContent, field, wantValue string) {
	t.Helper()
	prefix := field + ":"
	for _, line := range strings.Split(controlContent, "\n") {
		if strings.HasPrefix(line, prefix) {
			gotValue := strings.TrimSpace(strings.TrimPrefix(line, prefix))
			if gotValue != wantValue {
				t.Errorf("control field %q: got %q, want %q", field, gotValue, wantValue)
			}
			return
		}
	}
	t.Errorf("control field %q missing from control content:\n%s", field, controlContent)
}
