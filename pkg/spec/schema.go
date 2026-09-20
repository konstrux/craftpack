// SPDX-FileCopyrightText: 2026 Marcin Kaim
// SPDX-License-Identifier: GPL-3.0-only

package spec

import "fmt"

// CraftpackConfig represents the root configuration schema of craftpack.yml.
// It defines metadata, application layout, lifecycle hooks, resources, and target packaging configurations.
type CraftpackConfig struct {
	// Section 6.1: Package Metadata
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Maintainer  string `yaml:"maintainer"`
	Homepage    string `yaml:"homepage"`
	License     string `yaml:"license"`

	// Section 6.2: Core Application Properties
	Command    string `yaml:"command"`
	PayloadDir string `yaml:"payload_dir"`
	Entrypoint string `yaml:"entrypoint"`

	// Section 6.3: Universal Lifecycle Hooks
	PreInstall  string `yaml:"preinstall,omitempty"`
	PostInstall string `yaml:"postinstall,omitempty"`
	PreRemove   string `yaml:"preremove,omitempty"`
	PostRemove  string `yaml:"postremove,omitempty"`

	// Section 6.4: Documentation and Configuration Resources
	ManPages      []ManPageConfig   `yaml:"man_pages,omitempty"`
	TemplatesDir  string            `yaml:"templates_dir,omitempty"`
	DefaultConfig map[string]string `yaml:"default_config,omitempty"`

	// Section 7: Target-Specific Configurations
	Targets TargetConfigs `yaml:"targets"`
}

// ManPageConfig specifies a single markdown source file to be compiled into a roff manual page.
type ManPageConfig struct {
	Source  string `yaml:"source"`
	Section int    `yaml:"section"`
	Name    string `yaml:"name,omitempty"`
	Title   string `yaml:"title,omitempty"`
	Header  string `yaml:"header,omitempty"`
	Footer  string `yaml:"footer,omitempty"`
}

// TargetConfigs encapsulates configuration blocks for specific packaging platforms.
type TargetConfigs struct {
	Deb *DebianTargetConfig `yaml:"deb,omitempty"`
}

// DebianTargetConfig defines target parameters for Debian (.deb) binary packages.
type DebianTargetConfig struct {
	Section      string   `yaml:"section,omitempty"`      // APT category section (default: "utils")
	Priority     string   `yaml:"priority,omitempty"`     // Package priority (default: "optional")
	Wrapper      bool     `yaml:"wrapper,omitempty"`      // If true, generate isolated vault and proxy launcher (default: false)
	Dependencies []string `yaml:"dependencies,omitempty"` // Runtime package dependencies
}

// ValidationError represents a specific schema or constraint violation.
type ValidationError struct {
	Field   string `json:"field"`
	Line    int    `json:"line,omitempty"`
	Column  int    `json:"column,omitempty"`
	Message string `json:"message"`
}

func (e ValidationError) Error() string {
	if e.Line > 0 && e.Column > 0 {
		return fmt.Sprintf("line %d, column %d: [%s] %s", e.Line, e.Column, e.Field, e.Message)
	}
	if e.Field != "" {
		return fmt.Sprintf("[%s] %s", e.Field, e.Message)
	}
	return e.Message
}

// ValidationErrors represents a collection of validation errors.
type ValidationErrors []ValidationError

func (ve ValidationErrors) Error() string {
	if len(ve) == 0 {
		return ""
	}
	if len(ve) == 1 {
		return ve[0].Error()
	}
	var msg string
	for i, err := range ve {
		if i > 0 {
			msg += "\n"
		}
		msg += "- " + err.Error()
	}
	return msg
}
