// SPDX-FileCopyrightText: 2026 Marcin Kaim
// SPDX-License-Identifier: GPL-3.0-only

package template

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolver_TiersPrecedence(t *testing.T) {
	tmp := t.TempDir()

	envDir := filepath.Join(tmp, "env")
	wsDir := filepath.Join(tmp, "workspace")
	wsTemplates := filepath.Join(wsDir, "templates")
	userDir := filepath.Join(tmp, "user")
	sysDir := filepath.Join(tmp, "system")

	for _, d := range []string{envDir, wsTemplates, userDir, sysDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("failed creating dir %s: %v", d, err)
		}
	}

	_ = os.WriteFile(filepath.Join(envDir, "deb.yml"), []byte("name: env-deb\ndescription: Env template\n"), 0644)
	_ = os.WriteFile(filepath.Join(wsTemplates, "deb.yml"), []byte("name: ws-deb\ndescription: Workspace template\n"), 0644)
	_ = os.WriteFile(filepath.Join(userDir, "deb.yml"), []byte("name: user-deb\ndescription: User template\n"), 0644)
	_ = os.WriteFile(filepath.Join(sysDir, "deb.yml"), []byte("name: sys-deb\ndescription: System template\n"), 0644)

	// 1. All tiers present: Env wins
	r := &Resolver{
		Cwd:                wsDir,
		EnvTemplatesDir:    envDir,
		UserTemplatesDir:   userDir,
		SystemTemplatesDir: sysDir,
	}
	tmpl, data, err := r.Find("deb")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tmpl.Origin != OriginEnv {
		t.Errorf("origin = %s, want %s", tmpl.Origin, OriginEnv)
	}
	if tmpl.Description != "Env template" {
		t.Errorf("description = %q, want 'Env template'", tmpl.Description)
	}
	if string(data) != "name: env-deb\ndescription: Env template\n" {
		t.Errorf("data mismatch: %s", string(data))
	}

	// 2. Env empty: Workspace wins
	r.EnvTemplatesDir = ""
	tmpl, _, err = r.Find("deb")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tmpl.Origin != OriginWorkspace {
		t.Errorf("origin = %s, want %s", tmpl.Origin, OriginWorkspace)
	}

	// 3. Workspace templates removed: User wins
	_ = os.Remove(filepath.Join(wsTemplates, "deb.yml"))
	tmpl, _, err = r.Find("deb")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tmpl.Origin != OriginUser {
		t.Errorf("origin = %s, want %s", tmpl.Origin, OriginUser)
	}

	// 4. User removed: System wins
	_ = os.Remove(filepath.Join(userDir, "deb.yml"))
	tmpl, _, err = r.Find("deb")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tmpl.Origin != OriginSystem {
		t.Errorf("origin = %s, want %s", tmpl.Origin, OriginSystem)
	}

	// 5. System removed: Not found
	_ = os.Remove(filepath.Join(sysDir, "deb.yml"))
	_, _, err = r.Find("deb")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, ErrTemplateNotFound) {
		t.Errorf("expected ErrTemplateNotFound, got: %v", err)
	}
}

func TestResolver_ListDeduplication(t *testing.T) {
	tmp := t.TempDir()

	wsDir := filepath.Join(tmp, "workspace")
	wsTemplates := filepath.Join(wsDir, "templates")
	sysDir := filepath.Join(tmp, "system")

	_ = os.MkdirAll(wsTemplates, 0755)
	_ = os.MkdirAll(sysDir, 0755)

	_ = os.WriteFile(filepath.Join(wsTemplates, "alpha.yml"), []byte("description: Alpha from workspace\n"), 0644)
	_ = os.WriteFile(filepath.Join(wsTemplates, "deb.yml"), []byte("description: Deb from workspace\n"), 0644)
	_ = os.WriteFile(filepath.Join(sysDir, "deb.yml"), []byte("description: Deb from system\n"), 0644)
	_ = os.WriteFile(filepath.Join(sysDir, "zeta.yaml"), []byte("description: Zeta from system\n"), 0644)
	_ = os.WriteFile(filepath.Join(sysDir, "ignore.txt"), []byte("text file\n"), 0644)

	r := &Resolver{
		Cwd:                wsDir,
		SystemTemplatesDir: sysDir,
	}

	list, err := r.List()
	if err != nil {
		t.Fatalf("unexpected list error: %v", err)
	}

	if len(list) != 3 {
		t.Fatalf("expected 3 templates, got %d: %+v", len(list), list)
	}

	// Check alphabetical sort: alpha, deb, zeta
	if list[0].Name != "alpha" || list[0].Origin != OriginWorkspace {
		t.Errorf("item 0 mismatch: %+v", list[0])
	}
	if list[1].Name != "deb" || list[1].Origin != OriginWorkspace || list[1].Description != "Deb from workspace" {
		t.Errorf("item 1 (dedup deb) mismatch: %+v", list[1])
	}
	if list[2].Name != "zeta" || list[2].Origin != OriginSystem {
		t.Errorf("item 2 mismatch: %+v", list[2])
	}
}

func TestResolver_FindExtensions(t *testing.T) {
	tmp := t.TempDir()
	wsDir := filepath.Join(tmp, "workspace")
	wsTemplates := filepath.Join(wsDir, "templates")
	_ = os.MkdirAll(wsTemplates, 0755)

	_ = os.WriteFile(filepath.Join(wsTemplates, "custom.yaml"), []byte("description: YAML custom\n"), 0644)

	r := &Resolver{Cwd: wsDir}

	// Lookup without extension
	tmpl, _, err := r.Find("custom")
	if err != nil {
		t.Fatalf("unexpected error finding 'custom': %v", err)
	}
	if tmpl.Name != "custom" {
		t.Errorf("template name = %q, want 'custom'", tmpl.Name)
	}

	// Lookup with extension
	tmpl, _, err = r.Find("custom.yaml")
	if err != nil {
		t.Fatalf("unexpected error finding 'custom.yaml': %v", err)
	}
	if tmpl.Name != "custom" {
		t.Errorf("template name = %q, want 'custom'", tmpl.Name)
	}
}

func TestResolver_InvalidNames(t *testing.T) {
	r := &Resolver{Cwd: "/some/path"}

	for _, bad := range []string{"../deb", "foo/bar", "foo\\bar", ".."} {
		_, _, err := r.Find(bad)
		if err == nil {
			t.Errorf("expected error for invalid name %q, got nil", bad)
		}
	}
}

func TestExtractDescription_FallbackComment(t *testing.T) {
	content := []byte(`# SPDX-FileCopyrightText: 2026 Test
# SPDX-License-Identifier: Apache-2.0

# ==============================================================================
# Sample Application Configuration
# ==============================================================================

name: test-app
`)
	desc := extractTemplateDescription(content)
	if desc != "Sample Application Configuration" {
		t.Errorf("extracted description = %q, want 'Sample Application Configuration'", desc)
	}
}
