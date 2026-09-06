// SPDX-FileCopyrightText: 2026 Marcin Kaim
// SPDX-License-Identifier: Apache-2.0

package generator

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"craftpack/pkg/fsutil"
	"craftpack/pkg/spec"
	"github.com/cpuguy83/go-md2man/v2/md2man"
)

var (
	// ErrZeroMarkupViolation is returned when Markdown source violates the Zero-Markup policy
	// by including YAML front-matter or formatting metadata directives.
	ErrZeroMarkupViolation = errors.New("zero-markup policy violation")

	// ErrInvalidSection is returned when a manual page section is outside the range 1-8.
	ErrInvalidSection = errors.New("invalid manual page section: must be an integer between 1 and 8")

	// ErrEmptySource is returned when a manual page source path is empty.
	ErrEmptySource = errors.New("manual page source path cannot be empty")
)

// ManPageMetadata encapsulates the derived attributes computed by the Metadata Inference Engine.
type ManPageMetadata struct {
	Title   string // Document title (e.g., "CRAFTPACK")
	Section int    // Manual section (1 to 8)
	Date    string // Compilation date formatted as YYYY-MM-DD
	Header  string // Localized category header (e.g., "User Commands Manual")
	Footer  string // Localized footer string (e.g., "craftpack 1.0.0")
}

// ManPageResult represents the compiled, compressed manual page artifact ready for staging.
type ManPageResult struct {
	SourcePath      string          // Original relative source path (e.g., "docs/craftpack.1.md")
	DestinationPath string          // Absolute destination path (e.g., "/usr/share/man/man1/craftpack.1.gz")
	Content         []byte          // Gzip-compressed roff bytes
	RoffContent     []byte          // Uncompressed roff bytes
	Mode            os.FileMode     // Standard file permissions (0644)
	Section         int             // Manual section number
	Command         string          // Associated command name
	Metadata        ManPageMetadata // Inferred metadata used for compilation
}

// RelPath returns the relative destination path suitable for tar archive serialization.
func (r *ManPageResult) RelPath() string {
	return strings.TrimPrefix(r.DestinationPath, "/")
}

// DefaultSectionHeader returns the standard UNIX manual category label for sections 1 through 8.
func DefaultSectionHeader(section int) string {
	switch section {
	case 1:
		return "User Commands Manual"
	case 2:
		return "System Calls Manual"
	case 3:
		return "Library Functions Manual"
	case 4:
		return "Special Files Manual"
	case 5:
		return "File Formats Manual"
	case 6:
		return "Games Manual"
	case 7:
		return "Miscellaneous Information Manual"
	case 8:
		return "System Administration Commands"
	default:
		return "User Commands Manual"
	}
}

// InferManPageMetadata applies the out-of-band Metadata Inference Engine rules to derive all required attributes.
func InferManPageMetadata(mp spec.ManPageConfig, cfg *spec.CraftpackConfig, packageVersion string, buildDate time.Time) ManPageMetadata {
	var meta ManPageMetadata

	// Section: 1 to 8 (default: 1)
	if mp.Section >= 1 && mp.Section <= 8 {
		meta.Section = mp.Section
	} else {
		meta.Section = 1
	}

	// Document Title: Inferred from uppercase manPage.Title or config Command or Name
	if strings.TrimSpace(mp.Title) != "" {
		meta.Title = strings.ToUpper(strings.TrimSpace(mp.Title))
	} else if cfg != nil && strings.TrimSpace(cfg.Command) != "" {
		meta.Title = strings.ToUpper(strings.TrimSpace(cfg.Command))
	} else if cfg != nil && strings.TrimSpace(cfg.Name) != "" {
		meta.Title = strings.ToUpper(strings.TrimSpace(cfg.Name))
	} else {
		meta.Title = "MANUAL"
	}

	// Compilation Date: Build date formatted as YYYY-MM-DD
	if buildDate.IsZero() {
		buildDate = time.Now().UTC()
	}
	meta.Date = buildDate.UTC().Format("2006-01-02")

	// Localized Header: manPage.Header or derived section label
	if strings.TrimSpace(mp.Header) != "" {
		meta.Header = strings.TrimSpace(mp.Header)
	} else {
		meta.Header = DefaultSectionHeader(meta.Section)
	}

	// Footer: manPage.Footer or fmt.Sprintf("%s %s", config.Name, packageVersion)
	if strings.TrimSpace(mp.Footer) != "" {
		meta.Footer = strings.TrimSpace(mp.Footer)
	} else if cfg != nil && strings.TrimSpace(cfg.Name) != "" && strings.TrimSpace(packageVersion) != "" {
		meta.Footer = fmt.Sprintf("%s %s", strings.TrimSpace(cfg.Name), strings.TrimSpace(packageVersion))
	} else if cfg != nil && strings.TrimSpace(cfg.Name) != "" {
		meta.Footer = strings.TrimSpace(cfg.Name)
	} else if strings.TrimSpace(packageVersion) != "" {
		meta.Footer = strings.TrimSpace(packageVersion)
	}

	return meta
}

// FormatTitleHeaderDirective formats the roff title header directive for md2man:
// % TITLE(SECTION) Footer | Header
func FormatTitleHeaderDirective(meta ManPageMetadata) string {
	titleSection := fmt.Sprintf("%s(%d)", meta.Title, meta.Section)

	if meta.Footer != "" && meta.Header != "" {
		return fmt.Sprintf("%% %s %s | %s\n\n", titleSection, meta.Footer, meta.Header)
	} else if meta.Footer != "" {
		return fmt.Sprintf("%% %s %s\n\n", titleSection, meta.Footer)
	} else if meta.Header != "" {
		return fmt.Sprintf("%% %s | %s\n\n", titleSection, meta.Header)
	}
	return fmt.Sprintf("%% %s\n\n", titleSection)
}

// ValidateZeroMarkup verifies that markdown source does not contain YAML front-matter ('---').
func ValidateZeroMarkup(markdown []byte) error {
	trimmed := strings.TrimSpace(string(markdown))
	if strings.HasPrefix(trimmed, "---") {
		return fmt.Errorf("%w: YAML front-matter ('---') detected", ErrZeroMarkupViolation)
	}
	return nil
}

// RenderRoff executes On-the-Fly Staging: checks Zero-Markup, prepends synthesized title directive,
// and compiles the composite markdown buffer to roff format using md2man.
func RenderRoff(markdown []byte, meta ManPageMetadata) ([]byte, error) {
	if err := ValidateZeroMarkup(markdown); err != nil {
		return nil, err
	}

	headerDirective := FormatTitleHeaderDirective(meta)
	var composite bytes.Buffer
	composite.WriteString(headerDirective)
	composite.Write(markdown)

	roff := md2man.Render(composite.Bytes())
	return roff, nil
}

// CompressRoff compresses rendered roff bytes using gzip with deterministic ModTime.
func CompressRoff(roff []byte, modTime time.Time) ([]byte, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if !modTime.IsZero() {
		gw.Header.ModTime = modTime.UTC().Truncate(time.Second)
	}
	gw.Header.OS = 255 // Unknown / non-specific OS for deterministic reproducibility

	if _, err := gw.Write(roff); err != nil {
		return nil, fmt.Errorf("failed to compress roff bytes: %w", err)
	}
	if err := gw.Close(); err != nil {
		return nil, fmt.Errorf("failed to close gzip writer: %w", err)
	}
	return buf.Bytes(), nil
}

// CompileManPage compiles raw Markdown bytes to roff and gzip compresses it in memory.
// Returns (compressedBytes, roffBytes, error).
func CompileManPage(markdown []byte, meta ManPageMetadata, modTime time.Time) ([]byte, []byte, error) {
	roff, err := RenderRoff(markdown, meta)
	if err != nil {
		return nil, nil, err
	}

	compressed, err := CompressRoff(roff, modTime)
	if err != nil {
		return nil, nil, err
	}

	return compressed, roff, nil
}

// SynthesizeManPage compiles a mapped markdown source file into a compressed manual page.
func SynthesizeManPage(mp spec.ManPageConfig, cfg *spec.CraftpackConfig, workspaceDir, packageVersion string, buildDate time.Time) (*ManPageResult, error) {
	if mp.Source == "" {
		return nil, ErrEmptySource
	}
	if mp.Section < 1 || mp.Section > 8 {
		return nil, fmt.Errorf("%w: %d", ErrInvalidSection, mp.Section)
	}

	// Boundary-checked resolution within workspace
	resolvedPath, err := fsutil.AssertWithinWorkspace(workspaceDir, mp.Source)
	if err != nil {
		return nil, fmt.Errorf("man page source '%s' boundary violation: %w", mp.Source, err)
	}

	markdownBytes, err := os.ReadFile(resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read man page source '%s': %w", mp.Source, err)
	}

	meta := InferManPageMetadata(mp, cfg, packageVersion, buildDate)

	compressed, roff, err := CompileManPage(markdownBytes, meta, buildDate)
	if err != nil {
		return nil, fmt.Errorf("failed to compile man page '%s': %w", mp.Source, err)
	}

	// Determine command name for output filename
	command := "app"
	if cfg != nil && strings.TrimSpace(cfg.Command) != "" {
		command = strings.TrimSpace(cfg.Command)
	} else if cfg != nil && strings.TrimSpace(cfg.Name) != "" {
		command = strings.TrimSpace(cfg.Name)
	}

	destPath := fmt.Sprintf("/usr/share/man/man%d/%s.%d.gz", meta.Section, command, meta.Section)

	return &ManPageResult{
		SourcePath:      mp.Source,
		DestinationPath: destPath,
		Content:         compressed,
		RoffContent:     roff,
		Mode:            fsutil.FileMode,
		Section:         meta.Section,
		Command:         command,
		Metadata:        meta,
	}, nil
}

// SynthesizeAllManPages synthesizes all man pages defined in cfg.ManPages.
func SynthesizeAllManPages(cfg *spec.CraftpackConfig, workspaceDir, packageVersion string, buildDate time.Time) ([]*ManPageResult, error) {
	if cfg == nil {
		return nil, errors.New("craftpack config cannot be nil")
	}
	var results []*ManPageResult
	for _, mp := range cfg.ManPages {
		res, err := SynthesizeManPage(mp, cfg, workspaceDir, packageVersion, buildDate)
		if err != nil {
			return nil, err
		}
		results = append(results, res)
	}
	return results, nil
}

// WriteManPage writes the synthesized man page result to destDir with 0644 permissions.
func WriteManPage(destDir string, res *ManPageResult) (string, error) {
	if res == nil {
		return "", errors.New("man page result cannot be nil")
	}
	cleanRel := strings.TrimPrefix(filepath.Clean(res.DestinationPath), "/")
	fullDest := filepath.Join(destDir, cleanRel)

	if err := os.MkdirAll(filepath.Dir(fullDest), fsutil.DirMode); err != nil {
		return "", fmt.Errorf("failed to create man page directory: %w", err)
	}

	if err := os.WriteFile(fullDest, res.Content, res.Mode); err != nil {
		return "", fmt.Errorf("failed to write man page file: %w", err)
	}

	return fullDest, nil
}
