// SPDX-FileCopyrightText: 2026 Marcin Kaim
// SPDX-License-Identifier: Apache-2.0

package deb

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"craftpack/pkg/fsutil"
	"craftpack/pkg/spec"
	"craftpack/pkg/target"
	"github.com/blakesmith/ar"
)

func TestGenerateControl_Formatting(t *testing.T) {
	data := ControlData{
		Package:      "craftpack",
		Version:      "1.0.0",
		Architecture: "amd64",
		Maintainer:   "Marcin Kaim <marcin@example.com>",
		Description:  "Standardized Linux packaging factory\n Craftpack builds deterministic deb packages.\n Additional details line.",
		Homepage:     "https://github.com/craftpack/craftpack",
		License:      "Apache-2.0",
		Section:      "utils",
		Priority:     "optional",
		Depends:      "libc6 (>= 2.31), ca-certificates",
	}

	content, err := GenerateControl(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := string(content)

	expectedFields := []string{
		"Package: craftpack\n",
		"Version: 1.0.0\n",
		"Architecture: amd64\n",
		"Maintainer: Marcin Kaim <marcin@example.com>\n",
		"Description: Standardized Linux packaging factory\n Craftpack builds deterministic deb packages.\n Additional details line.\n",
		"Homepage: https://github.com/craftpack/craftpack\n",
		"License: Apache-2.0\n",
		"Section: utils\n",
		"Priority: optional\n",
		"Depends: libc6 (>= 2.31), ca-certificates\n",
	}

	for _, expected := range expectedFields {
		if !strings.Contains(text, expected) {
			t.Errorf("control file missing expected field:\n%s\nGot:\n%s", expected, text)
		}
	}

	if !strings.HasSuffix(text, "\n") {
		t.Errorf("expected control file to end with newline")
	}
}

func TestGenerateControl_DefaultsAndOmissions(t *testing.T) {
	data := ControlData{
		Package:      "miniapp",
		Version:      "0.1.0",
		Architecture: "arm64",
		Maintainer:   "Dev <dev@test.org>",
		Description:  "Minimal synopsis without body",
	}

	content, err := GenerateControl(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := string(content)

	if !strings.Contains(text, "Section: utils\n") {
		t.Errorf("expected default Section 'utils', got:\n%s", text)
	}
	if !strings.Contains(text, "Priority: optional\n") {
		t.Errorf("expected default Priority 'optional', got:\n%s", text)
	}
	if strings.Contains(text, "Depends:") {
		t.Errorf("empty Depends should be omitted from control")
	}
	if strings.Contains(text, "Homepage:") {
		t.Errorf("empty Homepage should be omitted from control")
	}
	if strings.Contains(text, "License:") {
		t.Errorf("empty License should be omitted from control")
	}
}

func TestGenerateControl_ValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		data ControlData
	}{
		{"missing package", ControlData{Version: "1.0", Architecture: "amd64", Maintainer: "m@e.com"}},
		{"missing version", ControlData{Package: "pkg", Architecture: "amd64", Maintainer: "m@e.com"}},
		{"missing architecture", ControlData{Package: "pkg", Version: "1.0", Maintainer: "m@e.com"}},
		{"missing maintainer", ControlData{Package: "pkg", Version: "1.0", Architecture: "amd64"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := GenerateControl(tt.data)
			if err == nil {
				t.Errorf("expected error for %s, got nil", tt.name)
			}
		})
	}
}

func TestGenerateControlFromConfig(t *testing.T) {
	cfg := &spec.CraftpackConfig{
		Name:        "myapp",
		Maintainer:  "Maintainer <m@e.com>",
		Description: "A test application",
		Targets: spec.TargetConfigs{
			Deb: &spec.DebianTargetConfig{
				Section:      "devel",
				Priority:     "important",
				Dependencies: []string{"libc6 (>= 2.34)"},
			},
		},
	}

	content, err := GenerateControlFromConfig(cfg, "v2.0.0", "amd64")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := string(content)
	if !strings.Contains(text, "Package: myapp\n") {
		t.Errorf("missing package name")
	}
	if !strings.Contains(text, "Version: 2.0.0\n") {
		t.Errorf("expected normalized version '2.0.0', got:\n%s", text)
	}
	if !strings.Contains(text, "Section: devel\n") {
		t.Errorf("missing Section 'devel'")
	}
	if !strings.Contains(text, "Priority: important\n") {
		t.Errorf("missing Priority 'important'")
	}
	if !strings.Contains(text, "Depends: libc6 (>= 2.34)\n") {
		t.Errorf("missing Depends")
	}

	// Nil config
	_, err = GenerateControlFromConfig(nil, "1.0", "amd64")
	if err == nil {
		t.Errorf("expected error for nil config")
	}
}

func TestGenerateConffiles(t *testing.T) {
	configMap := map[string]string{
		"config/app.default.yml": "app.yml",
		"config/server.conf":     "server.conf",
		"config/auth.json":       "auth.json",
	}

	content, err := GenerateConffiles("myapp", configMap)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "/etc/myapp/app.yml\n/etc/myapp/auth.json\n/etc/myapp/server.conf\n"
	if string(content) != expected {
		t.Errorf("expected sorted conffiles:\n%q\nGot:\n%q", expected, string(content))
	}

	// Empty map returns nil
	emptyContent, err := GenerateConffiles("myapp", nil)
	if err != nil || emptyContent != nil {
		t.Errorf("expected nil for empty map, got %v, err: %v", emptyContent, err)
	}

	// Empty app ID returns error
	_, err = GenerateConffiles("", configMap)
	if err == nil {
		t.Errorf("expected error for empty app ID")
	}

	// Invalid target paths with traversal
	invalidMap := map[string]string{"foo": "../evil.conf"}
	_, err = GenerateConffiles("myapp", invalidMap)
	if err == nil {
		t.Errorf("expected error for traversal path in conffiles")
	}
}

func TestAdaptMaintainerScript(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantPrefix  string
		wantContain string
	}{
		{
			name:        "empty script",
			input:       "",
			wantPrefix:  "#!/bin/sh\nset -e\n",
			wantContain: "set -e",
		},
		{
			name:        "plain shell code without shebang",
			input:       "echo \"configuring system\"\nadduser --system myapp",
			wantPrefix:  "#!/bin/sh\nset -e\necho \"configuring system\"\n",
			wantContain: "adduser --system myapp",
		},
		{
			name:        "script with shebang but no set -e",
			input:       "#!/bin/sh\necho \"starting\"\n",
			wantPrefix:  "#!/bin/sh\nset -e\necho \"starting\"\n",
			wantContain: "set -e",
		},
		{
			name:        "script with bash shebang",
			input:       "#!/bin/bash\necho \"bash\"\n",
			wantPrefix:  "#!/bin/sh\nset -e\necho \"bash\"\n",
			wantContain: "set -e",
		},
		{
			name:        "script already containing set -e",
			input:       "#!/bin/sh\nset -e\necho \"already safe\"\n",
			wantPrefix:  "#!/bin/sh\nset -e\necho \"already safe\"\n",
			wantContain: "already safe",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(AdaptMaintainerScript(tt.input))
			if !strings.HasPrefix(got, tt.wantPrefix) {
				t.Errorf("expected prefix:\n%q\nGot:\n%q", tt.wantPrefix, got)
			}
			if !strings.Contains(got, tt.wantContain) {
				t.Errorf("expected to contain %q, got:\n%q", tt.wantContain, got)
			}
			if !strings.HasSuffix(got, "\n") {
				t.Errorf("expected trailing newline")
			}
			// Verify set -e is not duplicated
			count := strings.Count(got, "set -e")
			if count != 1 {
				t.Errorf("expected exactly 1 'set -e', found %d in:\n%s", count, got)
			}
		})
	}
}

func TestGenerateMaintainerScripts(t *testing.T) {
	tempWorkspace := t.TempDir()

	// Create script file in workspace
	postinstFile := filepath.Join(tempWorkspace, "scripts", "postinst.sh")
	_ = os.MkdirAll(filepath.Dir(postinstFile), 0755)
	_ = os.WriteFile(postinstFile, []byte("echo 'running postinst from file'"), 0755)

	cfg := &spec.CraftpackConfig{
		PreInstall:  "echo 'pre-install inline'",
		PostInstall: "scripts/postinst.sh",
		PreRemove:   "echo 'prerm inline'",
		PostRemove:  "echo 'postrm inline'",
	}

	scripts, err := GenerateMaintainerScripts(cfg, tempWorkspace)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(scripts) != 4 {
		t.Fatalf("expected 4 maintainer scripts, got %d", len(scripts))
	}

	expectedMap := map[string]string{
		"preinst":  "pre-install inline",
		"postinst": "running postinst from file",
		"prerm":    "prerm inline",
		"postrm":   "postrm inline",
	}

	for _, s := range scripts {
		if s.Mode != fsutil.ExecMode {
			t.Errorf("script '%s' mode = %v, want 0755", s.Name, s.Mode)
		}
		expectedContent, ok := expectedMap[s.Name]
		if !ok {
			t.Errorf("unexpected script name '%s'", s.Name)
			continue
		}
		contentStr := string(s.Content)
		if !strings.Contains(contentStr, expectedContent) {
			t.Errorf("script '%s' does not contain '%s', got:\n%s", s.Name, expectedContent, contentStr)
		}
		if !strings.HasPrefix(contentStr, "#!/bin/sh\nset -e\n") {
			t.Errorf("script '%s' missing standard shebang and set -e header:\n%s", s.Name, contentStr)
		}
	}
}

func TestGenerateMD5Sums(t *testing.T) {
	data1 := []byte("first content")
	data2 := []byte("second content")

	h1 := fmt.Sprintf("%x", md5.Sum(data1))
	h2 := fmt.Sprintf("%x", md5.Sum(data2))

	entries := []fsutil.TarEntry{
		{Path: "usr/bin/tool", Data: data1},
		{Path: "etc/myapp/app.conf", Data: data2},
		{Path: "usr/lib/myapp/", IsDir: true}, // should be ignored
	}

	content, err := GenerateMD5SumsFromEntries(entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := fmt.Sprintf("%s  etc/myapp/app.conf\n%s  usr/bin/tool\n", h2, h1)
	if string(content) != expected {
		t.Errorf("expected sorted md5sums:\n%q\nGot:\n%q", expected, string(content))
	}

	// Test from directory
	tempDir := t.TempDir()
	f1Path := filepath.Join(tempDir, "usr", "bin", "tool")
	f2Path := filepath.Join(tempDir, "etc", "myapp", "app.conf")
	_ = os.MkdirAll(filepath.Dir(f1Path), 0755)
	_ = os.MkdirAll(filepath.Dir(f2Path), 0755)
	_ = os.WriteFile(f1Path, data1, 0755)
	_ = os.WriteFile(f2Path, data2, 0644)

	dirContent, err := GenerateMD5SumsFromDir(tempDir)
	if err != nil {
		t.Fatalf("unexpected error from dir: %v", err)
	}

	if string(dirContent) != expected {
		t.Errorf("expected dir md5sums:\n%q\nGot:\n%q", expected, string(dirContent))
	}
}

func TestAssembleDebAndReadDeb(t *testing.T) {
	controlData := []byte("fake control.tar.gz bytes")
	dataData := []byte("fake data.tar.gz bytes")
	modTime := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

	var buf bytes.Buffer
	err := AssembleDeb(&buf, controlData, dataData, modTime)
	if err != nil {
		t.Fatalf("unexpected error assembling deb: %v", err)
	}

	// Verify pure-Go ar reader can parse the deb
	deb, err := ReadDeb(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("unexpected error reading deb: %v", err)
	}

	if string(deb.DebianBinary) != "2.0\n" {
		t.Errorf("expected debian-binary '2.0\\n', got '%s'", string(deb.DebianBinary))
	}
	if !bytes.Equal(deb.ControlTarGz, controlData) {
		t.Errorf("control.tar.gz bytes mismatch")
	}
	if !bytes.Equal(deb.DataTarGz, dataData) {
		t.Errorf("data.tar.gz bytes mismatch")
	}

	// Error cases
	if err := AssembleDeb(nil, controlData, dataData, modTime); err == nil {
		t.Errorf("expected error for nil writer")
	}
	if err := AssembleDeb(&buf, nil, dataData, modTime); err == nil {
		t.Errorf("expected error for empty control")
	}
	if err := AssembleDeb(&buf, controlData, nil, modTime); err == nil {
		t.Errorf("expected error for empty data")
	}
}

func TestNormalizeArchitecture(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"x86_64", "amd64"},
		{"AMD64", "amd64"},
		{"aarch64", "arm64"},
		{"ARM64", "arm64"},
		{"i386", "i386"},
		{"386", "i386"},
		{"arm", "armhf"},
		{"all", "all"},
		{"custom_arch", "custom_arch"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeArchitecture(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeArchitecture(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}

	// Empty defaults to host architecture
	hostArch := NormalizeArchitecture("")
	if hostArch == "" {
		t.Errorf("expected non-empty host architecture")
	}
}

func TestNormalizeVersion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"v1.2.3", "1.2.3"},
		{"V2.0.0", "2.0.0"},
		{" 1.0.0 ", "1.0.0"},
		{"v1.0.0-rc.1", "1.0.0-rc.1"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeVersion(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeVersion(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestPackager_Build_EndToEnd executes a full mock packaging pipeline and validates
// all requirements: debian-binary, control.tar.gz (control, conffiles, md5sums, hooks),
// and data.tar.gz (usr/lib/<app_id>/, usr/bin/<command>, usr/share/man/, etc/<app_id>/).
func TestPackager_Build_EndToEnd(t *testing.T) {
	workspace := t.TempDir()
	outDir := t.TempDir()

	// 1. Setup payload directory with executable binary and asset file
	payloadDir := filepath.Join(workspace, "build", "bin")
	_ = os.MkdirAll(payloadDir, 0755)
	binaryFile := filepath.Join(payloadDir, "craftpack")
	_ = os.WriteFile(binaryFile, []byte("#!/bin/sh\necho 'payload binary'\n"), 0755)
	assetFile := filepath.Join(payloadDir, "data.txt")
	_ = os.WriteFile(assetFile, []byte("asset file content"), 0644)

	// 2. Setup documentation
	docsDir := filepath.Join(workspace, "docs")
	_ = os.MkdirAll(docsDir, 0755)
	manFile := filepath.Join(docsDir, "craftpack.1.md")
	manContent := `# NAME
craftpack - packaging factory

# SYNOPSIS
craftpack build [options]
`
	_ = os.WriteFile(manFile, []byte(manContent), 0644)

	// 3. Setup default configuration template
	confDir := filepath.Join(workspace, "config")
	_ = os.MkdirAll(confDir, 0755)
	confFile := filepath.Join(confDir, "craftpack.default.yml")
	_ = os.WriteFile(confFile, []byte("settings:\n  enabled: true\n"), 0644)

	// 4. Setup maintainer hook file
	scriptsDir := filepath.Join(workspace, "scripts")
	_ = os.MkdirAll(scriptsDir, 0755)
	postinstFile := filepath.Join(scriptsDir, "postinst.sh")
	_ = os.WriteFile(postinstFile, []byte("echo 'post-installation configured'\n"), 0755)

	// 5. Assemble CraftpackConfig
	cfg := &spec.CraftpackConfig{
		Name:        "craftpack",
		Description: "Standardized Linux packaging factory",
		Maintainer:  "Marcin Kaim <marcin@example.com>",
		Homepage:    "https://github.com/craftpack/craftpack",
		License:     "Apache-2.0",
		Command:     "craftpack",
		PayloadDir:  "build/bin",
		Entrypoint:  "craftpack",
		PreInstall:  "echo 'preinst running'",
		PostInstall: "scripts/postinst.sh",
		ManPages: []spec.ManPageConfig{
			{
				Source:  "docs/craftpack.1.md",
				Section: 1,
				Title:   "CRAFTPACK",
			},
		},
		DefaultConfig: map[string]string{
			"config/craftpack.default.yml": "craftpack.yml",
		},
		Targets: spec.TargetConfigs{
			Deb: &spec.DebianTargetConfig{
				Section:      "utils",
				Priority:     "optional",
				Dependencies: []string{"libc6 (>= 2.31)"},
			},
		},
	}

	fixedDate := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	packager := NewPackager()

	opts := target.PackageOptions{
		Config:         cfg,
		WorkspaceDir:   workspace,
		OutputDir:      outDir,
		PackageVersion: "v1.0.0",
		Architecture:   "amd64",
		BuildDate:      fixedDate,
	}

	res, err := packager.Build(context.Background(), opts)
	if err != nil {
		t.Fatalf("packager.Build failed: %v", err)
	}

	expectedFilename := "craftpack_1.0.0_amd64.deb"
	if res.Filename != expectedFilename {
		t.Errorf("expected filename '%s', got '%s'", expectedFilename, res.Filename)
	}

	if fi, err := os.Stat(res.PackageFile); err != nil || fi.Size() == 0 {
		t.Fatalf("emitted package file does not exist or is empty: %v", err)
	}

	// 6. Verify .deb internals using native pure-Go reader
	debFile, err := os.Open(res.PackageFile)
	if err != nil {
		t.Fatalf("failed opening package file: %v", err)
	}
	defer debFile.Close()

	deb, err := ReadDeb(debFile)
	if err != nil {
		t.Fatalf("failed parsing .deb container: %v", err)
	}

	if string(deb.DebianBinary) != "2.0\n" {
		t.Errorf("expected '2.0\\n' in debian-binary, got '%s'", string(deb.DebianBinary))
	}

	// 7. Verify control.tar.gz members
	controlHeaders, err := fsutil.ReadTarGzHeaders(deb.ControlTarGz)
	if err != nil {
		t.Fatalf("failed reading control.tar.gz headers: %v", err)
	}

	var controlNames []string
	for _, h := range controlHeaders {
		controlNames = append(controlNames, h.Name)
		if h.Uid != 0 || h.Gid != 0 {
			t.Errorf("control header '%s' ownership not root:root (UID %d, GID %d)", h.Name, h.Uid, h.Gid)
		}
	}

	expectedControlFiles := []string{"control", "conffiles", "md5sums", "preinst", "postinst"}
	for _, name := range expectedControlFiles {
		found := false
		for _, cn := range controlNames {
			if cn == name || cn == "./"+name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("control.tar.gz missing expected member '%s', found: %v", name, controlNames)
		}
	}

	// 8. Verify data.tar.gz members and hierarchy
	dataHeaders, err := fsutil.ReadTarGzHeaders(deb.DataTarGz)
	if err != nil {
		t.Fatalf("failed reading data.tar.gz headers: %v", err)
	}

	var dataNames []string
	for _, h := range dataHeaders {
		dataNames = append(dataNames, h.Name)
		if h.Uid != 0 || h.Gid != 0 {
			t.Errorf("data header '%s' ownership not root:root", h.Name)
		}
	}

	expectedDataFiles := []string{
		"usr/bin/craftpack",
		"usr/lib/craftpack/craftpack",
		"usr/lib/craftpack/data.txt",
		"usr/share/man/man1/craftpack.1.gz",
		"etc/craftpack/craftpack.yml",
	}

	for _, name := range expectedDataFiles {
		found := false
		for _, dn := range dataNames {
			if dn == name || dn == "./"+name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("data.tar.gz missing expected member '%s', found: %v", name, dataNames)
		}
	}

	// 9. Host dpkg-deb verification (if dpkg-deb is available on system)
	if dpkgPath, err := exec.LookPath("dpkg-deb"); err == nil {
		// Run dpkg-deb -I
		infoCmd := exec.Command(dpkgPath, "-I", res.PackageFile)
		infoOut, err := infoCmd.CombinedOutput()
		if err != nil {
			t.Errorf("dpkg-deb -I failed: %v\nOutput:\n%s", err, string(infoOut))
		} else {
			if !strings.Contains(string(infoOut), "Package: craftpack") {
				t.Errorf("dpkg-deb -I output missing package name: %s", string(infoOut))
			}
		}

		// Run dpkg-deb -c
		contentsCmd := exec.Command(dpkgPath, "-c", res.PackageFile)
		contentsOut, err := contentsCmd.CombinedOutput()
		if err != nil {
			t.Errorf("dpkg-deb -c failed: %v\nOutput:\n%s", err, string(contentsOut))
		} else {
			if !strings.Contains(string(contentsOut), "usr/bin/craftpack") {
				t.Errorf("dpkg-deb -c output missing launcher: %s", string(contentsOut))
			}
		}
	}
}

func TestPackager_Build_WithPreStagedDataDir(t *testing.T) {
	outDir := t.TempDir()
	dataDir := t.TempDir()

	// Pre-stage files in dataDir
	binPath := filepath.Join(dataDir, "usr", "bin", "sample")
	_ = os.MkdirAll(filepath.Dir(binPath), 0755)
	_ = os.WriteFile(binPath, []byte("#!/bin/sh\necho hi\n"), 0755)

	cfg := &spec.CraftpackConfig{
		Name:        "sample",
		Maintainer:  "Dev <dev@test.org>",
		Description: "Sample package",
	}

	packager := NewPackager()
	res, err := packager.Build(context.Background(), target.PackageOptions{
		Config:         cfg,
		OutputDir:      outDir,
		PackageVersion: "1.0.0",
		Architecture:   "amd64",
		DataDir:        dataDir,
	})
	if err != nil {
		t.Fatalf("unexpected error with pre-staged DataDir: %v", err)
	}

	if fi, err := os.Stat(res.PackageFile); err != nil || fi.Size() == 0 {
		t.Fatalf("expected valid package file from pre-staged DataDir")
	}
}

func TestExtractGzipPayload(t *testing.T) {
	// Test extracting a gzipped buffer
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	_, _ = gw.Write([]byte("sample data"))
	_ = gw.Close()

	gr, err := gzip.NewReader(&buf)
	if err != nil {
		t.Fatalf("failed creating gzip reader: %v", err)
	}
	defer gr.Close()

	out, err := io.ReadAll(gr)
	if err != nil || string(out) != "sample data" {
		t.Errorf("failed decompressing gzip: %v, got %s", err, string(out))
	}
}

func TestPackager_TargetNameAndRegistry(t *testing.T) {
	packager := NewPackager()
	if packager.TargetName() != "deb" {
		t.Errorf("expected TargetName 'deb', got '%s'", packager.TargetName())
	}

	fromRegistry, err := target.Get("deb")
	if err != nil {
		t.Fatalf("unexpected error getting 'deb' from registry: %v", err)
	}
	if fromRegistry.TargetName() != "deb" {
		t.Errorf("expected registry target 'deb', got '%s'", fromRegistry.TargetName())
	}
}

func TestPackager_BuildValidationErrors(t *testing.T) {
	packager := NewPackager()
	ctx := context.Background()

	// Nil config
	_, err := packager.Build(ctx, target.PackageOptions{Config: nil})
	if err == nil {
		t.Errorf("expected error for nil config")
	}

	// Empty config name
	_, err = packager.Build(ctx, target.PackageOptions{
		Config:         &spec.CraftpackConfig{Name: ""},
		PackageVersion: "1.0",
	})
	if err == nil {
		t.Errorf("expected error for empty config name")
	}

	// Empty package version
	_, err = packager.Build(ctx, target.PackageOptions{
		Config:         &spec.CraftpackConfig{Name: "app"},
		PackageVersion: "",
	})
	if err == nil {
		t.Errorf("expected error for empty version")
	}

	// Invalid payload dir (not a directory)
	tempWorkspace := t.TempDir()
	filePath := filepath.Join(tempWorkspace, "somefile.txt")
	_ = os.WriteFile(filePath, []byte("file"), 0644)
	_, err = packager.Build(ctx, target.PackageOptions{
		Config: &spec.CraftpackConfig{
			Name:       "app",
			PayloadDir: "somefile.txt",
		},
		WorkspaceDir:   tempWorkspace,
		PackageVersion: "1.0",
	})
	if err == nil {
		t.Errorf("expected error when payload_dir is a regular file")
	}
}

func TestReadDeb_Errors(t *testing.T) {
	// Nil reader
	_, err := ReadDeb(nil)
	if err == nil {
		t.Errorf("expected error for nil reader")
	}

	// Invalid sequence
	var buf bytes.Buffer
	// Assemble bad sequence: control first instead of debian-binary
	err = AssembleDeb(&buf, []byte("ctrl"), []byte("data"), time.Now())
	if err != nil {
		t.Fatalf("assemble failed: %v", err)
	}

	// Corrupted bytes
	_, err = ReadDeb(bytes.NewReader([]byte("corrupted ar archive header")))
	if err == nil {
		t.Errorf("expected error for corrupted archive")
	}
}

func TestGenerateConffilesFromConfig_NilConfig(t *testing.T) {
	_, err := GenerateConffilesFromConfig(nil)
	if err == nil {
		t.Errorf("expected error for nil config in GenerateConffilesFromConfig")
	}
}

func TestGenerateMaintainerScripts_NilConfig(t *testing.T) {
	_, err := GenerateMaintainerScripts(nil, t.TempDir())
	if err == nil {
		t.Errorf("expected error for nil config in GenerateMaintainerScripts")
	}
}

func TestResolveHookContent_Empty(t *testing.T) {
	res, err := ResolveHookContent(t.TempDir(), "")
	if err != nil || res != "" {
		t.Errorf("expected empty string and nil error, got %q, %v", res, err)
	}
}

func TestGenerateMD5SumsFromDir_NonExistent(t *testing.T) {
	_, err := GenerateMD5SumsFromDir("/non/existent/path/that/does/not/exist")
	if err == nil {
		t.Errorf("expected error for non-existent directory in GenerateMD5SumsFromDir")
	}
}

func TestGenerateControl_HeaderInjection(t *testing.T) {
	baseData := ControlData{
		Package:      "valid-package",
		Version:      "1.0.0",
		Architecture: "amd64",
		Maintainer:   "Dev <dev@test.org>",
		Description:  "Valid description synopsis\n Extended description line.",
		Homepage:     "https://example.com",
		License:      "Apache-2.0",
		Section:      "utils",
		Priority:     "optional",
		Depends:      "libc6",
	}

	tests := []struct {
		name      string
		modify    func(d *ControlData)
		errSubstr string
	}{
		{
			name: "package contains newline",
			modify: func(d *ControlData) {
				d.Package = "evil\nPackage: hacked"
			},
			errSubstr: "cannot contain newline characters",
		},
		{
			name: "version contains carriage return",
			modify: func(d *ControlData) {
				d.Version = "1.0.0\r\n"
			},
			errSubstr: "cannot contain newline characters",
		},
		{
			name: "architecture contains newline",
			modify: func(d *ControlData) {
				d.Architecture = "amd64\n"
			},
			errSubstr: "cannot contain newline characters",
		},
		{
			name: "maintainer contains newline",
			modify: func(d *ControlData) {
				d.Maintainer = "Hacker <h@test.org>\nEssential: yes"
			},
			errSubstr: "cannot contain newline characters",
		},
		{
			name: "homepage contains carriage return",
			modify: func(d *ControlData) {
				d.Homepage = "https://example.com\r"
			},
			errSubstr: "cannot contain newline characters",
		},
		{
			name: "license contains newline",
			modify: func(d *ControlData) {
				d.License = "MIT\n"
			},
			errSubstr: "cannot contain newline characters",
		},
		{
			name: "section contains newline",
			modify: func(d *ControlData) {
				d.Section = "admin\n"
			},
			errSubstr: "cannot contain newline characters",
		},
		{
			name: "priority contains newline",
			modify: func(d *ControlData) {
				d.Priority = "important\n"
			},
			errSubstr: "cannot contain newline characters",
		},
		{
			name: "depends contains newline injection",
			modify: func(d *ControlData) {
				d.Depends = "libc6\nPre-Depends: bash"
			},
			errSubstr: "cannot contain newline characters",
		},
		{
			name: "empty package",
			modify: func(d *ControlData) {
				d.Package = ""
			},
			errSubstr: "package name is mandatory",
		},
		{
			name: "empty version",
			modify: func(d *ControlData) {
				d.Version = ""
			},
			errSubstr: "version is mandatory",
		},
		{
			name: "empty architecture",
			modify: func(d *ControlData) {
				d.Architecture = ""
			},
			errSubstr: "architecture is mandatory",
		},
		{
			name: "empty description",
			modify: func(d *ControlData) {
				d.Description = "   "
			},
			errSubstr: "description is mandatory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := baseData
			tt.modify(&d)
			_, err := GenerateControl(d)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.errSubstr)
			}
			if !strings.Contains(err.Error(), tt.errSubstr) {
				t.Errorf("error %q does not contain expected substring %q", err.Error(), tt.errSubstr)
			}
		})
	}
}

func TestGenerateControl_MultilineDescriptionFormatting(t *testing.T) {
	d := ControlData{
		Package:      "myapp",
		Version:      "1.0.0",
		Architecture: "amd64",
		Maintainer:   "Dev <dev@test.org>",
		Description:  "Short synopsis\nFirst detail line\n\nThird line after empty line\n  Line with existing indent",
	}

	content, err := GenerateControl(d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedDesc := "Description: Short synopsis\n First detail line\n .\n Third line after empty line\n  Line with existing indent\n"
	if !strings.Contains(string(content), expectedDesc) {
		t.Errorf("expected formatted description:\n%s\nGot:\n%s", expectedDesc, string(content))
	}
}

func TestGenerateConffiles_EdgeCases(t *testing.T) {
	// Deduplication test
	defaultConfig := map[string]string{
		"src/app.conf":      "app.conf",
		"template/app.conf": "app.conf",
	}
	content, err := GenerateConffiles("myapp", defaultConfig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "/etc/myapp/app.conf\n"
	if string(content) != expected {
		t.Errorf("expected deduplicated conffiles %q, got %q", expected, string(content))
	}

	// Invalid app IDs
	invalidAppIDs := []string{
		"../myapp",
		"myapp/sub",
		"myapp\\sub",
		"myapp\nname",
		"",
		" ",
	}
	for _, badID := range invalidAppIDs {
		_, err := GenerateConffiles(badID, map[string]string{"foo": "bar.conf"})
		if err == nil {
			t.Errorf("expected error for invalid appID %q, got nil", badID)
		}
	}

	// Path traversal in target
	traversals := []string{
		"../escape.conf",
		"../../escape.conf",
		"",
		".",
	}
	for _, badTarget := range traversals {
		_, err := GenerateConffiles("myapp", map[string]string{"foo": badTarget})
		if err == nil {
			t.Errorf("expected error for invalid target path %q, got nil", badTarget)
		}
	}

	// Empty config map returns empty
	emptyRes, err := GenerateConffiles("myapp", nil)
	if err != nil || len(emptyRes) != 0 {
		t.Errorf("expected empty result for nil config map, got %q, %v", string(emptyRes), err)
	}
}

func TestAdaptMaintainerScript_EdgeCases(t *testing.T) {
	// Carriage returns and CRLF
	crlfScript := "#!/bin/sh\r\necho 'hello'\r\nexit 0\r\n"
	adapted := string(AdaptMaintainerScript(crlfScript))
	if strings.Contains(adapted, "\r") {
		t.Errorf("adapted script contains carriage returns: %q", adapted)
	}
	if !strings.HasPrefix(adapted, "#!/bin/sh\nset -e\n") {
		t.Errorf("expected shebang and set -e, got:\n%s", adapted)
	}

	// Script with comment lines after shebang
	scriptWithComments := "#!/bin/sh\n# Copyright 2026\n# All rights reserved\necho running\n"
	adaptedComments := string(AdaptMaintainerScript(scriptWithComments))
	lines := strings.Split(adaptedComments, "\n")
	if lines[0] != "#!/bin/sh" {
		t.Errorf("expected shebang as first line, got %q", lines[0])
	}
	foundSetE := false
	for _, l := range lines {
		if strings.TrimSpace(l) == "set -e" {
			foundSetE = true
			break
		}
	}
	if !foundSetE {
		t.Errorf("missing set -e in adapted script:\n%s", adaptedComments)
	}

	// Script already containing set -o errexit
	errexitScript := "#!/bin/bash\nset -o errexit\necho hi\n"
	adaptedErrexit := string(AdaptMaintainerScript(errexitScript))
	if strings.Count(adaptedErrexit, "set -e") > 0 {
		t.Errorf("should not inject redundant set -e when set -o errexit exists:\n%s", adaptedErrexit)
	}

	// Script with custom shebang - Debian policy normalizes to #!/bin/sh
	flaggedShebang := "#!/bin/bash -x\necho trace\n"
	adaptedFlagged := string(AdaptMaintainerScript(flaggedShebang))
	if !strings.HasPrefix(adaptedFlagged, "#!/bin/sh\nset -e\n") {
		t.Errorf("expected normalized shebang and set -e, got:\n%s", adaptedFlagged)
	}

	// Empty and whitespace scripts yield standard skeleton
	emptySkeleton := "#!/bin/sh\nset -e\n"
	if string(AdaptMaintainerScript("")) != emptySkeleton {
		t.Errorf("expected empty skeleton, got: %q", string(AdaptMaintainerScript("")))
	}
	if string(AdaptMaintainerScript("   \n\n\t  ")) != emptySkeleton {
		t.Errorf("expected empty skeleton for whitespace, got: %q", string(AdaptMaintainerScript("   \n\n\t  ")))
	}
}

func TestResolveHookContent_TraversalAndEdgeCases(t *testing.T) {
	tmpDir := t.TempDir()

	// Non-existent traversal path
	_, err := ResolveHookContent(tmpDir, "../escaping.sh")
	if err == nil || !strings.Contains(err.Error(), "path traversal") {
		t.Errorf("expected path traversal error for non-existent outside path, got %v", err)
	}

	// Path that is a directory
	subDir := filepath.Join(tmpDir, "some_dir")
	_ = os.Mkdir(subDir, 0755)
	_, err = ResolveHookContent(tmpDir, "some_dir")
	if err == nil || !strings.Contains(err.Error(), "directory") {
		t.Errorf("expected directory error when hook path is a directory, got %v", err)
	}

	// Inline script containing newlines
	inline := "echo 'line 1'\necho 'line 2'\n"
	res, err := ResolveHookContent(tmpDir, inline)
	if err != nil || res != strings.TrimSpace(inline) {
		t.Errorf("expected exact inline script preserved, got %q, %v", res, err)
	}
}

func TestGenerateMD5SumsFromDir_EdgeCases(t *testing.T) {
	tmpDir := t.TempDir()

	// Regular file passed instead of directory
	filePath := filepath.Join(tmpDir, "regular_file.txt")
	_ = os.WriteFile(filePath, []byte("content"), 0644)
	_, err := GenerateMD5SumsFromDir(filePath)
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("expected 'not a directory' error, got: %v", err)
	}

	// Empty directory returns empty md5sums
	emptyDir := filepath.Join(tmpDir, "empty")
	_ = os.Mkdir(emptyDir, 0755)
	sums, err := GenerateMD5SumsFromDir(emptyDir)
	if err != nil {
		t.Fatalf("unexpected error for empty dir: %v", err)
	}
	if len(sums) != 0 {
		t.Errorf("expected 0 bytes for empty dir md5sums, got %q", string(sums))
	}
}

func TestAssembleDeb_ZeroModTimeDeterminism(t *testing.T) {
	controlData := []byte("control payload")
	dataData := []byte("data payload")

	var buf1, buf2 bytes.Buffer
	// Both called with zero modTime
	err1 := AssembleDeb(&buf1, controlData, dataData, time.Time{})
	err2 := AssembleDeb(&buf2, controlData, dataData, time.Time{})
	if err1 != nil || err2 != nil {
		t.Fatalf("AssembleDeb failed: %v, %v", err1, err2)
	}

	if !bytes.Equal(buf1.Bytes(), buf2.Bytes()) {
		t.Errorf("AssembleDeb outputs are not byte-for-byte deterministic with zero modTime")
	}

	deb, err := ReadDeb(bytes.NewReader(buf1.Bytes()))
	if err != nil {
		t.Fatalf("ReadDeb failed: %v", err)
	}
	if string(deb.DebianBinary) != "2.0\n" {
		t.Errorf("expected 2.0\\n, got %q", string(deb.DebianBinary))
	}
}

func createTestAr(members []struct {
	name string
	data []byte
}) []byte {
	var buf bytes.Buffer
	aw := ar.NewWriter(&buf)
	_ = aw.WriteGlobalHeader()
	for _, m := range members {
		_ = aw.WriteHeader(&ar.Header{
			Name: m.name,
			Size: int64(len(m.data)),
			Mode: 0644,
		})
		_, _ = aw.Write(m.data)
	}
	return buf.Bytes()
}

func TestReadDeb_StrictValidation(t *testing.T) {
	controlData := []byte("ctrl")
	dataData := []byte("data")
	now := time.Now()

	// Helper to assemble a valid ar archive
	var validBuf bytes.Buffer
	if err := AssembleDeb(&validBuf, controlData, dataData, now); err != nil {
		t.Fatalf("AssembleDeb failed: %v", err)
	}

	// 1. Truncated before ar header
	_, err := ReadDeb(bytes.NewReader([]byte("!<ar")))
	if err == nil {
		t.Errorf("expected error for truncated ar archive")
	}

	// 2. Bad magic header
	badMagic := bytes.Clone(validBuf.Bytes())
	copy(badMagic[:8], []byte("!<evil>\n"))
	_, err = ReadDeb(bytes.NewReader(badMagic))
	if err == nil || !strings.Contains(err.Error(), "invalid ar archive magic") {
		t.Errorf("expected invalid ar magic error, got: %v", err)
	}

	// 3. Corrupted debian-binary content (e.g. 3.0\n)
	badVersion := bytes.Replace(validBuf.Bytes(), []byte("2.0\n"), []byte("3.0\n"), 1)
	_, err = ReadDeb(bytes.NewReader(badVersion))
	if err == nil || !strings.Contains(err.Error(), "invalid debian-binary content") {
		t.Errorf("expected invalid debian-binary content error, got: %v", err)
	}

	// 4. Incomplete archive with only debian-binary (missing control and data)
	shortData := createTestAr([]struct {
		name string
		data []byte
	}{
		{name: "debian-binary", data: []byte("2.0\n")},
	})
	_, err = ReadDeb(bytes.NewReader(shortData))
	if err == nil || !strings.Contains(err.Error(), "invalid .deb member sequence") {
		t.Errorf("expected member sequence error, got: %v", err)
	}

	// 5. Wrong member sequence (member 1 is not debian-binary)
	wrongSeq := createTestAr([]struct {
		name string
		data []byte
	}{
		{name: "control.tar.gz", data: controlData},
		{name: "debian-binary", data: []byte("2.0\n")},
		{name: "data.tar.gz", data: dataData},
	})
	_, err = ReadDeb(bytes.NewReader(wrongSeq))
	if err == nil || !strings.Contains(err.Error(), "invalid .deb member sequence") {
		t.Errorf("expected member sequence error, got: %v", err)
	}

	// 6. Member 2 is not control.tar.gz
	wrongMember2 := createTestAr([]struct {
		name string
		data []byte
	}{
		{name: "debian-binary", data: []byte("2.0\n")},
		{name: "foo.tar.gz", data: controlData},
		{name: "data.tar.gz", data: dataData},
	})
	_, err = ReadDeb(bytes.NewReader(wrongMember2))
	if err == nil || !strings.Contains(err.Error(), "invalid .deb member sequence") {
		t.Errorf("expected member sequence error, got: %v", err)
	}

	// 7. Member 3 is not data.tar.gz
	wrongMember3 := createTestAr([]struct {
		name string
		data []byte
	}{
		{name: "debian-binary", data: []byte("2.0\n")},
		{name: "control.tar.gz", data: controlData},
		{name: "bar.tar.gz", data: dataData},
	})
	_, err = ReadDeb(bytes.NewReader(wrongMember3))
	if err == nil || !strings.Contains(err.Error(), "invalid .deb member sequence") {
		t.Errorf("expected member sequence error, got: %v", err)
	}
}

func TestPackager_NormalizeArchitecture_FullMatrix(t *testing.T) {
	archMatrix := []struct {
		input string
		want  string
	}{
		{"x86_64", "amd64"},
		{"x64", "amd64"},
		{"amd64", "amd64"},
		{"aarch64", "arm64"},
		{"arm64", "arm64"},
		{"i386", "i386"},
		{"i686", "i386"},
		{"386", "i386"},
		{"x86", "i386"},
		{"arm", "armhf"},
		{"armhf", "armhf"},
		{"armv7l", "armhf"},
		{"armel", "armel"},
		{"armv5", "armel"},
		{"armv6", "armel"},
		{"ppc64le", "ppc64el"},
		{"ppc64el", "ppc64el"},
		{"riscv64", "riscv64"},
		{"s390x", "s390x"},
		{"all", "all"},
		{"noarch", "all"},
		{"any", "all"},
		{"mips", "mips"},
		{"sparc64", "sparc64"},
	}

	for _, tt := range archMatrix {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeArchitecture(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeArchitecture(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}

	host := DefaultHostArchitecture()
	if host == "" {
		t.Errorf("expected non-empty DefaultHostArchitecture")
	}
}

func TestPackager_Build_SecurityAndEdgeCases(t *testing.T) {
	packager := NewPackager()

	// 1. Context already cancelled
	ctxCancelled, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := &spec.CraftpackConfig{
		Name:        "testapp",
		Description: "Test application",
	}
	_, err := packager.Build(ctxCancelled, target.PackageOptions{
		Config:         cfg,
		PackageVersion: "1.0.0",
		OutputDir:      t.TempDir(),
	})
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled error, got: %v", err)
	}

	// 2. DefaultConfig path traversal
	tmpWorkspace := t.TempDir()
	confSrc := filepath.Join(tmpWorkspace, "my.conf")
	_ = os.WriteFile(confSrc, []byte("key=val\n"), 0644)

	cfgTraversal := &spec.CraftpackConfig{
		Name:        "testapp",
		Description: "Test application",
		DefaultConfig: map[string]string{
			"my.conf": "../evil.conf",
		},
	}
	_, err = packager.Build(context.Background(), target.PackageOptions{
		Config:         cfgTraversal,
		WorkspaceDir:   tmpWorkspace,
		PackageVersion: "1.0.0",
		OutputDir:      t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "clean relative path") {
		t.Errorf("expected clean relative path error for DefaultConfig traversal, got: %v", err)
	}

	// 3. PayloadDir symlink escape
	payloadDir := filepath.Join(tmpWorkspace, "payload")
	_ = os.MkdirAll(payloadDir, 0755)
	outsideTarget := filepath.Join(t.TempDir(), "secret.txt")
	_ = os.WriteFile(outsideTarget, []byte("secret data"), 0600)
	symlinkPath := filepath.Join(payloadDir, "evil_link")
	_ = os.Symlink(outsideTarget, symlinkPath)

	cfgSymlink := &spec.CraftpackConfig{
		Name:        "testapp",
		Description: "Test application",
		PayloadDir:  "payload",
	}
	_, err = packager.Build(context.Background(), target.PackageOptions{
		Config:         cfgSymlink,
		WorkspaceDir:   tmpWorkspace,
		PackageVersion: "1.0.0",
		OutputDir:      t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "escaped boundary") {
		t.Errorf("expected escaped boundary error for symlink escape, got: %v", err)
	}

	// 4. DataDir symlink escape
	outsideDir := t.TempDir()
	dataDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "outside.txt")
	_ = os.WriteFile(outsideFile, []byte("outside"), 0644)
	_ = os.Symlink(outsideFile, filepath.Join(dataDir, "link_outside"))

	_, err = packager.Build(context.Background(), target.PackageOptions{
		Config:         cfg,
		PackageVersion: "1.0.0",
		DataDir:        dataDir,
		OutputDir:      t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "escaped boundary") {
		t.Errorf("expected escaped boundary error for DataDir symlink escape, got: %v", err)
	}
}

type failWriter struct{}

func (f *failWriter) Write(p []byte) (n int, err error) {
	return 0, errors.New("write failure")
}

func TestAssembleDeb_FailWriter(t *testing.T) {
	err := AssembleDeb(&failWriter{}, []byte("ctrl"), []byte("data"), time.Now())
	if err == nil || !strings.Contains(err.Error(), "failed writing") {
		t.Errorf("expected write error from failWriter, got: %v", err)
	}
}

func TestFormatDescription_Empty(t *testing.T) {
	var b strings.Builder
	formatDescription(&b, "")
	if b.String() != "Description: \n" {
		t.Errorf("expected 'Description: \\n', got %q", b.String())
	}
}

func TestGenerateMD5SumsFromEntries_SourcePathAndEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	sourceFile := filepath.Join(tmpDir, "source.txt")
	_ = os.WriteFile(sourceFile, []byte("file data"), 0644)

	entries := []fsutil.TarEntry{
		{Path: "usr/bin/tool", SourcePath: sourceFile},
		{Path: "usr/share/empty.txt"}, // len(Data)==0, SourcePath==""
		{Path: ""},                     // Path=="" should be skipped
		{Path: "."},                    // Path=="." should be skipped
	}

	sums, err := GenerateMD5SumsFromEntries(entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(sums), "usr/bin/tool") || !strings.Contains(string(sums), "usr/share/empty.txt") {
		t.Errorf("expected both entries in md5sums: %s", string(sums))
	}

	// Missing SourcePath returns error
	badEntries := []fsutil.TarEntry{
		{Path: "usr/bin/bad", SourcePath: filepath.Join(tmpDir, "does_not_exist")},
	}
	_, err = GenerateMD5SumsFromEntries(badEntries)
	if err == nil || !strings.Contains(err.Error(), "failed reading source file") {
		t.Errorf("expected error for missing SourcePath, got: %v", err)
	}
}

func TestPackager_Build_DefaultConfigMissingSource(t *testing.T) {
	packager := NewPackager()
	tmpWorkspace := t.TempDir()

	cfgMissingConf := &spec.CraftpackConfig{
		Name:        "testapp",
		Description: "Test application",
		DefaultConfig: map[string]string{
			"non_existent.conf": "app.conf",
		},
	}

	_, err := packager.Build(context.Background(), target.PackageOptions{
		Config:         cfgMissingConf,
		WorkspaceDir:   tmpWorkspace,
		PackageVersion: "1.0.0",
		OutputDir:      t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "failed reading default_config") {
		t.Errorf("expected error for non-existent default_config source, got: %v", err)
	}
}

func TestPackager_Build_PreStagedDataDir_NonExecAndEmptyOutputDir(t *testing.T) {
	tempWorkspace := t.TempDir()
	dataDir := filepath.Join(tempWorkspace, "staged")
	f1 := filepath.Join(dataDir, "usr", "share", "doc", "readme.txt")
	_ = os.MkdirAll(filepath.Dir(f1), 0755)
	_ = os.WriteFile(f1, []byte("readme text"), 0644) // non-executable file

	cfg := &spec.CraftpackConfig{
		Name:        "stagedapp",
		Description: "Staged app with non-exec file",
		Maintainer:  "Dev <dev@test.org>",
		// Targets.Deb is nil to exercise default DebianTargetConfig
	}

	packager := NewPackager()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	_ = os.Chdir(tempWorkspace)

	res, err := packager.Build(context.Background(), target.PackageOptions{
		Config:         cfg,
		PackageVersion: "1.0.0",
		DataDir:        dataDir,
		OutputDir:      "",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(res.Filename, "stagedapp_1.0.0_") {
		t.Errorf("unexpected filename %s", res.Filename)
	}
}



