// SPDX-FileCopyrightText: 2026 Marcin Kaim
// SPDX-License-Identifier: Apache-2.0

package deb

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"craftpack/pkg/spec"
)

// GenerateConffiles lists all /etc/<app_id>/<file> paths mapped from defaultConfig,
// sorted alphabetically, with a trailing newline.
// Returns nil if defaultConfig is empty.
func GenerateConffiles(appID string, defaultConfig map[string]string) ([]byte, error) {
	if len(defaultConfig) == 0 {
		return nil, nil
	}
	trimmedID := strings.TrimSpace(appID)
	if trimmedID == "" {
		return nil, errors.New("app ID cannot be empty for conffiles generation")
	}

	var paths []string
	for _, targetFile := range defaultConfig {
		cleanTarget := filepath.Clean(strings.TrimSpace(targetFile))
		if cleanTarget == "" || cleanTarget == "." || strings.HasPrefix(cleanTarget, "..") || filepath.IsAbs(cleanTarget) {
			return nil, fmt.Errorf("invalid default_config target path: '%s'", targetFile)
		}
		paths = append(paths, fmt.Sprintf("/etc/%s/%s", trimmedID, filepath.ToSlash(cleanTarget)))
	}

	sort.Strings(paths)

	var buf bytes.Buffer
	for _, p := range paths {
		buf.WriteString(p)
		buf.WriteByte('\n')
	}

	return buf.Bytes(), nil
}

// GenerateConffilesFromConfig generates DEBIAN/conffiles content for cfg.
func GenerateConffilesFromConfig(cfg *spec.CraftpackConfig) ([]byte, error) {
	if cfg == nil {
		return nil, errors.New("craftpack config cannot be nil")
	}
	return GenerateConffiles(cfg.Name, cfg.DefaultConfig)
}
