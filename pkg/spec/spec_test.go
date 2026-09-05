// SPDX-FileCopyrightText: 2026 Marcin Kaim
// SPDX-License-Identifier: Apache-2.0

package spec

import (
	"os"
	"path/filepath"
	"testing"
)

func sampleValidYAML() string {
	return `name: myapp
description: A high-performance packaging utility for Linux systems
maintainer: John Doe <john@example.com>
homepage: https://example.com/myapp
license: Apache-2.0
command: myapp
payload_dir: dist/payload
entrypoint: myapp-bin
man_pages:
  - source: docs/myapp.1.md
    section: 1
    title: MYAPP
    header: User Commands Manual
default_config:
  config/app.conf: app.conf
targets:
  deb:
    section: utils
    priority: optional
    dependencies:
      - libc6 (>= 2.31)
      - systemd
`
}

func TestParseBytes_Valid(t *testing.T) {
	yamlData := sampleValidYAML()
	res, err := ParseBytes([]byte(yamlData), ParseOptions{})
	if err != nil {
		t.Fatalf("expected valid parse, got error: %v", err)
	}

	cfg := res.Config
	if cfg.Name != "myapp" {
		t.Errorf("expected name 'myapp', got '%s'", cfg.Name)
	}
	if cfg.Targets.Deb == nil {
		t.Fatal("expected deb target to be non-nil")
	}
	if cfg.Targets.Deb.Section != "utils" {
		t.Errorf("expected section 'utils', got '%s'", cfg.Targets.Deb.Section)
	}
	if cfg.Targets.Deb.Priority != "optional" {
		t.Errorf("expected priority 'optional', got '%s'", cfg.Targets.Deb.Priority)
	}
	if len(cfg.Targets.Deb.Dependencies) != 2 {
		t.Errorf("expected 2 dependencies, got %d", len(cfg.Targets.Deb.Dependencies))
	}
}

func TestParseBytes_DefaultsApplied(t *testing.T) {
	yamlData := `name: myapp
description: A high-performance packaging utility for Linux systems
maintainer: John Doe <john@example.com>
homepage: https://example.com/myapp
license: Apache-2.0
command: myapp
payload_dir: dist/payload
entrypoint: myapp-bin
targets:
  deb: {}
`
	res, err := ParseBytes([]byte(yamlData), ParseOptions{})
	if err != nil {
		t.Fatalf("expected valid parse, got: %v", err)
	}
	if res.Config.Targets.Deb.Section != "utils" {
		t.Errorf("expected default section 'utils', got '%s'", res.Config.Targets.Deb.Section)
	}
	if res.Config.Targets.Deb.Priority != "optional" {
		t.Errorf("expected default priority 'optional', got '%s'", res.Config.Targets.Deb.Priority)
	}
}

func TestValidate_NameConstraints(t *testing.T) {
	tests := []struct {
		name    string
		val     string
		wantErr bool
	}{
		{"valid single word", "craftpack", false},
		{"valid hyphenated", "my-tool-2", false},
		{"invalid uppercase", "MyApp", true},
		{"invalid underscore", "my_app", true},
		{"invalid leading hyphen", "-myapp", true},
		{"invalid trailing hyphen", "myapp-", true},
		{"invalid consecutive hyphens", "my--app", true},
		{"invalid space", "my app", true},
		{"empty name", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &CraftpackConfig{
				Name:        tt.val,
				Description: "Valid description for the application",
				Maintainer:  "Dev <dev@example.com>",
				Homepage:    "https://example.com",
				License:     "MIT",
				Command:     "myapp",
				PayloadDir:  "dist",
				Entrypoint:  "bin",
				Targets:     TargetConfigs{Deb: &DebianTargetConfig{}},
			}
			v := NewValidator("", false)
			errs := v.Validate(cfg)
			hasErr := len(errs) > 0
			if hasErr != tt.wantErr {
				t.Errorf("Name=%q: wantErr=%v, got errs=%v", tt.val, tt.wantErr, errs)
			}
		})
	}
}

func TestValidate_MaintainerConstraints(t *testing.T) {
	tests := []struct {
		name    string
		val     string
		wantErr bool
	}{
		{"valid rfc822", "Marcin Kaim <marcin@example.com>", false},
		{"valid with punctuation", "Jane O'Neil <jane.oneil@sub.domain.org>", false},
		{"missing angle brackets", "Marcin Kaim marcin@example.com", true},
		{"missing name", "<marcin@example.com>", true},
		{"invalid email syntax", "Marcin Kaim <not-an-email>", true},
		{"empty maintainer", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &CraftpackConfig{
				Name:        "myapp",
				Description: "Valid description for the application",
				Maintainer:  tt.val,
				Homepage:    "https://example.com",
				License:     "MIT",
				Command:     "myapp",
				PayloadDir:  "dist",
				Entrypoint:  "bin",
				Targets:     TargetConfigs{Deb: &DebianTargetConfig{}},
			}
			v := NewValidator("", false)
			errs := v.Validate(cfg)
			hasErr := len(errs) > 0
			if hasErr != tt.wantErr {
				t.Errorf("Maintainer=%q: wantErr=%v, got errs=%v", tt.val, tt.wantErr, errs)
			}
		})
	}
}

func TestValidate_HomepageConstraints(t *testing.T) {
	tests := []struct {
		name    string
		val     string
		wantErr bool
	}{
		{"valid https", "https://github.com/craftpack/craftpack", false},
		{"valid http", "http://example.org/project", false},
		{"invalid localhost", "http://localhost:8080", true},
		{"invalid 127.0.0.1", "http://127.0.0.1/app", true},
		{"invalid ipv6 loopback", "http://[::1]/app", true},
		{"invalid relative", "/relative/path", true},
		{"invalid traversal in url", "https://example.com/foo/../bar", true},
		{"empty homepage", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &CraftpackConfig{
				Name:        "myapp",
				Description: "Valid description for the application",
				Maintainer:  "Dev <dev@example.com>",
				Homepage:    tt.val,
				License:     "MIT",
				Command:     "myapp",
				PayloadDir:  "dist",
				Entrypoint:  "bin",
				Targets:     TargetConfigs{Deb: &DebianTargetConfig{}},
			}
			v := NewValidator("", false)
			errs := v.Validate(cfg)
			hasErr := len(errs) > 0
			if hasErr != tt.wantErr {
				t.Errorf("Homepage=%q: wantErr=%v, got errs=%v", tt.val, tt.wantErr, errs)
			}
		})
	}
}

func TestValidate_LicenseSPDX(t *testing.T) {
	tests := []struct {
		name    string
		val     string
		wantErr bool
	}{
		{"valid simple", "Apache-2.0", false},
		{"valid MIT", "MIT", false},
		{"valid GPL", "GPL-3.0-only", false},
		{"valid composite OR", "MIT OR Apache-2.0", false},
		{"valid composite AND with parens", "(BSD-3-Clause AND Apache-2.0)", false},
		{"valid with exception", "GPL-3.0-only WITH GCC-exception-3.1", false},
		{"invalid unknown license", "MyCustomLicense", true},
		{"invalid syntax", "MIT OR", true},
		{"empty license", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &CraftpackConfig{
				Name:        "myapp",
				Description: "Valid description for the application",
				Maintainer:  "Dev <dev@example.com>",
				Homepage:    "https://example.com",
				License:     tt.val,
				Command:     "myapp",
				PayloadDir:  "dist",
				Entrypoint:  "bin",
				Targets:     TargetConfigs{Deb: &DebianTargetConfig{}},
			}
			v := NewValidator("", false)
			errs := v.Validate(cfg)
			hasErr := len(errs) > 0
			if hasErr != tt.wantErr {
				t.Errorf("License=%q: wantErr=%v, got errs=%v", tt.val, tt.wantErr, errs)
			}
		})
	}
}

func TestValidate_CommandBlacklist(t *testing.T) {
	tests := []struct {
		name    string
		val     string
		wantErr bool
	}{
		{"valid custom command", "my-builder", false},
		{"blacklisted cd", "cd", true},
		{"blacklisted ls", "ls", true},
		{"blacklisted tar", "tar", true},
		{"blacklisted sh", "sh", true},
		{"blacklisted dpkg", "dpkg", true},
		{"blacklisted gzip", "gzip", true},
		{"command with slashes", "bin/app", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &CraftpackConfig{
				Name:        "myapp",
				Description: "Valid description for the application",
				Maintainer:  "Dev <dev@example.com>",
				Homepage:    "https://example.com",
				License:     "MIT",
				Command:     tt.val,
				PayloadDir:  "dist",
				Entrypoint:  "bin",
				Targets:     TargetConfigs{Deb: &DebianTargetConfig{}},
			}
			v := NewValidator("", false)
			errs := v.Validate(cfg)
			hasErr := len(errs) > 0
			if hasErr != tt.wantErr {
				t.Errorf("Command=%q: wantErr=%v, got errs=%v", tt.val, tt.wantErr, errs)
			}
		})
	}
}

func TestValidate_DebianTarget(t *testing.T) {
	tests := []struct {
		name     string
		section  string
		priority string
		deps     []string
		wantErr  bool
	}{
		{"valid standard", "utils", "optional", []string{"libc6 (>= 2.31)"}, false},
		{"valid complex deps", "devel", "important", []string{"libc6 (>= 2.31)", "python3 (<< 4.0)", "systemd"}, false},
		{"invalid section", "nonexistent-sec", "optional", nil, true},
		{"invalid priority", "utils", "critical", nil, true},
		{"invalid dep syntax", "utils", "optional", []string{"invalid dep !@#"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &CraftpackConfig{
				Name:        "myapp",
				Description: "Valid description for the application",
				Maintainer:  "Dev <dev@example.com>",
				Homepage:    "https://example.com",
				License:     "MIT",
				Command:     "myapp",
				PayloadDir:  "dist",
				Entrypoint:  "bin",
				Targets: TargetConfigs{
					Deb: &DebianTargetConfig{
						Section:      tt.section,
						Priority:     tt.priority,
						Dependencies: tt.deps,
					},
				},
			}
			v := NewValidator("", false)
			errs := v.Validate(cfg)
			hasErr := len(errs) > 0
			if hasErr != tt.wantErr {
				t.Errorf("DebianConfig wantErr=%v, got errs=%v", tt.wantErr, errs)
			}
		})
	}
}

func TestForwardTolerance_LenientVsStrict(t *testing.T) {
	yamlWithUnknownKey := `name: myapp
description: A high-performance packaging utility for Linux systems
maintainer: John Doe <john@example.com>
homepage: https://example.com/myapp
license: Apache-2.0
command: myapp
payload_dir: dist/payload
entrypoint: myapp-bin
unknown_experimental_key: "some value"
targets:
  deb:
    section: utils
    priority: optional
    unknown_deb_flag: 123
`

	// In Lenient mode (default), parsing should succeed and record warnings
	resLenient, err := ParseBytes([]byte(yamlWithUnknownKey), ParseOptions{Strict: false})
	if err != nil {
		t.Fatalf("expected lenient mode to succeed with warnings, got: %v", err)
	}
	if len(resLenient.Warnings) == 0 {
		t.Errorf("expected warnings in lenient mode, got 0")
	}

	// In Strict mode, parsing should fail with validation error for unknown keys
	_, errStrict := ParseBytes([]byte(yamlWithUnknownKey), ParseOptions{Strict: true})
	if errStrict == nil {
		t.Fatal("expected strict mode to fail on unknown keys, but succeeded")
	}
}

func TestLineAndColumnReporting(t *testing.T) {
	invalidYAML := `name: INVALID_NAME
description: Good description for this tool
maintainer: Dev <dev@example.com>
homepage: https://example.com
license: MIT
command: myapp
payload_dir: dist/payload
entrypoint: myapp-bin
targets:
  deb: {}
`
	_, err := ParseBytes([]byte(invalidYAML), ParseOptions{})
	if err == nil {
		t.Fatal("expected error on invalid name, got nil")
	}

	valErrs, ok := err.(ValidationErrors)
	if !ok {
		t.Fatalf("expected ValidationErrors type, got %T: %v", err, err)
	}

	if len(valErrs) == 0 {
		t.Fatal("expected at least one validation error")
	}

	nameErr := valErrs[0]
	if nameErr.Line != 1 {
		t.Errorf("expected line 1 for name error, got line %d", nameErr.Line)
	}
	if nameErr.Column != 1 {
		t.Errorf("expected column 1 for name error, got column %d", nameErr.Column)
	}
}

func TestValidate_WorkspaceFilesystem(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Create a valid payload directory with a file
	payloadDir := filepath.Join(tempDir, "dist", "payload")
	if err := os.MkdirAll(payloadDir, 0755); err != nil {
		t.Fatal(err)
	}
	entrypointPath := filepath.Join(payloadDir, "myapp-bin")
	if err := os.WriteFile(entrypointPath, []byte("#!/bin/sh\necho hi"), 0755); err != nil {
		t.Fatal(err)
	}

	// 2. Create a clean man page (Zero-Markup)
	docsDir := filepath.Join(tempDir, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatal(err)
	}
	cleanManPage := filepath.Join(docsDir, "myapp.1.md")
	if err := os.WriteFile(cleanManPage, []byte("# NAME\nmyapp - cool tool"), 0644); err != nil {
		t.Fatal(err)
	}

	// 3. Create a default config template
	confDir := filepath.Join(tempDir, "config")
	if err := os.MkdirAll(confDir, 0755); err != nil {
		t.Fatal(err)
	}
	confFile := filepath.Join(confDir, "app.conf")
	if err := os.WriteFile(confFile, []byte("key=value"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &CraftpackConfig{
		Name:        "myapp",
		Description: "A high-performance packaging utility for Linux systems",
		Maintainer:  "Dev <dev@example.com>",
		Homepage:    "https://example.com",
		License:     "MIT",
		Command:     "myapp",
		PayloadDir:  "dist/payload",
		Entrypoint:  "myapp-bin",
		ManPages: []ManPageConfig{
			{Source: "docs/myapp.1.md", Section: 1},
		},
		DefaultConfig: map[string]string{
			"config/app.conf": "app.conf",
		},
		Targets: TargetConfigs{Deb: &DebianTargetConfig{}},
	}

	v := NewValidator(tempDir, true)
	errs := v.Validate(cfg)
	if len(errs) > 0 {
		t.Fatalf("expected valid workspace validation, got errors: %v", errs)
	}

	// Test Zero-Markup violation
	pollutedManPage := filepath.Join(docsDir, "polluted.1.md")
	if err := os.WriteFile(pollutedManPage, []byte("---\ntitle: Polluted\n---\n# Content"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg.ManPages[0].Source = "docs/polluted.1.md"
	errsPolluted := v.Validate(cfg)
	if len(errsPolluted) == 0 {
		t.Fatal("expected Zero-Markup violation error for front-matter, got nil")
	}
}

func TestValidate_DescriptionConstraints(t *testing.T) {
	tests := []struct {
		name    string
		val     string
		wantErr bool
	}{
		{"valid description", "Standard Linux packaging factory for the SDP platform", false},
		{"multiline with newline", "First line\nSecond line", true},
		{"multiline with carriage return", "First line\rSecond line", true},
		{"tab character", "Tab\tdescription", true},
		{"trailing space", "Description with trailing space ", true},
		{"markdown header", "# Markdown Header", true},
		{"markdown bold", "This is **bold** text", true},
		{"shell script command", "echo 'hello'; rm -rf /", true},
		{"empty description", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &CraftpackConfig{
				Name:        "myapp",
				Description: tt.val,
				Maintainer:  "Dev <dev@example.com>",
				Homepage:    "https://example.com",
				License:     "MIT",
				Command:     "myapp",
				PayloadDir:  "dist",
				Entrypoint:  "bin",
				Targets:     TargetConfigs{Deb: &DebianTargetConfig{}},
			}
			v := NewValidator("", false)
			errs := v.Validate(cfg)
			hasErr := len(errs) > 0
			if hasErr != tt.wantErr {
				t.Errorf("Description=%q: wantErr=%v, got errs=%v", tt.val, tt.wantErr, errs)
			}
		})
	}
}

func TestValidate_LifecycleHooks(t *testing.T) {
	tests := []struct {
		name    string
		hook    string
		wantErr bool
	}{
		{"safe script", "#!/bin/sh\necho 'installing'", false},
		{"destructive rm -rf /", "rm -rf /", true},
		{"destructive rm -rf /*", "rm -rf /*", true},
		{"destructive rm -r -f /", "rm -r -f /", true},
		{"absolute script path", "/tmp/preinstall.sh", true},
		{"traversal script path", "../preinstall.sh", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &CraftpackConfig{
				Name:        "myapp",
				Description: "Valid description for the application",
				Maintainer:  "Dev <dev@example.com>",
				Homepage:    "https://example.com",
				License:     "MIT",
				Command:     "myapp",
				PayloadDir:  "dist",
				Entrypoint:  "bin",
				PreInstall:  tt.hook,
				Targets:     TargetConfigs{Deb: &DebianTargetConfig{}},
			}
			v := NewValidator("", false)
			errs := v.Validate(cfg)
			hasErr := len(errs) > 0
			if hasErr != tt.wantErr {
				t.Errorf("Hook=%q: wantErr=%v, got errs=%v", tt.hook, tt.wantErr, errs)
			}
		})
	}
}

func TestValidate_DefaultConfigConstraints(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		target  string
		wantErr bool
	}{
		{"valid flat target", "config/my.conf", "my.conf", false},
		{"target with slash", "config/my.conf", "sub/my.conf", true},
		{"target with backslash", "config/my.conf", "sub\\my.conf", true},
		{"target with traversal", "config/my.conf", "../my.conf", true},
		{"target is dot", "config/my.conf", ".", true},
		{"source is absolute", "/etc/my.conf", "my.conf", true},
		{"source has traversal", "../my.conf", "my.conf", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &CraftpackConfig{
				Name:        "myapp",
				Description: "Valid description for the application",
				Maintainer:  "Dev <dev@example.com>",
				Homepage:    "https://example.com",
				License:     "MIT",
				Command:     "myapp",
				PayloadDir:  "dist",
				Entrypoint:  "bin",
				DefaultConfig: map[string]string{
					tt.src: tt.target,
				},
				Targets: TargetConfigs{Deb: &DebianTargetConfig{}},
			}
			v := NewValidator("", false)
			errs := v.Validate(cfg)
			hasErr := len(errs) > 0
			if hasErr != tt.wantErr {
				t.Errorf("DefaultConfig: src=%q, target=%q: wantErr=%v, got errs=%v", tt.src, tt.target, tt.wantErr, errs)
			}
		})
	}
}

func TestParseFile(t *testing.T) {
	tempDir := t.TempDir()
	specPath := filepath.Join(tempDir, "craftpack.yml")
	if err := os.WriteFile(specPath, []byte(sampleValidYAML()), 0644); err != nil {
		t.Fatal(err)
	}

	res, err := ParseFile(specPath, ParseOptions{})
	if err != nil {
		t.Fatalf("expected ParseFile to succeed, got: %v", err)
	}
	if res.Config.Name != "myapp" {
		t.Errorf("expected name 'myapp', got '%s'", res.Config.Name)
	}

	// Test non-existent file
	_, errMissing := ParseFile(filepath.Join(tempDir, "missing.yml"), ParseOptions{})
	if errMissing == nil {
		t.Fatal("expected error on missing file, got nil")
	}
}
