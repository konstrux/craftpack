// SPDX-FileCopyrightText: 2026 Marcin Kaim
// SPDX-License-Identifier: Apache-2.0

package fsutil

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var (
	// ErrPathTraversal is returned when a path resolves outside the permitted workspace directory.
	ErrPathTraversal = errors.New("path traversal detected: path references location outside workspace")
)

// AssertWithinWorkspace verifies that targetPath resides strictly within workspaceDir,
// defending against directory traversal tricks, parent directory references ('..'),
// absolute path injections, and symbolic links resolving outside the workspace.
// Returns the resolved, clean absolute path on success.
func AssertWithinWorkspace(workspaceDir string, targetPath string) (string, error) {
	if workspaceDir == "" {
		workspaceDir = "."
	}

	absWorkspace, err := filepath.Abs(workspaceDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve workspace path '%s': %w", workspaceDir, err)
	}

	// Resolve symlinks in workspace directory if it exists
	if evalWorkspace, err := filepath.EvalSymlinks(absWorkspace); err == nil {
		absWorkspace = evalWorkspace
	}
	absWorkspace = filepath.Clean(absWorkspace)

	var fullTarget string
	if filepath.IsAbs(targetPath) {
		fullTarget = filepath.Clean(targetPath)
	} else {
		// Clean the relative target before joining
		cleanRel := filepath.Clean(targetPath)
		if cleanRel == ".." || strings.HasPrefix(cleanRel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("%w: relative path '%s' attempts to traverse above root", ErrPathTraversal, targetPath)
		}
		fullTarget = filepath.Clean(filepath.Join(absWorkspace, cleanRel))
	}

	// Lexical containment check: fullTarget must equal absWorkspace or have absWorkspace + separator as prefix
	if fullTarget != absWorkspace && !strings.HasPrefix(fullTarget, absWorkspace+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: target path '%s' resolves to '%s' which is outside workspace '%s'", ErrPathTraversal, targetPath, fullTarget, absWorkspace)
	}

	// If the file or directory exists, check for symlink escapes
	if targetFi, err := os.Lstat(fullTarget); err == nil {
		if targetFi.Mode()&os.ModeSymlink != 0 {
			evalTarget, err := filepath.EvalSymlinks(fullTarget)
			if err != nil {
				return "", fmt.Errorf("failed to evaluate symlink '%s': %w", fullTarget, err)
			}
			evalTarget = filepath.Clean(evalTarget)
			if evalTarget != absWorkspace && !strings.HasPrefix(evalTarget, absWorkspace+string(filepath.Separator)) {
				return "", fmt.Errorf("%w: symlink '%s' points to '%s' outside workspace '%s'", ErrPathTraversal, fullTarget, evalTarget, absWorkspace)
			}
			return evalTarget, nil
		}
	}

	return fullTarget, nil
}

// CollectFiles traverses dirPath recursively, verifying that no paths or symlinks
// escape workspaceDir, and returns relative paths from dirPath sorted alphabetically.
func CollectFiles(dirPath string, workspaceDir string) ([]string, error) {
	absDir, err := AssertWithinWorkspace(workspaceDir, dirPath)
	if err != nil {
		return nil, err
	}

	var relPaths []string
	err = filepath.Walk(absDir, func(currentPath string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		// Don't include the root dir itself as a file entry
		if currentPath == absDir {
			return nil
		}

		// Verify that this path is within workspace (including evaluating symlinks)
		if _, err := AssertWithinWorkspace(workspaceDir, currentPath); err != nil {
			return err
		}

		rel, err := filepath.Rel(absDir, currentPath)
		if err != nil {
			return fmt.Errorf("failed to compute relative path for '%s': %w", currentPath, err)
		}

		relPaths = append(relPaths, rel)
		return nil
	})

	if err != nil {
		return nil, err
	}

	return relPaths, nil
}

// CopyFile copies a single file from src to dst, creating any required parent
// directories with 0755 permissions and applying normalized permissions to dst.
func CopyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file '%s': %w", src, err)
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat source file '%s': %w", src, err)
	}

	if err := os.MkdirAll(filepath.Dir(dst), DirMode); err != nil {
		return fmt.Errorf("failed to create destination directory for '%s': %w", dst, err)
	}

	dstMode := NormalizeFileMode(srcInfo.Mode())
	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, dstMode)
	if err != nil {
		return fmt.Errorf("failed to create destination file '%s': %w", dst, err)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("failed to copy data from '%s' to '%s': %w", src, dst, err)
	}

	// Preserve timestamps
	_ = os.Chtimes(dst, srcInfo.ModTime(), srcInfo.ModTime())

	return nil
}

// CopyDir recursively copies the contents of srcDir to dstDir, ensuring all
// source files reside safely within workspaceDir.
func CopyDir(srcDir, dstDir string, workspaceDir string) error {
	absSrc, err := AssertWithinWorkspace(workspaceDir, srcDir)
	if err != nil {
		return err
	}

	return filepath.Walk(absSrc, func(currentPath string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		rel, err := filepath.Rel(absSrc, currentPath)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		targetDst := filepath.Join(dstDir, rel)

		if info.IsDir() {
			return os.MkdirAll(targetDst, DirMode)
		}

		// Verify source path safety
		if _, err := AssertWithinWorkspace(workspaceDir, currentPath); err != nil {
			return err
		}

		return CopyFile(currentPath, targetDst)
	})
}
