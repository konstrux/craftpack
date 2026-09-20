// SPDX-FileCopyrightText: 2026 Marcin Kaim
// SPDX-License-Identifier: GPL-3.0-only

package builder

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"craftpack/pkg/fsutil"
	"craftpack/pkg/generator"
	"craftpack/pkg/spec"
	"craftpack/pkg/target"
	"craftpack/pkg/target/deb"
)

// Orchestrator executes the 7-stage build lifecycle pipeline of Craftpack.
type Orchestrator struct{}

// NewOrchestrator creates a new Orchestrator instance.
func NewOrchestrator() *Orchestrator {
	return &Orchestrator{}
}

// BuildWithOptions instantiates a BuildContext from opts and runs the build lifecycle.
func (o *Orchestrator) BuildWithOptions(ctx context.Context, opts BuildOptions) (*BuildResult, error) {
	bCtx, err := NewBuildContext(opts)
	if err != nil {
		return nil, err
	}
	return o.Build(ctx, bCtx)
}

// Build coordinates and executes the 7 sequential stages of package compilation.
func (o *Orchestrator) Build(ctx context.Context, bCtx *BuildContext) (*BuildResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if bCtx == nil {
		return nil, errors.New("build context cannot be nil")
	}

	// Guarantee cleanup of ephemeral staging workspace upon function completion or error
	defer func() {
		if !bCtx.Options.KeepStagingDir {
			_ = bCtx.Cleanup()
		}
	}()

	// -------------------------------------------------------------------------
	// Stage 1: CLI Ingestion & Strict Schema Validation
	// -------------------------------------------------------------------------
	bCtx.NotifyStage(Stage1CLIValidation, "Validating parameters and specification schema")
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	targetName := strings.ToLower(strings.TrimSpace(bCtx.Options.Target))
	if targetName == "" {
		return nil, errors.New("packaging target is mandatory (e.g. --target deb)")
	}

	packager, err := target.Get(targetName)
	if err != nil {
		return nil, fmt.Errorf("stage 1: %w", err)
	}
	bCtx.TargetPackager = packager

	rawVersion := strings.TrimSpace(bCtx.Options.PackageVersion)
	if rawVersion == "" {
		return nil, errors.New("package version is mandatory (e.g. --package-version 1.0.0)")
	}
	normalizedVersion := deb.NormalizeVersion(rawVersion)
	if normalizedVersion == "" {
		return nil, errors.New("package version cannot be empty after normalization")
	}
	bCtx.NormalizedVersion = normalizedVersion

	workspaceDir := bCtx.Options.WorkspaceDir
	if workspaceDir == "" {
		workspaceDir = "."
	}
	absWorkspace, err := filepath.Abs(workspaceDir)
	if err != nil {
		return nil, fmt.Errorf("stage 1: failed to resolve workspace path '%s': %w", workspaceDir, err)
	}
	fi, err := os.Stat(absWorkspace)
	if err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("stage 1: workspace directory '%s' does not exist", absWorkspace)
	}
	bCtx.Options.WorkspaceDir = absWorkspace

	var cfg *spec.CraftpackConfig
	if bCtx.Config != nil {
		cfg = bCtx.Config
	} else {
		specPath := bCtx.Options.SpecPath
		if specPath == "" {
			specPath = filepath.Join(absWorkspace, "craftpack.yml")
		} else if !filepath.IsAbs(specPath) {
			specPath = filepath.Join(absWorkspace, specPath)
		}
		if _, err := os.Stat(specPath); err != nil {
			return nil, fmt.Errorf("stage 1: specification file '%s' not found: %w", specPath, err)
		}

		parseRes, err := spec.ParseFile(specPath, spec.ParseOptions{
			WorkspaceDir:   absWorkspace,
			CheckWorkspace: true,
			Strict:         bCtx.Options.Strict,
		})
		if err != nil {
			return nil, fmt.Errorf("stage 1: specification parsing failed: %w", err)
		}
		for _, w := range parseRes.Warnings {
			bCtx.NotifyWarning(w)
		}
		cfg = parseRes.Config
		bCtx.Config = cfg
	}

	bCtx.NormalizedArch = deb.NormalizeArchitecture(bCtx.Options.Architecture)
	if bCtx.NormalizedArch == "" {
		bCtx.NormalizedArch = deb.DefaultHostArchitecture()
	}

	if bCtx.Options.BuildDate.IsZero() {
		bCtx.BuildDate = time.Now().UTC().Truncate(time.Second)
	} else {
		bCtx.BuildDate = bCtx.Options.BuildDate.UTC().Truncate(time.Second)
	}

	// -------------------------------------------------------------------------
	// Stage 2: Staging Area Setup & Payload Crawling
	// -------------------------------------------------------------------------
	bCtx.NotifyStage(Stage2StagingPayload, "Allocating staging workspace and crawling payload")
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if _, err := bCtx.EnsureStagingDir(); err != nil {
		return nil, fmt.Errorf("stage 2: %w", err)
	}

	dataDir := bCtx.DataDir()
	if err := os.MkdirAll(dataDir, fsutil.DirMode); err != nil {
		return nil, fmt.Errorf("stage 2: failed to create staging data directory: %w", err)
	}

	wrapMode := requiresWrapper(cfg, targetName)
	cleanEntrypoint := filepath.ToSlash(filepath.Clean(cfg.Entrypoint))

	if cfg.PayloadDir != "" {
		payloadSrc, err := fsutil.AssertWithinWorkspace(absWorkspace, cfg.PayloadDir)
		if err != nil {
			return nil, fmt.Errorf("stage 2: payload boundary error: %w", err)
		}

		destBase := filepath.Join(dataDir, "usr", "lib", cfg.Name)
		if wrapMode {
			if err := os.MkdirAll(destBase, fsutil.DirMode); err != nil {
				return nil, fmt.Errorf("stage 2: failed to create payload target directory: %w", err)
			}
		}

		err = filepath.Walk(payloadSrc, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if path == payloadSrc {
				return nil
			}
			rel, err := filepath.Rel(payloadSrc, path)
			if err != nil {
				return err
			}

			if _, err := fsutil.AssertWithinWorkspace(payloadSrc, path); err != nil {
				return fmt.Errorf("payload file '%s' escaped boundary: %w", path, err)
			}

			cleanRel := filepath.ToSlash(filepath.Clean(rel))

			// Direct binary placement mode (wrapper=false)
			if !wrapMode && cleanRel == cleanEntrypoint {
				if info.IsDir() {
					return nil
				}
				data, err := os.ReadFile(path)
				if err != nil {
					return fmt.Errorf("failed reading payload entrypoint '%s': %w", path, err)
				}
				cmdName := cfg.Command
				if cmdName == "" {
					cmdName = cfg.Name
				}
				destPath := filepath.Join(dataDir, "usr", "bin", cmdName)
				if err := os.MkdirAll(filepath.Dir(destPath), fsutil.DirMode); err != nil {
					return err
				}
				if err := os.WriteFile(destPath, data, fsutil.ExecMode); err != nil {
					return fmt.Errorf("failed writing direct binary to staging '%s': %w", destPath, err)
				}
				stagedRel := filepath.ToSlash(filepath.Join("usr", "bin", cmdName))
				bCtx.StagedFiles = append(bCtx.StagedFiles, stagedRel)
				return nil
			}

			// If directory is an ancestor of the entrypoint in direct binary mode,
			// skip creating it under destBase to avoid creating empty parent directories.
			if !wrapMode && info.IsDir() {
				if strings.HasPrefix(cleanEntrypoint, cleanRel+"/") || cleanRel == cleanEntrypoint {
					return nil
				}
			}

			destPath := filepath.Join(destBase, rel)
			if info.IsDir() {
				return os.MkdirAll(destPath, fsutil.DirMode)
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("failed reading payload file '%s': %w", path, err)
			}

			mode := fsutil.FileMode
			if fsutil.IsExecutable(info.Mode()) || cleanRel == cleanEntrypoint {
				mode = fsutil.ExecMode
			}

			if err := os.MkdirAll(filepath.Dir(destPath), fsutil.DirMode); err != nil {
				return err
			}

			if err := os.WriteFile(destPath, data, mode); err != nil {
				return fmt.Errorf("failed writing payload file to staging '%s': %w", destPath, err)
			}

			stagedRel := filepath.ToSlash(filepath.Join("usr", "lib", cfg.Name, rel))
			bCtx.StagedFiles = append(bCtx.StagedFiles, stagedRel)
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("stage 2: payload crawling failed: %w", err)
		}
	}

	// Stage templates into /usr/share/<name>/templates/
	if cfg.TemplatesDir != "" {
		templatesSrc, err := fsutil.AssertWithinWorkspace(absWorkspace, cfg.TemplatesDir)
		if err != nil {
			return nil, fmt.Errorf("stage 2: templates boundary error: %w", err)
		}
		info, err := os.Stat(templatesSrc)
		if err != nil {
			return nil, fmt.Errorf("stage 2: failed stating templates directory '%s': %w", templatesSrc, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("stage 2: templates_dir '%s' is not a directory", cfg.TemplatesDir)
		}

		destTemplatesBase := filepath.Join(dataDir, "usr", "share", cfg.Name, "templates")
		if err := os.MkdirAll(destTemplatesBase, fsutil.DirMode); err != nil {
			return nil, fmt.Errorf("stage 2: failed to create templates target directory: %w", err)
		}

		err = filepath.Walk(templatesSrc, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if path == templatesSrc {
				return nil
			}
			rel, err := filepath.Rel(templatesSrc, path)
			if err != nil {
				return err
			}

			if _, err := fsutil.AssertWithinWorkspace(templatesSrc, path); err != nil {
				return fmt.Errorf("template file '%s' escaped boundary: %w", path, err)
			}

			destPath := filepath.Join(destTemplatesBase, rel)
			if info.IsDir() {
				return os.MkdirAll(destPath, fsutil.DirMode)
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("failed reading template file '%s': %w", path, err)
			}

			if err := os.MkdirAll(filepath.Dir(destPath), fsutil.DirMode); err != nil {
				return err
			}

			if err := os.WriteFile(destPath, data, fsutil.FileMode); err != nil {
				return fmt.Errorf("failed writing template file to staging '%s': %w", destPath, err)
			}

			stagedRel := filepath.ToSlash(filepath.Join("usr", "share", cfg.Name, "templates", rel))
			bCtx.StagedFiles = append(bCtx.StagedFiles, stagedRel)
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("stage 2: templates crawling failed: %w", err)
		}
	}

	// -------------------------------------------------------------------------
	// Stage 3: Proxy Launcher Synthesis
	// -------------------------------------------------------------------------
	if wrapMode {
		bCtx.NotifyStage(Stage3Launcher, "Synthesizing proxy launcher script")
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if cfg.Entrypoint != "" && cfg.Command != "" {
			launcherRes, err := generator.SynthesizeLauncherFromConfig(cfg)
			if err != nil {
				return nil, fmt.Errorf("stage 3: launcher synthesis failed: %w", err)
			}

			destPath := filepath.Join(dataDir, launcherRes.RelPath())
			if err := os.MkdirAll(filepath.Dir(destPath), fsutil.DirMode); err != nil {
				return nil, fmt.Errorf("stage 3: failed to create launcher directory: %w", err)
			}

			if err := os.WriteFile(destPath, launcherRes.Content, launcherRes.Mode); err != nil {
				return nil, fmt.Errorf("stage 3: failed writing launcher to '%s': %w", destPath, err)
			}
			bCtx.StagedFiles = append(bCtx.StagedFiles, launcherRes.RelPath())
		}
	} else {
		bCtx.NotifyStage(Stage3Launcher, "Direct binary placement enabled (wrapper=false); skipping proxy launcher synthesis")
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}

	// -------------------------------------------------------------------------
	// Stage 4: Documentation Staging & On-the-Fly Man Page Compression
	// -------------------------------------------------------------------------
	bCtx.NotifyStage(Stage4Documentation, "Compiling and staging compressed manual pages")
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if len(cfg.ManPages) > 0 {
		manResults, err := generator.SynthesizeAllManPages(cfg, absWorkspace, bCtx.NormalizedVersion, bCtx.BuildDate)
		if err != nil {
			return nil, fmt.Errorf("stage 4: documentation synthesis failed: %w", err)
		}

		for _, mr := range manResults {
			destPath := filepath.Join(dataDir, mr.RelPath())
			if err := os.MkdirAll(filepath.Dir(destPath), fsutil.DirMode); err != nil {
				return nil, fmt.Errorf("stage 4: failed to create man page directory: %w", err)
			}
			if err := os.WriteFile(destPath, mr.Content, mr.Mode); err != nil {
				return nil, fmt.Errorf("stage 4: failed writing man page to '%s': %w", destPath, err)
			}
			bCtx.StagedFiles = append(bCtx.StagedFiles, mr.RelPath())
		}
	}

	// -------------------------------------------------------------------------
	// Stage 5: Target Metadata Synthesis & Conffiles Staging
	// -------------------------------------------------------------------------
	bCtx.NotifyStage(Stage5TargetMetadata, "Synthesizing target metadata and conffiles")
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if len(cfg.DefaultConfig) > 0 {
		var confKeys []string
		for src := range cfg.DefaultConfig {
			confKeys = append(confKeys, src)
		}
		sort.Strings(confKeys)

		for _, src := range confKeys {
			dst := cfg.DefaultConfig[src]
			cleanDst := filepath.Clean(strings.TrimSpace(dst))
			if cleanDst == "" || cleanDst == "." || strings.HasPrefix(cleanDst, "..") || filepath.IsAbs(cleanDst) {
				return nil, fmt.Errorf("stage 5: invalid default_config destination '%s': must be a clean relative path", dst)
			}

			resolvedSrc, err := fsutil.AssertWithinWorkspace(absWorkspace, src)
			if err != nil {
				return nil, fmt.Errorf("stage 5: default_config boundary error: %w", err)
			}

			data, err := os.ReadFile(resolvedSrc)
			if err != nil {
				return nil, fmt.Errorf("stage 5: failed reading default_config '%s': %w", src, err)
			}

			destRel := filepath.ToSlash(filepath.Join("etc", cfg.Name, cleanDst))
			destPath := filepath.Join(dataDir, destRel)
			if err := os.MkdirAll(filepath.Dir(destPath), fsutil.DirMode); err != nil {
				return nil, fmt.Errorf("stage 5: failed to create conffiles directory: %w", err)
			}
			if err := os.WriteFile(destPath, data, fsutil.FileMode); err != nil {
				return nil, fmt.Errorf("stage 5: failed writing conffile '%s': %w", destPath, err)
			}
			bCtx.StagedFiles = append(bCtx.StagedFiles, destRel)
		}
	}

	// Perform dry-run target metadata validation
	if bCtx.Options.DryRun && targetName == "deb" {
		if _, err := deb.GenerateControlFromConfig(cfg, bCtx.NormalizedVersion, bCtx.NormalizedArch); err != nil {
			return nil, fmt.Errorf("stage 5: deb control validation failed: %w", err)
		}
		if _, err := deb.GenerateConffilesFromConfig(cfg); err != nil {
			return nil, fmt.Errorf("stage 5: deb conffiles validation failed: %w", err)
		}
		if _, err := deb.GenerateMaintainerScripts(cfg, absWorkspace); err != nil {
			return nil, fmt.Errorf("stage 5: deb maintainer scripts validation failed: %w", err)
		}
	}

	// -------------------------------------------------------------------------
	// Stage 6: Archive Compilation
	// -------------------------------------------------------------------------
	bCtx.NotifyStage(Stage6Archive, "Compiling package container archive")
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	packageFilename := fmt.Sprintf("%s_%s_%s.%s", cfg.Name, bCtx.NormalizedVersion, bCtx.NormalizedArch, targetName)

	var pkgResult *target.PackageResult
	if !bCtx.Options.DryRun {
		outputDir := bCtx.Options.OutputDir
		if strings.TrimSpace(outputDir) == "" {
			outputDir = "./dist"
		}
		absOutputDir, err := filepath.Abs(outputDir)
		if err != nil {
			return nil, fmt.Errorf("stage 6: failed to resolve output directory '%s': %w", outputDir, err)
		}
		if err := os.MkdirAll(absOutputDir, fsutil.DirMode); err != nil {
			return nil, fmt.Errorf("stage 6: failed to create output directory '%s': %w", absOutputDir, err)
		}

		pkgOpts := target.PackageOptions{
			Config:         cfg,
			WorkspaceDir:   absWorkspace,
			OutputDir:      absOutputDir,
			PackageVersion: bCtx.NormalizedVersion,
			Architecture:   bCtx.NormalizedArch,
			BuildDate:      bCtx.BuildDate,
			DataDir:        dataDir,
		}

		res, err := bCtx.TargetPackager.Build(ctx, pkgOpts)
		if err != nil {
			return nil, fmt.Errorf("stage 6: package build failed: %w", err)
		}
		if res == nil || strings.TrimSpace(res.PackageFile) == "" {
			return nil, errors.New("stage 6: packager returned nil result or empty package file")
		}
		pkgResult = res
	}

	// -------------------------------------------------------------------------
	// Stage 7: Release Manifest Generation & Staging Cleanup
	// -------------------------------------------------------------------------
	bCtx.NotifyStage(Stage7ManifestCleanup, "Generating SHA-256 release manifest and purging staging workspace")
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if bCtx.Options.DryRun {
		_ = bCtx.Cleanup()
		return &BuildResult{
			Success:      true,
			PackageFile:  "",
			PackageName:  cfg.Name,
			Filename:     packageFilename,
			Version:      bCtx.NormalizedVersion,
			Architecture: bCtx.NormalizedArch,
			Target:       targetName,
			SHA256:       "",
			SizeBytes:    0,
			DryRun:       true,
			BuildDate:    bCtx.BuildDate,
			ManifestPath: "",
			StagedFiles:  bCtx.StagedFiles,
			Warnings:     bCtx.Warnings,
		}, nil
	}

	hash, size, err := ComputeSHA256(pkgResult.PackageFile)
	if err != nil {
		return nil, fmt.Errorf("stage 7: sha256 checksum calculation failed: %w", err)
	}

	manifestPath := filepath.Join(filepath.Dir(pkgResult.PackageFile), ManifestFileName)
	if err := WriteOrUpdateManifest(manifestPath, hash, pkgResult.Filename, size); err != nil {
		return nil, fmt.Errorf("stage 7: manifest update failed: %w", err)
	}

	_ = bCtx.Cleanup()

	return &BuildResult{
		Success:      true,
		PackageFile:  pkgResult.PackageFile,
		PackageName:  cfg.Name,
		Filename:     pkgResult.Filename,
		Version:      bCtx.NormalizedVersion,
		Architecture: bCtx.NormalizedArch,
		Target:       targetName,
		SHA256:       hash,
		SizeBytes:    size,
		DryRun:       false,
		BuildDate:    bCtx.BuildDate,
		ManifestPath: manifestPath,
		StagedFiles:  bCtx.StagedFiles,
		Warnings:     bCtx.Warnings,
	}, nil
}

// requiresWrapper returns true if the target configuration explicitly requests
// generating a proxy launcher wrapper and private payload isolation directory.
// Defaults to false (direct binary placement in /usr/bin/<command>).
func requiresWrapper(cfg *spec.CraftpackConfig, targetName string) bool {
	if cfg == nil || cfg.Targets.Deb == nil {
		return false
	}
	if strings.EqualFold(targetName, "deb") {
		return cfg.Targets.Deb.Wrapper
	}
	return false
}
