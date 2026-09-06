// SPDX-FileCopyrightText: 2026 Marcin Kaim
// SPDX-License-Identifier: Apache-2.0

package deb

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/md5"
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
