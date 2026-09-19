// SPDX-FileCopyrightText: 2026 Marcin Kaim
// SPDX-License-Identifier: GPL-3.0-only

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

	"craftpack/pkg/cli"
	"craftpack/pkg/spec"
)

// setupSelfPackagingWorkspace copies repository assets needed for self-packaging
// into an isolated workspace and compiles the fresh craftpack binary into dist/payload/bin/craftpack.
// It returns the workspace directory, the path to the compiled binary, and the dynamic package version.
func setupSelfPackagingWorkspace(t *testing.T) (string, string, string) {
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

	// 4. Compile binary N directly into dist/payload/bin/craftpack using dynamic package version
	testVer := cli.CleanVersion(cli.Version)
	payloadBinDir := filepath.Join(wsDir, "dist", "payload", "bin")
	if err := os.MkdirAll(payloadBinDir, 0755); err != nil {
		t.Fatalf("failed creating payload bin directory: %v", err)
	}
	compiledBinary := filepath.Join(payloadBinDir, "craftpack")

	cmdBuild := exec.Command("go", "build",
		fmt.Sprintf("-ldflags=-s -w -X main.version=%s -X craftpack/pkg/cli.Version=%s", testVer, testVer),
		"-o", compiledBinary, "./cmd/craftpack")
	cmdBuild.Dir = rootDir
	cmdBuild.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmdBuild.CombinedOutput(); err != nil {
		t.Fatalf("failed compiling craftpack into payload: %v\nOutput:\n%s", err, string(out))
	}

	return wsDir, compiledBinary, testVer
}

// TestSelfPackaging_EndToEnd tests the complete N -> N self-packaging pipeline per CR-2026-001.
func TestSelfPackaging_EndToEnd(t *testing.T) {
	wsDir, payloadBin, testVer := setupSelfPackagingWorkspace(t)
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
		"--package-version", testVer,
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
	expectedDebName := fmt.Sprintf("craftpack_%s_%s.deb", testVer, expectedArch)
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
	assertControlField(t, controlStr, "Version", testVer)
	assertControlField(t, controlStr, "Architecture", expectedArch)

	// Validate metadata dynamically against the parsed spec to avoid hardcoding personal identity or repository URLs
	parsedSpec, err := spec.ParseFile(filepath.Join(wsDir, "craftpack.yml"), spec.ParseOptions{})
	if err != nil {
		t.Fatalf("failed parsing workspace craftpack.yml: %v", err)
	}
	maintainer := getControlField(t, controlStr, "Maintainer")
	if maintainer == "" || maintainer != parsedSpec.Config.Maintainer {
		t.Errorf("control Maintainer = %q, want %q", maintainer, parsedSpec.Config.Maintainer)
	}
	if !strings.Contains(maintainer, "@") {
		t.Errorf("control Maintainer missing RFC 822 email address: %q", maintainer)
	}
	homepage := getControlField(t, controlStr, "Homepage")
	if homepage == "" || homepage != parsedSpec.Config.Homepage {
		t.Errorf("control Homepage = %q, want %q", homepage, parsedSpec.Config.Homepage)
	}
	if !strings.HasPrefix(homepage, "http://") && !strings.HasPrefix(homepage, "https://") {
		t.Errorf("control Homepage must be valid HTTP/HTTPS URL: %q", homepage)
	}

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
	md5Str := string(md5Data)
	if !strings.Contains(md5Str, "usr/bin/craftpack") {
		t.Errorf("DEBIAN/md5sums missing usr/bin/craftpack:\n%s", md5Str)
	}
	if !strings.Contains(md5Str, "etc/craftpack/craftpack.yml") {
		t.Errorf("DEBIAN/md5sums missing etc/craftpack/craftpack.yml:\n%s", md5Str)
	}
	if !strings.Contains(md5Str, "usr/share/man/man1/craftpack.1.gz") {
		t.Errorf("DEBIAN/md5sums missing usr/share/man/man1/craftpack.1.gz:\n%s", md5Str)
	}
	if strings.Contains(md5Str, "usr/lib") {
		t.Errorf("DEBIAN/md5sums unexpectedly contains usr/lib entries in direct mode:\n%s", md5Str)
	}

	// 5. data.tar.gz - Direct Binary Placement at /usr/bin/craftpack
	binData, ok := unpacked.DataFiles["/usr/bin/craftpack"]
	if !ok {
		t.Fatalf("direct binary /usr/bin/craftpack missing from data archive")
	}
	binHdr := unpacked.DataHeaders["/usr/bin/craftpack"]
	if binHdr != nil && binHdr.FileInfo().Mode().Perm() != 0755 {
		t.Errorf("binary permissions = %o, want 0755", binHdr.FileInfo().Mode().Perm())
	}
	origPayloadData, err := os.ReadFile(payloadBin)
	if err != nil {
		t.Fatalf("failed reading original payload binary: %v", err)
	}
	if !bytes.Equal(binData, origPayloadData) {
		t.Errorf("packaged direct binary does not match compiled binary (%d vs %d bytes)", len(binData), len(origPayloadData))
	}
	binStr := string(binData)
	if strings.HasPrefix(binStr, "#!/bin/sh") && strings.Contains(binStr, "REAL_PAYLOAD") {
		t.Errorf("expected direct executable binary at /usr/bin/craftpack, got proxy launcher script")
	}

	// 6. data.tar.gz - Assert /usr/lib/craftpack is absent (clean single binary root)
	if _, ok := unpacked.DataFiles["/usr/lib/craftpack/bin/craftpack"]; ok {
		t.Errorf("unexpected payload binary found at /usr/lib/craftpack/bin/craftpack in direct mode")
	}
	for path := range unpacked.DataFiles {
		if strings.HasPrefix(path, "/usr/lib") {
			t.Errorf("unexpected entry under /usr/lib in single binary mode: %s", path)
		}
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
	wsDir, payloadBin, testVer := setupSelfPackagingWorkspace(t)
	outDir1 := filepath.Join(wsDir, "dist1")
	outDir2 := filepath.Join(wsDir, "dist2")

	epoch := "1700000000"

	runBuild := func(outDir string) string {
		cmd := exec.Command(payloadBin,
			"build",
			"--spec", "craftpack.yml",
			"--target", "deb",
			"--package-version", testVer,
			"--output-dir", outDir,
		)
		cmd.Dir = wsDir
		cmd.Env = append(os.Environ(), "SOURCE_DATE_EPOCH="+epoch)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("build failed for %s: %v\nOutput:\n%s", outDir, err, string(out))
		}

		debName := fmt.Sprintf("craftpack_%s_%s.deb", testVer, runtime.GOARCH)
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
	wsDir, payloadBin, testVer := setupSelfPackagingWorkspace(t)
	outDir := filepath.Join(wsDir, "dist")

	buildCmd := exec.Command(payloadBin,
		"build",
		"--spec", "craftpack.yml",
		"--target", "deb",
		"--package-version", testVer,
		"--output-dir", outDir,
	)
	buildCmd.Dir = wsDir
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\nOutput:\n%s", err, string(out))
	}

	debName := fmt.Sprintf("craftpack_%s_%s.deb", testVer, runtime.GOARCH)
	debPath := filepath.Join(outDir, debName)
	unpacked := unpackDeb(t, debPath)

	extractedBinData, ok := unpacked.DataFiles["/usr/bin/craftpack"]
	if !ok {
		t.Fatalf("payload binary missing from package at /usr/bin/craftpack")
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
	if !strings.Contains(string(verOut), testVer) {
		t.Errorf("expected version %s, got: %s", testVer, string(verOut))
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
	if telemetry.Version != testVer {
		t.Errorf("telemetry version = %s, want %s", telemetry.Version, testVer)
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

// getControlField retrieves the value of a key-value header in Debian control content.
func getControlField(t *testing.T, controlContent, field string) string {
	t.Helper()
	prefix := field + ":"
	for _, line := range strings.Split(controlContent, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	t.Errorf("control field %q missing from control content:\n%s", field, controlContent)
	return ""
}

// TestSelfPackaging_WrapperTrue_EndToEnd verifies that Craftpack can package itself
// in isolated vault mode when targets.deb.wrapper: true is explicitly configured.
func TestSelfPackaging_WrapperTrue_EndToEnd(t *testing.T) {
	wsDir, payloadBin, testVer := setupSelfPackagingWorkspace(t)
	outDir := filepath.Join(wsDir, "dist-wrap")

	// Update craftpack.yml in workspace to set wrapper: true
	specFile := filepath.Join(wsDir, "craftpack.yml")
	specContent, err := os.ReadFile(specFile)
	if err != nil {
		t.Fatalf("failed reading craftpack.yml: %v", err)
	}
	newSpec := strings.Replace(string(specContent), "wrapper: false", "wrapper: true", 1)
	if err := os.WriteFile(specFile, []byte(newSpec), 0644); err != nil {
		t.Fatalf("failed updating craftpack.yml with wrapper: true: %v", err)
	}

	wrapVer := testVer + "-wrap"
	buildCmd := exec.Command(payloadBin,
		"build",
		"--spec", "craftpack.yml",
		"--target", "deb",
		"--package-version", wrapVer,
		"--output-dir", outDir,
		"-v",
	)
	buildCmd.Dir = wsDir
	var buildStdout, buildStderr bytes.Buffer
	buildCmd.Stdout = &buildStdout
	buildCmd.Stderr = &buildStderr

	if err := buildCmd.Run(); err != nil {
		t.Fatalf("self-packaging build with wrapper: true failed: %v\nSTDERR:\n%s", err, buildStderr.String())
	}

	expectedArch := runtime.GOARCH
	debPath := filepath.Join(outDir, fmt.Sprintf("craftpack_%s_%s.deb", wrapVer, expectedArch))
	unpacked := unpackDeb(t, debPath)

	// 1. /usr/bin/craftpack must be the proxy launcher
	launcherData, ok := unpacked.DataFiles["/usr/bin/craftpack"]
	if !ok {
		t.Fatalf("proxy launcher /usr/bin/craftpack missing in data archive")
	}
	launcherStr := string(launcherData)
	if !strings.HasPrefix(launcherStr, "#!/bin/sh") {
		t.Errorf("launcher script must start with #!/bin/sh, got:\n%s", launcherStr)
	}
	if !strings.Contains(launcherStr, `REAL_PAYLOAD="/usr/lib/craftpack/bin/craftpack"`) {
		t.Errorf("launcher target payload incorrect:\n%s", launcherStr)
	}
	if !strings.Contains(launcherStr, `exec "$REAL_PAYLOAD" "$@"`) {
		t.Errorf("launcher missing POSIX exec delegation:\n%s", launcherStr)
	}

	// 2. /usr/lib/craftpack/bin/craftpack must be the compiled payload binary
	payloadData, ok := unpacked.DataFiles["/usr/lib/craftpack/bin/craftpack"]
	if !ok {
		t.Fatalf("payload binary /usr/lib/craftpack/bin/craftpack missing in data archive")
	}
	origPayloadData, err := os.ReadFile(payloadBin)
	if err != nil {
		t.Fatalf("failed reading original payload binary: %v", err)
	}
	if !bytes.Equal(payloadData, origPayloadData) {
		t.Errorf("payload binary mismatch (%d vs %d bytes)", len(payloadData), len(origPayloadData))
	}
	payloadHdr := unpacked.DataHeaders["/usr/lib/craftpack/bin/craftpack"]
	if payloadHdr != nil && payloadHdr.FileInfo().Mode().Perm() != 0755 {
		t.Errorf("payload binary permissions = %o, want 0755", payloadHdr.FileInfo().Mode().Perm())
	}

	// 3. md5sums must contain both launcher and payload binary
	md5Data, ok := unpacked.ControlFiles["md5sums"]
	if !ok {
		t.Fatalf("DEBIAN/md5sums missing in control archive")
	}
	md5Str := string(md5Data)
	if !strings.Contains(md5Str, "usr/bin/craftpack") {
		t.Errorf("DEBIAN/md5sums missing usr/bin/craftpack:\n%s", md5Str)
	}
	if !strings.Contains(md5Str, "usr/lib/craftpack/bin/craftpack") {
		t.Errorf("DEBIAN/md5sums missing usr/lib/craftpack/bin/craftpack:\n%s", md5Str)
	}

	// 4. Test executing the extracted private binary directly
	tmpDir := t.TempDir()
	extractedBin := filepath.Join(tmpDir, "craftpack")
	if err := os.WriteFile(extractedBin, payloadData, 0755); err != nil {
		t.Fatalf("failed writing extracted binary: %v", err)
	}
	out, err := exec.Command(extractedBin, "--version").Output()
	if err != nil {
		t.Fatalf("extracted binary failed running --version: %v", err)
	}
	if !strings.Contains(string(out), testVer) {
		t.Errorf("extracted binary unexpected version output: %s", string(out))
	}
}

// TestSelfPackaging_PackagedBinaryBuildsPackage asserts the N -> N -> N+1 cycle:
// The binary packaged inside the self-packaged .deb is extracted and used to build
// an application from source specification.
func TestSelfPackaging_PackagedBinaryBuildsPackage(t *testing.T) {
	wsDir, payloadBin, testVer := setupSelfPackagingWorkspace(t)
	outDir := filepath.Join(wsDir, "dist")

	// 1. Build self-package N
	buildCmd := exec.Command(payloadBin,
		"build",
		"--spec", "craftpack.yml",
		"--target", "deb",
		"--package-version", testVer,
		"--output-dir", outDir,
	)
	buildCmd.Dir = wsDir
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("self-packaging failed: %v\nOutput:\n%s", err, string(out))
	}

	debName := fmt.Sprintf("craftpack_%s_%s.deb", testVer, runtime.GOARCH)
	debPath := filepath.Join(outDir, debName)
	unpacked := unpackDeb(t, debPath)

	extractedBinData, ok := unpacked.DataFiles["/usr/bin/craftpack"]
	if !ok {
		t.Fatalf("payload binary missing from package at /usr/bin/craftpack")
	}

	// 2. Write packaged binary to a new directory
	pkgBinDir := t.TempDir()
	extractedCraftpack := filepath.Join(pkgBinDir, "craftpack")
	if err := os.WriteFile(extractedCraftpack, extractedBinData, 0755); err != nil {
		t.Fatalf("failed writing extracted craftpack binary: %v", err)
	}

	// 3. Use the extracted packaged binary to validate and build valid-minimal fixture (N+1)
	rootDir, _ := filepath.Abs("../..")
	minimalSpec := filepath.Join(rootDir, "test", "fixtures", "valid-minimal", "craftpack.yml")

	// Validate fixture
	valCmd := exec.Command(extractedCraftpack, "validate", "--spec", minimalSpec, "--strict")
	if out, err := valCmd.CombinedOutput(); err != nil {
		t.Fatalf("packaged craftpack failed validating minimal fixture: %v\nOutput:\n%s", err, string(out))
	}

	// Build fixture
	nestedOutDir := filepath.Join(t.TempDir(), "nested-out")
	nestedBuildCmd := exec.Command(extractedCraftpack,
		"build",
		"--spec", minimalSpec,
		"--target", "deb",
		"--package-version", "0.9.0",
		"--output-dir", nestedOutDir,
	)
	if out, err := nestedBuildCmd.CombinedOutput(); err != nil {
		t.Fatalf("packaged craftpack failed building minimal fixture: %v\nOutput:\n%s", err, string(out))
	}

	// Verify the produced minimal package
	entries, err := os.ReadDir(nestedOutDir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("packaged craftpack did not produce output files in %s", nestedOutDir)
	}

	var nestedDeb string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".deb") {
			nestedDeb = filepath.Join(nestedOutDir, e.Name())
			break
		}
	}
	if nestedDeb == "" {
		t.Fatalf("no .deb found in nested build output: %v", entries)
	}

	nestedUnpacked := unpackDeb(t, nestedDeb)
	if _, ok := nestedUnpacked.DataFiles["/usr/bin/minimal-app"]; !ok {
		t.Errorf("nested build missing /usr/bin/minimal-app in package")
	}
	for path := range nestedUnpacked.DataFiles {
		if strings.HasPrefix(path, "/usr/lib") {
			t.Errorf("nested build unexpectedly contains /usr/lib: %s", path)
		}
	}
}

