// SPDX-FileCopyrightText: 2026 Marcin Kaim
// SPDX-License-Identifier: Apache-2.0

package integration_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/blakesmith/ar"
)

var (
	binPathOnce sync.Once
	binaryPath  string
	buildErr    error
)

// getCraftpackBinary builds the craftpack CLI executable once and returns its path.
func getCraftpackBinary(t *testing.T) string {
	t.Helper()
	binPathOnce.Do(func() {
		tmpDir, err := os.MkdirTemp("", "craftpack-integ-bin-*")
		if err != nil {
			buildErr = err
			return
		}
		targetBin := filepath.Join(tmpDir, "craftpack")

		// Locate project root relative to test/integration
		rootDir, err := filepath.Abs("../..")
		if err != nil {
			buildErr = err
			return
		}

		cmd := exec.Command("go", "build", "-ldflags=-s -w", "-o", targetBin, "./cmd/craftpack")
		cmd.Dir = rootDir
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		output, err := cmd.CombinedOutput()
		if err != nil {
			buildErr = fmt.Errorf("failed to compile craftpack CLI: %w\nOutput: %s", err, string(output))
			return
		}
		binaryPath = targetBin
	})

	if buildErr != nil {
		t.Fatalf("getCraftpackBinary failed: %v", buildErr)
	}
	return binaryPath
}

// UnpackedDeb represents the dissected contents and metadata of a Debian package (.deb).
type UnpackedDeb struct {
	DebianBinary string
	ControlFiles map[string][]byte
	DataFiles    map[string][]byte
	DataHeaders  map[string]*tar.Header
}

// unpackDeb opens a .deb archive using pure Go and validates its ar structure and payloads.
func unpackDeb(t *testing.T, debPath string) *UnpackedDeb {
	t.Helper()

	f, err := os.Open(debPath)
	if err != nil {
		t.Fatalf("failed to open deb file %q: %v", debPath, err)
	}
	defer f.Close()

	arReader := ar.NewReader(f)
	result := &UnpackedDeb{
		ControlFiles: make(map[string][]byte),
		DataFiles:    make(map[string][]byte),
		DataHeaders:  make(map[string]*tar.Header),
	}

	// 1. Entry 1: debian-binary
	hdr1, err := arReader.Next()
	if err != nil {
		t.Fatalf("failed reading ar header 1: %v", err)
	}
	if hdr1.Name != "debian-binary" {
		t.Errorf("ar entry 1: name = %q, want 'debian-binary'", hdr1.Name)
	}
	if hdr1.Mode != 0644 {
		t.Errorf("ar entry 1: mode = %o, want 0644", hdr1.Mode)
	}
	content1, err := io.ReadAll(arReader)
	if err != nil {
		t.Fatalf("failed reading debian-binary: %v", err)
	}
	result.DebianBinary = string(content1)
	if result.DebianBinary != "2.0\n" {
		t.Errorf("debian-binary content = %q, want '2.0\\n'", result.DebianBinary)
	}

	// 2. Entry 2: control.tar.gz
	hdr2, err := arReader.Next()
	if err != nil {
		t.Fatalf("failed reading ar header 2: %v", err)
	}
	if hdr2.Name != "control.tar.gz" {
		t.Errorf("ar entry 2: name = %q, want 'control.tar.gz'", hdr2.Name)
	}
	if hdr2.Mode != 0644 {
		t.Errorf("ar entry 2: mode = %o, want 0644", hdr2.Mode)
	}
	controlBytes, err := io.ReadAll(arReader)
	if err != nil {
		t.Fatalf("failed reading control.tar.gz bytes: %v", err)
	}

	gzControl, err := gzip.NewReader(bytes.NewReader(controlBytes))
	if err != nil {
		t.Fatalf("failed creating gzip reader for control.tar.gz: %v", err)
	}
	defer gzControl.Close()

	tarControl := tar.NewReader(gzControl)
	for {
		thdr, err := tarControl.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("failed reading control tar header: %v", err)
		}

		// Verify deterministic ownership
		if thdr.Uid != 0 || thdr.Gid != 0 {
			t.Errorf("control tar header %s: Uid/Gid = %d/%d, want 0/0", thdr.Name, thdr.Uid, thdr.Gid)
		}
		if thdr.Uname != "root" || thdr.Gname != "root" {
			t.Errorf("control tar header %s: Uname/Gname = %q/%q, want root/root", thdr.Name, thdr.Uname, thdr.Gname)
		}

		cleanName := strings.TrimPrefix(filepath.Clean(thdr.Name), "./")
		cleanName = strings.TrimPrefix(cleanName, ".")
		cleanName = strings.TrimPrefix(cleanName, "/")

		if thdr.Typeflag == tar.TypeReg {
			data, err := io.ReadAll(tarControl)
			if err != nil {
				t.Fatalf("failed reading file %s in control.tar.gz: %v", thdr.Name, err)
			}
			result.ControlFiles[cleanName] = data
		}
	}

	// 3. Entry 3: data.tar.gz
	hdr3, err := arReader.Next()
	if err != nil {
		t.Fatalf("failed reading ar header 3: %v", err)
	}
	if hdr3.Name != "data.tar.gz" {
		t.Errorf("ar entry 3: name = %q, want 'data.tar.gz'", hdr3.Name)
	}
	if hdr3.Mode != 0644 {
		t.Errorf("ar entry 3: mode = %o, want 0644", hdr3.Mode)
	}
	dataBytes, err := io.ReadAll(arReader)
	if err != nil {
		t.Fatalf("failed reading data.tar.gz bytes: %v", err)
	}

	gzData, err := gzip.NewReader(bytes.NewReader(dataBytes))
	if err != nil {
		t.Fatalf("failed creating gzip reader for data.tar.gz: %v", err)
	}
	defer gzData.Close()

	tarData := tar.NewReader(gzData)
	for {
		thdr, err := tarData.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("failed reading data tar header: %v", err)
		}

		// Verify deterministic ownership
		if thdr.Uid != 0 || thdr.Gid != 0 {
			t.Errorf("data tar header %s: Uid/Gid = %d/%d, want 0/0", thdr.Name, thdr.Uid, thdr.Gid)
		}
		if thdr.Uname != "root" || thdr.Gname != "root" {
			t.Errorf("data tar header %s: Uname/Gname = %q/%q, want root/root", thdr.Name, thdr.Uname, thdr.Gname)
		}

		cleanPath := "/" + strings.TrimPrefix(filepath.ToSlash(filepath.Clean(thdr.Name)), "./")
		cleanPath = filepath.Clean(cleanPath)

		result.DataHeaders[cleanPath] = thdr

		if thdr.Typeflag == tar.TypeReg {
			data, err := io.ReadAll(tarData)
			if err != nil {
				t.Fatalf("failed reading file %s in data.tar.gz: %v", thdr.Name, err)
			}
			result.DataFiles[cleanPath] = data
		}
	}

	// Verify no trailing entries in ar container
	_, err = arReader.Next()
	if err != io.EOF {
		t.Errorf("expected EOF after data.tar.gz, got: %v", err)
	}

	return result
}

func TestIntegration_Build_ValidMinimal(t *testing.T) {
	bin := getCraftpackBinary(t)
	rootDir, _ := filepath.Abs("../..")
	specPath := filepath.Join(rootDir, "test/fixtures/valid-minimal/craftpack.yml")
	outDir := t.TempDir()

	cmd := exec.Command(bin,
		"build",
		"--spec", specPath,
		"--target", "deb",
		"--package-version", "1.0.0",
		"--output-dir", outDir,
		"-v",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		t.Fatalf("craftpack build failed: %v\nSTDERR:\n%s", err, stderr.String())
	}

	// 1. STDOUT must be 0 bytes (strict stream separation)
	if stdout.Len() != 0 {
		t.Errorf("STDOUT must be clean (0 bytes), got %d bytes:\n%s", stdout.Len(), stdout.String())
	}

	// 2. Locate generated .deb package
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("failed reading outDir: %v", err)
	}

	var debFile string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".deb") {
			debFile = filepath.Join(outDir, e.Name())
			break
		}
	}
	if debFile == "" {
		t.Fatalf("no .deb package found in output directory %s", outDir)
	}

	// 3. Unpack and verify pure-Go .deb structure
	unpacked := unpackDeb(t, debFile)

	// Verify control file
	ctrlData, ok := unpacked.ControlFiles["control"]
	if !ok {
		t.Fatalf("DEBIAN/control not found in control.tar.gz")
	}
	ctrlStr := string(ctrlData)
	if !strings.Contains(ctrlStr, "Package: minimal-app") {
		t.Errorf("control missing Package: minimal-app\n%s", ctrlStr)
	}
	if !strings.Contains(ctrlStr, "Version: 1.0.0") {
		t.Errorf("control missing Version: 1.0.0\n%s", ctrlStr)
	}
	if !strings.Contains(ctrlStr, "Maintainer: Jane Doe <jane.doe@example.com>") {
		t.Errorf("control missing Maintainer\n%s", ctrlStr)
	}

	// Verify conffiles is omitted when default_config is empty
	if _, ok := unpacked.ControlFiles["conffiles"]; ok {
		t.Errorf("conffiles should not exist when default_config is omitted")
	}

	// Verify launcher in data.tar.gz
	launcherData, ok := unpacked.DataFiles["/usr/bin/minimal-app"]
	if !ok {
		t.Fatalf("/usr/bin/minimal-app launcher missing in data.tar.gz")
	}
	launcherStr := string(launcherData)
	if !strings.HasPrefix(launcherStr, "#!/bin/sh") {
		t.Errorf("launcher script missing #!/bin/sh shebang: %s", launcherStr)
	}
	if !strings.Contains(launcherStr, `REAL_PAYLOAD="/usr/lib/minimal-app/minimal-app"`) {
		t.Errorf("launcher missing absolute path anchor: %s", launcherStr)
	}
	if !strings.Contains(launcherStr, `exec "$REAL_PAYLOAD" "$@"`) {
		t.Errorf("launcher missing exec delegation: %s", launcherStr)
	}

	// Verify payload in /usr/lib/minimal-app/minimal-app
	payloadData, ok := unpacked.DataFiles["/usr/lib/minimal-app/minimal-app"]
	if !ok {
		t.Fatalf("/usr/lib/minimal-app/minimal-app payload missing in data.tar.gz")
	}
	if !strings.Contains(string(payloadData), "minimal-app running") {
		t.Errorf("payload content unexpected: %s", string(payloadData))
	}

	// Verify launcher permissions
	launcherHdr := unpacked.DataHeaders["/usr/bin/minimal-app"]
	if launcherHdr.Mode != 0755 {
		t.Errorf("launcher permissions = %o, want 0755", launcherHdr.Mode)
	}

	// 4. Verify checksums.sha256 manifest
	manifestPath := filepath.Join(outDir, "checksums.sha256")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("failed reading manifest: %v", err)
	}

	debBytes, _ := os.ReadFile(debFile)
	actualHash := sha256.Sum256(debBytes)
	actualHashStr := hex.EncodeToString(actualHash[:])
	actualSize := len(debBytes)

	manifestLine := string(manifestData)
	if !strings.Contains(manifestLine, actualHashStr) {
		t.Errorf("manifest does not contain actual SHA-256 hash %s:\n%s", actualHashStr, manifestLine)
	}
	if !strings.Contains(manifestLine, filepath.Base(debFile)) {
		t.Errorf("manifest does not contain package filename %s:\n%s", filepath.Base(debFile), manifestLine)
	}
	if !strings.Contains(manifestLine, fmt.Sprintf("(%d bytes)", actualSize)) {
		t.Errorf("manifest does not contain file size %d bytes:\n%s", actualSize, manifestLine)
	}
}

func TestIntegration_Build_ValidFull(t *testing.T) {
	bin := getCraftpackBinary(t)
	rootDir, _ := filepath.Abs("../..")
	specPath := filepath.Join(rootDir, "test/fixtures/valid-full/craftpack.yml")
	outDir := t.TempDir()

	// Use leading v in package version to assert normalization
	cmd := exec.Command(bin,
		"build",
		"--spec", specPath,
		"--target", "deb",
		"--package-version", "v2.1.0",
		"--output-dir", outDir,
		"-v",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		t.Fatalf("craftpack build failed: %v\nSTDERR:\n%s", err, stderr.String())
	}

	// STDOUT clean
	if stdout.Len() != 0 {
		t.Errorf("STDOUT must be 0 bytes, got: %s", stdout.String())
	}

	// Locate .deb package
	entries, _ := os.ReadDir(outDir)
	var debFile string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".deb") {
			debFile = filepath.Join(outDir, e.Name())
			break
		}
	}
	if debFile == "" {
		t.Fatalf("no .deb package created in %s", outDir)
	}

	unpacked := unpackDeb(t, debFile)

	// 1. Verify control metadata
	ctrlData := string(unpacked.ControlFiles["control"])
	if !strings.Contains(ctrlData, "Package: full-app") {
		t.Errorf("control missing Package: full-app")
	}
	if !strings.Contains(ctrlData, "Version: 2.1.0") {
		t.Errorf("control missing normalized Version: 2.1.0 (got %s)", ctrlData)
	}
	if !strings.Contains(ctrlData, "Depends: libc6 (>= 2.31), ca-certificates") {
		t.Errorf("control missing Depends: %s", ctrlData)
	}
	if !strings.Contains(ctrlData, "Section: utils") {
		t.Errorf("control missing Section: utils")
	}
	if !strings.Contains(ctrlData, "Priority: optional") {
		t.Errorf("control missing Priority: optional")
	}

	// 2. Verify conffiles in control.tar.gz
	conffilesData, ok := unpacked.ControlFiles["conffiles"]
	if !ok {
		t.Fatalf("DEBIAN/conffiles missing in control.tar.gz")
	}
	if !strings.Contains(string(conffilesData), "/etc/full-app/full-app.conf") {
		t.Errorf("conffiles missing /etc/full-app/full-app.conf:\n%s", string(conffilesData))
	}

	// 3. Verify maintainer script hooks with set -e
	hooks := []string{"preinst", "postinst", "prerm", "postrm"}
	for _, hook := range hooks {
		hookData, ok := unpacked.ControlFiles[hook]
		if !ok {
			t.Errorf("DEBIAN/%s missing in control.tar.gz", hook)
			continue
		}
		hookStr := string(hookData)
		if !strings.HasPrefix(hookStr, "#!/bin/sh") {
			t.Errorf("hook %s missing #!/bin/sh: %s", hook, hookStr)
		}
		if !strings.Contains(hookStr, "set -e") {
			t.Errorf("hook %s missing set -e: %s", hook, hookStr)
		}
		if !strings.Contains(hookStr, hook+" running") {
			t.Errorf("hook %s missing expected body: %s", hook, hookStr)
		}
	}

	// 4. Verify md5sums in control.tar.gz
	md5Data, ok := unpacked.ControlFiles["md5sums"]
	if !ok {
		t.Fatalf("DEBIAN/md5sums missing in control.tar.gz")
	}
	md5Lines := strings.Split(strings.TrimSpace(string(md5Data)), "\n")

	// Verify alphabetical ordering of md5sums by path
	var pathsInMd5 []string
	for _, line := range md5Lines {
		parts := strings.Fields(line)
		if len(parts) == 2 {
			pathsInMd5 = append(pathsInMd5, parts[1])
		}
	}
	if !sort.StringsAreSorted(pathsInMd5) {
		t.Errorf("md5sums entries not alphabetically sorted by path: %v", pathsInMd5)
	}

	// Verify md5 hash accuracy
	for _, line := range md5Lines {
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}
		expectedHash := parts[0]
		relPath := "/" + parts[1]

		fileData, exists := unpacked.DataFiles[relPath]
		if !exists {
			t.Errorf("md5sums references path %s which does not exist in data.tar.gz", relPath)
			continue
		}
		actualSum := md5.Sum(fileData)
		actualHashStr := hex.EncodeToString(actualSum[:])
		if actualHashStr != expectedHash {
			t.Errorf("md5 mismatch for %s: got %s, want %s", relPath, actualHashStr, expectedHash)
		}
	}

	// 5. Verify data layout: launcher
	launcherData := string(unpacked.DataFiles["/usr/bin/full-app"])
	if !strings.Contains(launcherData, `REAL_PAYLOAD="/usr/lib/full-app/bin/engine"`) {
		t.Errorf("launcher target payload incorrect: %s", launcherData)
	}

	// 6. Verify documentation: compressed man page
	manGzData, ok := unpacked.DataFiles["/usr/share/man/man1/full-app.1.gz"]
	if !ok {
		t.Fatalf("/usr/share/man/man1/full-app.1.gz missing in data.tar.gz")
	}
	gzMan, err := gzip.NewReader(bytes.NewReader(manGzData))
	if err != nil {
		t.Fatalf("failed reading gzipped manpage: %v", err)
	}
	manRoff, err := io.ReadAll(gzMan)
	_ = gzMan.Close()
	if err != nil {
		t.Fatalf("failed decompressing man page: %v", err)
	}
	if !strings.Contains(string(manRoff), "FULL-APP") && !strings.Contains(string(manRoff), "full-app") {
		t.Errorf("man page missing title: %s", string(manRoff))
	}

	// 7. Verify config template deployed to /etc/full-app/full-app.conf
	confData, ok := unpacked.DataFiles["/etc/full-app/full-app.conf"]
	if !ok {
		t.Fatalf("/etc/full-app/full-app.conf missing in data.tar.gz")
	}
	if !strings.Contains(string(confData), "server_port = 8080") {
		t.Errorf("config template content unexpected: %s", string(confData))
	}
}

func TestIntegration_Build_RejectsTraversal(t *testing.T) {
	bin := getCraftpackBinary(t)
	rootDir, _ := filepath.Abs("../..")
	specPath := filepath.Join(rootDir, "test/fixtures/invalid-traversal/craftpack.yml")
	outDir := t.TempDir()

	cmd := exec.Command(bin,
		"build",
		"--spec", specPath,
		"--target", "deb",
		"--package-version", "1.0.0",
		"--output-dir", outDir,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		t.Fatalf("expected build to fail for traversal exploit, but succeeded")
	}

	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("unexpected error type: %v", err)
	}
	if exitErr.ExitCode() != 1 {
		t.Errorf("exit code = %d, want 1 (ExitValidation)", exitErr.ExitCode())
	}

	errLower := strings.ToLower(stderr.String())
	if !strings.Contains(errLower, "traverse") &&
		!strings.Contains(errLower, "boundary") &&
		!strings.Contains(errLower, "validation") {
		t.Errorf("stderr missing traversal security error message:\n%s", stderr.String())
	}

	// Ensure no archives created
	entries, _ := os.ReadDir(outDir)
	if len(entries) > 0 {
		t.Errorf("no files should be generated on traversal exploit, found %d", len(entries))
	}
}

func TestIntegration_Build_RejectsInvalidSchema(t *testing.T) {
	bin := getCraftpackBinary(t)
	rootDir, _ := filepath.Abs("../..")
	specPath := filepath.Join(rootDir, "test/fixtures/invalid-schema/craftpack.yml")
	outDir := t.TempDir()

	cmd := exec.Command(bin,
		"build",
		"--spec", specPath,
		"--target", "deb",
		"--package-version", "1.0.0",
		"--output-dir", outDir,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		t.Fatalf("expected build to fail for invalid schema, but succeeded")
	}

	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("unexpected error type: %v", err)
	}
	if exitErr.ExitCode() != 1 {
		t.Errorf("exit code = %d, want 1 (ExitValidation)", exitErr.ExitCode())
	}

	if !strings.Contains(stderr.String(), "validation") &&
		!strings.Contains(stderr.String(), "maintainer") &&
		!strings.Contains(stderr.String(), "name") {
		t.Errorf("stderr missing validation details:\n%s", stderr.String())
	}
}

func TestIntegration_Build_DryRun(t *testing.T) {
	bin := getCraftpackBinary(t)
	rootDir, _ := filepath.Abs("../..")
	specPath := filepath.Join(rootDir, "test/fixtures/valid-minimal/craftpack.yml")
	outDir := t.TempDir()

	cmd := exec.Command(bin,
		"build",
		"--spec", specPath,
		"--target", "deb",
		"--package-version", "1.0.0",
		"--output-dir", outDir,
		"--dry-run",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		t.Fatalf("dry-run build failed: %v\nSTDERR:\n%s", err, stderr.String())
	}

	// 1. STDOUT must be 0 bytes (strict stream separation)
	if stdout.Len() != 0 {
		t.Errorf("STDOUT must be clean (0 bytes) during dry-run, got: %s", stdout.String())
	}

	// 2. STDERR must indicate dry-run simulation
	if !strings.Contains(stderr.String(), "Dry-run build simulation completed successfully") {
		t.Errorf("STDERR missing dry-run completion log: %s", stderr.String())
	}

	// 3. No files must be created in output directory
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("failed reading outDir: %v", err)
	}
	if len(entries) > 0 {
		t.Errorf("expected 0 files in output dir for dry-run, found %d", len(entries))
	}
}

func TestIntegration_Build_JSONOutput(t *testing.T) {
	bin := getCraftpackBinary(t)
	rootDir, _ := filepath.Abs("../..")
	specPath := filepath.Join(rootDir, "test/fixtures/valid-minimal/craftpack.yml")
	outDir := t.TempDir()

	cmd := exec.Command(bin,
		"build",
		"--spec", specPath,
		"--target", "deb",
		"--package-version", "1.0.0",
		"--output-dir", outDir,
		"--json",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		t.Fatalf("build --json failed: %v\nSTDERR:\n%s", err, stderr.String())
	}

	var res struct {
		Success      bool   `json:"success"`
		PackageFile  string `json:"package_file"`
		PackageName  string `json:"package_name"`
		Filename     string `json:"filename"`
		Version      string `json:"version"`
		Architecture string `json:"architecture"`
		Target       string `json:"target"`
		SHA256       string `json:"sha256"`
		SizeBytes    int64  `json:"size_bytes"`
		DryRun       bool   `json:"dry_run"`
	}

	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("failed unmarshaling STDOUT JSON: %v (raw: %s)", err, stdout.String())
	}

	if !res.Success || res.PackageName != "minimal-app" || res.DryRun {
		t.Errorf("unexpected build result: %+v", res)
	}
	if res.SHA256 == "" || res.SizeBytes <= 0 {
		t.Errorf("manifest checksum or size missing: %+v", res)
	}
	if _, err := os.Stat(res.PackageFile); err != nil {
		t.Errorf("package file referenced in JSON does not exist: %v", err)
	}
}

func TestIntegration_Build_DryRun_JSONOutput(t *testing.T) {
	bin := getCraftpackBinary(t)
	rootDir, _ := filepath.Abs("../..")
	specPath := filepath.Join(rootDir, "test/fixtures/valid-minimal/craftpack.yml")
	outDir := t.TempDir()

	cmd := exec.Command(bin,
		"build",
		"--spec", specPath,
		"--target", "deb",
		"--package-version", "1.0.0",
		"--output-dir", outDir,
		"--dry-run",
		"--json",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		t.Fatalf("build --dry-run --json failed: %v\nSTDERR:\n%s", err, stderr.String())
	}

	var res struct {
		Success      bool   `json:"success"`
		PackageFile  string `json:"package_file"`
		PackageName  string `json:"package_name"`
		Filename     string `json:"filename"`
		DryRun       bool   `json:"dry_run"`
	}

	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("failed unmarshaling STDOUT JSON: %v (raw: %s)", err, stdout.String())
	}

	if !res.Success || !res.DryRun || res.PackageFile != "" {
		t.Errorf("unexpected dry-run json result: %+v", res)
	}

	entries, _ := os.ReadDir(outDir)
	if len(entries) > 0 {
		t.Errorf("expected 0 files in outDir, found %d", len(entries))
	}
}

func TestIntegration_Build_SymlinkEscape(t *testing.T) {
	bin := getCraftpackBinary(t)
	dir := t.TempDir()

	appDir := filepath.Join(dir, "app")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("failed creating app dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "run.sh"), []byte("#!/bin/sh\necho hi\n"), 0755); err != nil {
		t.Fatalf("failed creating entrypoint: %v", err)
	}

	// Create a secret file outside the workspace
	outsideDir := t.TempDir()
	secretFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(secretFile, []byte("super-secret"), 0600); err != nil {
		t.Fatalf("failed creating secret file: %v", err)
	}

	// Symlink from inside app/ pointing outside workspace
	symlinkPath := filepath.Join(appDir, "evil_link")
	if err := os.Symlink(secretFile, symlinkPath); err != nil {
		t.Fatalf("failed creating symlink: %v", err)
	}

	specContent := `name: escape-app
description: App attempting symlink traversal
maintainer: Hacker <hacker@example.com>
homepage: https://example.com/escape
license: Apache-2.0
command: escape-app
payload_dir: app
entrypoint: run.sh
targets:
  deb:
    section: utils
`
	specFile := filepath.Join(dir, "craftpack.yml")
	if err := os.WriteFile(specFile, []byte(specContent), 0644); err != nil {
		t.Fatalf("failed writing spec: %v", err)
	}

	outDir := t.TempDir()
	cmd := exec.Command(bin, "build", "--spec", specFile, "--target", "deb", "--package-version", "1.0.0", "--output-dir", outDir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		t.Fatalf("expected build to fail on symlink traversal, but succeeded")
	}

	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 1 {
		t.Errorf("exit code = %v, want 1 (ExitValidation)", err)
	}

	errLower := strings.ToLower(stderr.String())
	if !strings.Contains(errLower, "escape") && !strings.Contains(errLower, "travers") && !strings.Contains(errLower, "symlink") {
		t.Errorf("stderr missing symlink escape message: %s", stderr.String())
	}
}

func TestIntegration_Build_ManPage_ZeroMarkupViolation(t *testing.T) {
	bin := getCraftpackBinary(t)
	dir := t.TempDir()

	appDir := filepath.Join(dir, "app")
	docsDir := filepath.Join(dir, "docs")
	_ = os.MkdirAll(appDir, 0755)
	_ = os.MkdirAll(docsDir, 0755)
	_ = os.WriteFile(filepath.Join(appDir, "run.sh"), []byte("#!/bin/sh\n"), 0755)

	badManPage := `---
title: Bad Page
section: 1
---
# Name
bad-app - An app with front-matter
`
	_ = os.WriteFile(filepath.Join(docsDir, "bad.1.md"), []byte(badManPage), 0644)

	specContent := `name: bad-manpage-app
description: App violating Zero-Markup policy
maintainer: Doc Writer <doc@example.com>
homepage: https://example.com/badman
license: Apache-2.0
command: bad-app
payload_dir: app
entrypoint: run.sh
man_pages:
  - source: docs/bad.1.md
    section: 1
targets:
  deb:
    section: utils
`
	specFile := filepath.Join(dir, "craftpack.yml")
	_ = os.WriteFile(specFile, []byte(specContent), 0644)

	outDir := t.TempDir()
	cmd := exec.Command(bin, "build", "--spec", specFile, "--target", "deb", "--package-version", "1.0.0", "--output-dir", outDir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		t.Fatalf("expected build to fail on Zero-Markup front-matter violation")
	}

	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 1 {
		t.Errorf("exit code = %v, want 1 (ExitValidation)", err)
	}

	if !strings.Contains(stderr.String(), "Zero-Markup") && !strings.Contains(stderr.String(), "front-matter") {
		t.Errorf("stderr missing Zero-Markup violation details: %s", stderr.String())
	}
}

func TestIntegration_Build_MissingPrerequisites(t *testing.T) {
	bin := getCraftpackBinary(t)

	cases := []struct {
		name        string
		setup       func(dir string) string
		errContains string
	}{
		{
			name: "nonexistent spec file",
			setup: func(dir string) string {
				return filepath.Join(dir, "nonexistent.yml")
			},
			errContains: "not found",
		},
		{
			name: "missing entrypoint in payload",
			setup: func(dir string) string {
				appDir := filepath.Join(dir, "app")
				_ = os.MkdirAll(appDir, 0755)
				spec := `name: missing-entrypoint
description: Testing missing entrypoint
maintainer: Tester <test@example.com>
homepage: https://example.com/test
license: MIT
command: testcmd
payload_dir: app
entrypoint: missing_bin
targets:
  deb:
    section: utils
`
				specFile := filepath.Join(dir, "craftpack.yml")
				_ = os.WriteFile(specFile, []byte(spec), 0644)
				return specFile
			},
			errContains: "entrypoint",
		},
		{
			name: "missing default_config source",
			setup: func(dir string) string {
				appDir := filepath.Join(dir, "app")
				_ = os.MkdirAll(appDir, 0755)
				_ = os.WriteFile(filepath.Join(appDir, "run.sh"), []byte("#!/bin/sh\n"), 0755)
				spec := `name: missing-config
description: Testing missing config
maintainer: Tester <test@example.com>
homepage: https://example.com/test
license: MIT
command: testcmd
payload_dir: app
entrypoint: run.sh
default_config:
  missing.conf: testcmd.conf
targets:
  deb:
    section: utils
`
				specFile := filepath.Join(dir, "craftpack.yml")
				_ = os.WriteFile(specFile, []byte(spec), 0644)
				return specFile
			},
			errContains: "default_config",
		},
		{
			name: "hook script attempting path traversal",
			setup: func(dir string) string {
				appDir := filepath.Join(dir, "app")
				_ = os.MkdirAll(appDir, 0755)
				_ = os.WriteFile(filepath.Join(appDir, "run.sh"), []byte("#!/bin/sh\n"), 0755)
				spec := `name: traversal-hook
description: Testing traversal hook
maintainer: Tester <test@example.com>
homepage: https://example.com/test
license: MIT
command: testcmd
payload_dir: app
entrypoint: run.sh
preinstall: ../../evil.sh
targets:
  deb:
    section: utils
`
				specFile := filepath.Join(dir, "craftpack.yml")
				_ = os.WriteFile(specFile, []byte(spec), 0644)
				return specFile
			},
			errContains: "traverse",
		},
		{
			name: "missing man page source",
			setup: func(dir string) string {
				appDir := filepath.Join(dir, "app")
				_ = os.MkdirAll(appDir, 0755)
				_ = os.WriteFile(filepath.Join(appDir, "run.sh"), []byte("#!/bin/sh\n"), 0755)
				spec := `name: missing-man
description: Testing missing man page
maintainer: Tester <test@example.com>
homepage: https://example.com/test
license: MIT
command: testcmd
payload_dir: app
entrypoint: run.sh
man_pages:
  - source: docs/nonexistent.1.md
    section: 1
targets:
  deb:
    section: utils
`
				specFile := filepath.Join(dir, "craftpack.yml")
				_ = os.WriteFile(specFile, []byte(spec), 0644)
				return specFile
			},
			errContains: "does not exist or is not readable",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			specFile := tc.setup(dir)
			outDir := t.TempDir()

			cmd := exec.Command(bin, "build", "--spec", specFile, "--target", "deb", "--package-version", "1.0.0", "--output-dir", outDir)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			err := cmd.Run()
			if err == nil {
				t.Fatalf("expected build to fail for %s", tc.name)
			}
			exitErr, ok := err.(*exec.ExitError)
			if !ok || exitErr.ExitCode() != 1 {
				t.Errorf("exit code = %v, want 1 (ExitValidation)", err)
			}
			if !strings.Contains(strings.ToLower(stderr.String()), strings.ToLower(tc.errContains)) {
				t.Errorf("stderr missing %q: %s", tc.errContains, stderr.String())
			}
		})
	}
}

func TestIntegration_Build_ArchitectureMatrix(t *testing.T) {
	bin := getCraftpackBinary(t)
	rootDir, _ := filepath.Abs("../..")
	specPath := filepath.Join(rootDir, "test/fixtures/valid-minimal/craftpack.yml")

	archCases := []struct {
		cliArch  string
		wantDeb  string
		wantArch string
	}{
		{cliArch: "arm64", wantDeb: "minimal-app_1.0.0_arm64.deb", wantArch: "arm64"},
		{cliArch: "all", wantDeb: "minimal-app_1.0.0_all.deb", wantArch: "all"},
		{cliArch: "aarch64", wantDeb: "minimal-app_1.0.0_arm64.deb", wantArch: "arm64"},
		{cliArch: "x86_64", wantDeb: "minimal-app_1.0.0_amd64.deb", wantArch: "amd64"},
	}

	for _, tc := range archCases {
		t.Run(tc.cliArch, func(t *testing.T) {
			outDir := t.TempDir()
			cmd := exec.Command(bin,
				"build",
				"--spec", specPath,
				"--target", "deb",
				"--package-version", "1.0.0",
				"--arch", tc.cliArch,
				"--output-dir", outDir,
			)

			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			if err := cmd.Run(); err != nil {
				t.Fatalf("build failed for arch %s: %v\nSTDERR:\n%s", tc.cliArch, err, stderr.String())
			}

			debPath := filepath.Join(outDir, tc.wantDeb)
			if _, err := os.Stat(debPath); err != nil {
				t.Fatalf("expected package %s was not generated: %v", tc.wantDeb, err)
			}

			unpacked := unpackDeb(t, debPath)
			ctrlData := string(unpacked.ControlFiles["control"])
			expectedLine := fmt.Sprintf("Architecture: %s", tc.wantArch)
			if !strings.Contains(ctrlData, expectedLine) {
				t.Errorf("control missing %q:\n%s", expectedLine, ctrlData)
			}
		})
	}
}

func TestIntegration_Build_ZeroByteFileAndDeepNesting(t *testing.T) {
	bin := getCraftpackBinary(t)
	dir := t.TempDir()

	payloadDir := filepath.Join(dir, "payload")
	nestedDir := filepath.Join(payloadDir, "sub1", "sub2", "sub3")
	if err := os.MkdirAll(nestedDir, 0755); err != nil {
		t.Fatalf("failed creating nested dir: %v", err)
	}

	// 1. Regular entrypoint
	_ = os.WriteFile(filepath.Join(payloadDir, "app.sh"), []byte("#!/bin/sh\necho running\n"), 0755)

	// 2. Empty 0-byte file
	emptyFile := filepath.Join(payloadDir, "empty.txt")
	if err := os.WriteFile(emptyFile, []byte{}, 0644); err != nil {
		t.Fatalf("failed writing empty file: %v", err)
	}

	// 3. Deeply nested file
	nestedFile := filepath.Join(nestedDir, "deep.conf")
	if err := os.WriteFile(nestedFile, []byte("nested_key = true\n"), 0644); err != nil {
		t.Fatalf("failed writing nested file: %v", err)
	}

	spec := `name: edge-app
description: Application with empty files and deep directory structures
maintainer: Developer <dev@example.com>
homepage: https://example.com/edge
license: Apache-2.0
command: edge-app
payload_dir: payload
entrypoint: app.sh
targets:
  deb:
    section: utils
`
	specPath := filepath.Join(dir, "craftpack.yml")
	_ = os.WriteFile(specPath, []byte(spec), 0644)

	outDir := t.TempDir()
	cmd := exec.Command(bin, "build", "--spec", specPath, "--target", "deb", "--package-version", "1.0.0", "--output-dir", outDir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("build failed: %v\nSTDERR:\n%s", err, stderr.String())
	}

	debFile := filepath.Join(outDir, "edge-app_1.0.0_amd64.deb")
	entries, _ := os.ReadDir(outDir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".deb") {
			debFile = filepath.Join(outDir, e.Name())
			break
		}
	}

	unpacked := unpackDeb(t, debFile)

	// Verify empty.txt exists and is 0 bytes
	emptyData, ok := unpacked.DataFiles["/usr/lib/edge-app/empty.txt"]
	if !ok {
		t.Fatalf("/usr/lib/edge-app/empty.txt not found in data.tar.gz")
	}
	if len(emptyData) != 0 {
		t.Errorf("empty.txt length = %d, want 0", len(emptyData))
	}

	// Verify empty file MD5 in DEBIAN/md5sums matches standard empty hash d41d8cd98f00b204e9800998ecf8427e
	md5Content := string(unpacked.ControlFiles["md5sums"])
	if !strings.Contains(md5Content, "d41d8cd98f00b204e9800998ecf8427e  usr/lib/edge-app/empty.txt") {
		t.Errorf("md5sums missing entry for 0-byte file:\n%s", md5Content)
	}

	// Verify deeply nested file exists
	nestedData, ok := unpacked.DataFiles["/usr/lib/edge-app/sub1/sub2/sub3/deep.conf"]
	if !ok {
		t.Fatalf("nested file not found in data.tar.gz")
	}
	if string(nestedData) != "nested_key = true\n" {
		t.Errorf("unexpected nested file content: %s", string(nestedData))
	}
}

func TestIntegration_Build_MultipleConffiles(t *testing.T) {
	bin := getCraftpackBinary(t)
	dir := t.TempDir()

	payloadDir := filepath.Join(dir, "bin")
	configDir := filepath.Join(dir, "configs")
	_ = os.MkdirAll(payloadDir, 0755)
	_ = os.MkdirAll(configDir, 0755)

	_ = os.WriteFile(filepath.Join(payloadDir, "daemon"), []byte("#!/bin/sh\n"), 0755)
	_ = os.WriteFile(filepath.Join(configDir, "main.conf"), []byte("main = 1\n"), 0644)
	_ = os.WriteFile(filepath.Join(configDir, "extra.conf"), []byte("extra = 2\n"), 0644)

	spec := `name: multiconf-app
description: App with multiple config files
maintainer: Admin <admin@example.com>
homepage: https://example.com/multiconf
license: BSD-3-Clause
command: daemon
payload_dir: bin
entrypoint: daemon
default_config:
  configs/main.conf: daemon.conf
  configs/extra.conf: extra.conf
targets:
  deb:
    section: utils
`
	specPath := filepath.Join(dir, "craftpack.yml")
	_ = os.WriteFile(specPath, []byte(spec), 0644)

	outDir := t.TempDir()
	cmd := exec.Command(bin, "build", "--spec", specPath, "--target", "deb", "--package-version", "1.0.0", "--output-dir", outDir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("build failed: %v\nSTDERR:\n%s", err, stderr.String())
	}

	entries, _ := os.ReadDir(outDir)
	var debFile string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".deb") {
			debFile = filepath.Join(outDir, e.Name())
			break
		}
	}

	unpacked := unpackDeb(t, debFile)

	conffilesData, ok := unpacked.ControlFiles["conffiles"]
	if !ok {
		t.Fatalf("DEBIAN/conffiles missing")
	}
	conffilesStr := string(conffilesData)

	// Must have leading slashes
	if !strings.Contains(conffilesStr, "/etc/multiconf-app/daemon.conf") {
		t.Errorf("conffiles missing /etc/multiconf-app/daemon.conf:\n%s", conffilesStr)
	}
	if !strings.Contains(conffilesStr, "/etc/multiconf-app/extra.conf") {
		t.Errorf("conffiles missing /etc/multiconf-app/extra.conf:\n%s", conffilesStr)
	}

	// Must be present in data.tar.gz
	if _, ok := unpacked.DataFiles["/etc/multiconf-app/daemon.conf"]; !ok {
		t.Errorf("daemon.conf missing in data.tar.gz")
	}
	if _, ok := unpacked.DataFiles["/etc/multiconf-app/extra.conf"]; !ok {
		t.Errorf("extra.conf missing in data.tar.gz")
	}
}

func TestIntegration_Build_NestedOutputDirAutoCreation(t *testing.T) {
	bin := getCraftpackBinary(t)
	rootDir, _ := filepath.Abs("../..")
	specPath := filepath.Join(rootDir, "test/fixtures/valid-minimal/craftpack.yml")

	dir := t.TempDir()
	deepOutDir := filepath.Join(dir, "dist", "releases", "2026", "linux")

	cmd := exec.Command(bin,
		"build",
		"--spec", specPath,
		"--target", "deb",
		"--package-version", "1.0.0",
		"--output-dir", deepOutDir,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("build failed: %v\nSTDERR:\n%s", err, stderr.String())
	}

	// Verify deepOutDir created and contains .deb and checksums.sha256
	manifestPath := filepath.Join(deepOutDir, "checksums.sha256")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Errorf("checksums.sha256 not found in deep outDir: %v", err)
	}
}

func TestIntegration_Build_ConcurrentExecutions(t *testing.T) {
	bin := getCraftpackBinary(t)
	rootDir, _ := filepath.Abs("../..")
	specPath := filepath.Join(rootDir, "test/fixtures/valid-minimal/craftpack.yml")

	concurrency := 4
	var wg sync.WaitGroup
	errCh := make(chan error, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			outDir := t.TempDir()
			cmd := exec.Command(bin,
				"build",
				"--spec", specPath,
				"--target", "deb",
				"--package-version", fmt.Sprintf("1.0.%d", idx),
				"--output-dir", outDir,
			)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			if err := cmd.Run(); err != nil {
				errCh <- fmt.Errorf("worker %d failed: %w (stderr: %s)", idx, err, stderr.String())
				return
			}

			manifest := filepath.Join(outDir, "checksums.sha256")
			if _, err := os.Stat(manifest); err != nil {
				errCh <- fmt.Errorf("worker %d manifest missing: %w", idx, err)
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent execution error: %v", err)
	}
}

