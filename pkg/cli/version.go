// SPDX-FileCopyrightText: 2026 Marcin Kaim
// SPDX-License-Identifier: GPL-3.0-only

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

// Build metadata populated via -ldflags at compile time or resolved dynamically at runtime.
var (
	// Version holds the semantic version of the craftpack utility.
	Version = "unknown"
	// GitCommit holds the git commit SHA at build time.
	GitCommit = "none"
	// BuildDate holds the ISO-8601 timestamp of compilation.
	BuildDate = "unknown"
)

var resolveOnce sync.Once

func init() {
	resolveVersionMetadata()
}

// resolveVersionMetadata resolves missing build metadata using runtime/debug VCS info
// and dynamic Git queries, ensuring fallback values are populated without manual bumping.
func resolveVersionMetadata() {
	resolveOnce.Do(func() {
		doResolveVersionMetadata()
	})
}

func doResolveVersionMetadata() {
	// 1. Inspect Go runtime/debug BuildInfo (embedded by Go 1.18+ during go build)
	if info, ok := debug.ReadBuildInfo(); ok {
		if isUnsetVersion(Version) && info.Main.Version != "" && info.Main.Version != "(devel)" {
			Version = CleanVersion(info.Main.Version)
		}
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				if isUnsetCommit(GitCommit) && s.Value != "" {
					GitCommit = s.Value
				}
			case "vcs.time":
				if isUnsetDate(BuildDate) && s.Value != "" {
					BuildDate = s.Value
				}
			}
		}
	}

	// 2. Query Git for the nearest tag if Version is still unset
	if isUnsetVersion(Version) {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		if tagOut, err := exec.CommandContext(ctx, "git", "describe", "--tags", "--abbrev=0").Output(); err == nil {
			tag := strings.TrimSpace(string(tagOut))
			if tag != "" {
				Version = CleanVersion(tag)
			}
		}
		cancel()
	}

	// 3. Query Git for the HEAD commit hash if GitCommit is still unset
	if isUnsetCommit(GitCommit) {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		if revOut, err := exec.CommandContext(ctx, "git", "rev-parse", "HEAD").Output(); err == nil {
			rev := strings.TrimSpace(string(revOut))
			if rev != "" {
				GitCommit = rev
			}
		}
		cancel()
	}

	// 4. Query Git for the commit ISO timestamp if BuildDate is still unset
	if isUnsetDate(BuildDate) {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		if dateOut, err := exec.CommandContext(ctx, "git", "log", "-1", "--format=%cI").Output(); err == nil {
			dateStr := strings.TrimSpace(string(dateOut))
			if dateStr != "" {
				BuildDate = dateStr
			}
		}
		cancel()
	}

	// 5. Final fallback if outside a Git repository and built without ldflags
	if isUnsetVersion(Version) {
		Version = "0.0.0-dev"
	}
}

func isUnsetVersion(v string) bool {
	clean := strings.TrimSpace(v)
	return clean == "" || clean == "unknown" || clean == "none"
}

func isUnsetCommit(c string) bool {
	clean := strings.TrimSpace(c)
	return clean == "" || clean == "none" || clean == "unknown"
}

func isUnsetDate(d string) bool {
	clean := strings.TrimSpace(d)
	return clean == "" || clean == "unknown" || clean == "none"
}

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

// GetVersion returns the cleaned, resolved semantic version of the Craftpack utility.
func GetVersion() string {
	resolveVersionMetadata()
	return CleanVersion(Version)
}

// GetVersionInfo assembles runtime and compilation metadata into a VersionInfo struct.
func GetVersionInfo() VersionInfo {
	resolveVersionMetadata()
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
	resolveVersionMetadata()
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

	if _, err := fmt.Fprintf(w, "craftpack v%s\n", info.Version); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  Git commit:  %s\n", info.GitCommit); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  Build date:  %s\n", info.BuildDate); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  Go version:  %s\n", info.GoVersion); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "  Platform:    %s\n", info.Platform)
	return err
}
