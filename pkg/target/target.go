// SPDX-FileCopyrightText: 2026 Marcin Kaim
// SPDX-License-Identifier: GPL-3.0-only

package target

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"craftpack/pkg/spec"
)

var (
	// ErrTargetNotSupported is returned when an unsupported packaging target is requested.
	ErrTargetNotSupported = errors.New("packaging target not supported")

	registry   = make(map[string]func() TargetPackager)
	registryMu sync.RWMutex
)

// PackageOptions encapsulates all parameters required to assemble a target package.
type PackageOptions struct {
	Config         *spec.CraftpackConfig // Parsed and validated specification
	WorkspaceDir   string                // Workspace root path
	OutputDir      string                // Destination directory for compiled package (e.g. "./dist")
	PackageVersion string                // Normalized SemVer string (e.g. "1.0.0")
	Architecture   string                // Normalized target architecture (e.g. "amd64", "all")
	BuildDate      time.Time             // Deterministic build timestamp
	DataDir        string                // Optional staged filesystem payload directory
}

// PackageResult represents the compiled package artifact metadata.
type PackageResult struct {
	PackageFile string // Absolute path to the emitted package file
	Filename    string // File basename (e.g. "craftpack_1.0.0_amd64.deb")
	Size        int64  // Package size in bytes
	TargetType  string // Target format identifier (e.g. "deb")
}

// TargetPackager defines the standard contract for target packaging engines.
type TargetPackager interface {
	TargetName() string
	Build(ctx context.Context, opts PackageOptions) (*PackageResult, error)
}

// Register registers a target packager factory function under a target name (e.g. "deb").
func Register(name string, factory func() TargetPackager) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = factory
}

// Get retrieves a new instance of the TargetPackager for the given target name.
func Get(name string) (TargetPackager, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	factory, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("%w: '%s'", ErrTargetNotSupported, name)
	}
	return factory(), nil
}

// AvailableTargets returns a sorted list of all registered target names.
func AvailableTargets() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	targets := make([]string, 0, len(registry))
	for name := range registry {
		targets = append(targets, name)
	}
	sort.Strings(targets)
	return targets
}
