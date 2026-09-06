// SPDX-FileCopyrightText: 2026 Marcin Kaim
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"strings"
)

// Build metadata populated via -ldflags at compile time.
var (
	// Version holds the semantic version of the craftpack utility.
	Version = "1.0.0"
	// GitCommit holds the git commit SHA at build time.
	GitCommit = "none"
	// BuildDate holds the ISO-8601 timestamp of compilation.
	BuildDate = "unknown"
)

// VersionInfo encapsulates comprehensive build and environment metadata.
type VersionInfo struct {
	Version   string `json:"version"`
	GitCommit string `json:"git_commit"`
	BuildDate string `json:"build_date"`
	GoVersion string `json:"go_version"`
	Compiler  string `json:"compiler"`
	Platform  string `json:"platform"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

// CleanVersion returns the semantic version without leading 'v' or 'V'.
func CleanVersion(v string) string {
	clean := strings.TrimSpace(v)
	clean = strings.TrimPrefix(clean, "v")
	clean = strings.TrimPrefix(clean, "V")
	return strings.TrimSpace(clean)
}

// GetVersionInfo assembles runtime and compilation metadata into a VersionInfo struct.
func GetVersionInfo() VersionInfo {
	return VersionInfo{
		Version:   CleanVersion(Version),
		GitCommit: GitCommit,
		BuildDate: BuildDate,
		GoVersion: runtime.Version(),
		Compiler:  runtime.Compiler,
		Platform:  fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}
}

// PrintVersion writes the single-line version string (craftpack vX.Y.Z) to w.
func PrintVersion(w io.Writer) {
	fmt.Fprintf(w, "craftpack v%s\n", CleanVersion(Version))
}

// PrintVersionInfo emits comprehensive build and runtime environment metadata to w.
// If asJSON is true, formatted JSON is output; otherwise, human-readable key-value lines are emitted.
func PrintVersionInfo(w io.Writer, asJSON bool) error {
	info := GetVersionInfo()
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(info)
	}

	fmt.Fprintf(w, "craftpack v%s\n", info.Version)
	fmt.Fprintf(w, "  Git commit:  %s\n", info.GitCommit)
	fmt.Fprintf(w, "  Build date:  %s\n", info.BuildDate)
	fmt.Fprintf(w, "  Go version:  %s\n", info.GoVersion)
	fmt.Fprintf(w, "  Platform:    %s\n", info.Platform)
	return nil
}
