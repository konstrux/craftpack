// SPDX-FileCopyrightText: 2026 Marcin Kaim
// SPDX-License-Identifier: Apache-2.0

package deb

import (
	"errors"
	"fmt"
	"strings"

	"craftpack/pkg/spec"
)

// ControlData holds the fields required to synthesize a DEBIAN/control file.
type ControlData struct {
	Package      string
	Version      string
	Architecture string
	Maintainer   string
	Description  string
	Homepage     string
	License      string
	Section      string
	Priority     string
	Depends      string
}

// formatDescription formats the Description field adhering strictly to Debian RFC 822 conventions:
// The first line is the short synopsis. Subsequent lines must start with a leading space.
// Empty lines within a multi-line description are represented by a single dot (" .").
func formatDescription(b *strings.Builder, desc string) {
	trimmedDesc := strings.TrimSpace(desc)
	if trimmedDesc == "" {
		b.WriteString("Description: \n")
		return
	}

	lines := strings.Split(strings.ReplaceAll(trimmedDesc, "\r\n", "\n"), "\n")
	b.WriteString("Description: " + strings.TrimSpace(lines[0]) + "\n")
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			b.WriteString(" .\n")
		} else {
			b.WriteString(" " + trimmed + "\n")
		}
	}
}

// GenerateControl synthesizes the RFC 822 DEBIAN/control file content.
func GenerateControl(data ControlData) ([]byte, error) {
	if strings.TrimSpace(data.Package) == "" {
		return nil, errors.New("package name is mandatory for DEBIAN/control")
	}
	if strings.TrimSpace(data.Version) == "" {
		return nil, errors.New("version is mandatory for DEBIAN/control")
	}
	if strings.TrimSpace(data.Architecture) == "" {
		return nil, errors.New("architecture is mandatory for DEBIAN/control")
	}
	if strings.TrimSpace(data.Maintainer) == "" {
		return nil, errors.New("maintainer is mandatory for DEBIAN/control")
	}

	section := strings.TrimSpace(data.Section)
	if section == "" {
		section = "utils"
	}

	priority := strings.TrimSpace(data.Priority)
	if priority == "" {
		priority = "optional"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Package: %s\n", strings.TrimSpace(data.Package))
	fmt.Fprintf(&b, "Version: %s\n", strings.TrimSpace(data.Version))
	fmt.Fprintf(&b, "Architecture: %s\n", strings.TrimSpace(data.Architecture))
	fmt.Fprintf(&b, "Maintainer: %s\n", strings.TrimSpace(data.Maintainer))
	formatDescription(&b, data.Description)
	if strings.TrimSpace(data.Homepage) != "" {
		fmt.Fprintf(&b, "Homepage: %s\n", strings.TrimSpace(data.Homepage))
	}
	if strings.TrimSpace(data.License) != "" {
		fmt.Fprintf(&b, "License: %s\n", strings.TrimSpace(data.License))
	}
	fmt.Fprintf(&b, "Section: %s\n", section)
	fmt.Fprintf(&b, "Priority: %s\n", priority)
	if strings.TrimSpace(data.Depends) != "" {
		fmt.Fprintf(&b, "Depends: %s\n", strings.TrimSpace(data.Depends))
	}

	return []byte(b.String()), nil
}

// GenerateControlFromConfig translates a CraftpackConfig and build parameters into DEBIAN/control bytes.
func GenerateControlFromConfig(cfg *spec.CraftpackConfig, version, arch string) ([]byte, error) {
	if cfg == nil {
		return nil, errors.New("craftpack config cannot be nil")
	}

	data := ControlData{
		Package:      cfg.Name,
		Version:      NormalizeVersion(version),
		Architecture: NormalizeArchitecture(arch),
		Maintainer:   cfg.Maintainer,
		Description:  cfg.Description,
		Homepage:     cfg.Homepage,
		License:      cfg.License,
		Section:      "utils",
		Priority:     "optional",
	}

	if cfg.Targets.Deb != nil {
		if cfg.Targets.Deb.Section != "" {
			data.Section = cfg.Targets.Deb.Section
		}
		if cfg.Targets.Deb.Priority != "" {
			data.Priority = cfg.Targets.Deb.Priority
		}
		if len(cfg.Targets.Deb.Dependencies) > 0 {
			data.Depends = strings.Join(cfg.Targets.Deb.Dependencies, ", ")
		}
	}

	return GenerateControl(data)
}
