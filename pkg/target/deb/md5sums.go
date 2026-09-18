// SPDX-FileCopyrightText: 2026 Marcin Kaim
// SPDX-License-Identifier: Apache-2.0

package deb

import (
	"bytes"
	"crypto/md5"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"craftpack/pkg/fsutil"
)

// MD5Entry stores an individual file path and its computed MD5 cryptographic digest.
type MD5Entry struct {
	Path string // Clean path without leading slash or ./ (e.g., "usr/bin/craftpack")
	Hash string // 32-character hexadecimal MD5 hash
}

// CleanMD5Path ensures the path has no leading slash or ./ and uses forward slash delimiters.
func CleanMD5Path(p string) string {
	clean := filepath.ToSlash(filepath.Clean(p))
	clean = strings.TrimPrefix(clean, "./")
	clean = strings.TrimPrefix(clean, "/")
	return clean
}

// GenerateMD5SumsFromEntries generates alphabetically sorted DEBIAN/md5sums content from TarEntry items.
func GenerateMD5SumsFromEntries(entries []fsutil.TarEntry) ([]byte, error) {
	var md5Entries []MD5Entry
	seen := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir {
			continue
		}
		cleanPath := CleanMD5Path(entry.Path)
		if cleanPath == "" || cleanPath == "." || seen[cleanPath] {
			continue
		}
		seen[cleanPath] = true

		var fileData []byte
		if len(entry.Data) > 0 {
			fileData = entry.Data
		} else if entry.SourcePath != "" {
			data, err := os.ReadFile(entry.SourcePath)
			if err != nil {
				return nil, fmt.Errorf("failed reading source file '%s' for md5 calculation: %w", entry.SourcePath, err)
			}
			fileData = data
		} else {
			fileData = []byte{}
		}

		hash := md5.Sum(fileData)
		md5Entries = append(md5Entries, MD5Entry{
			Path: cleanPath,
			Hash: fmt.Sprintf("%x", hash),
		})
	}

	sort.Slice(md5Entries, func(i, j int) bool {
		return md5Entries[i].Path < md5Entries[j].Path
	})

	var buf bytes.Buffer
	for _, e := range md5Entries {
		buf.WriteString(fmt.Sprintf("%s  %s\n", e.Hash, e.Path))
	}
	return buf.Bytes(), nil
}

// GenerateMD5SumsFromDir walks dataDir, computes MD5 hashes for all regular files, and returns sorted DEBIAN/md5sums.
func GenerateMD5SumsFromDir(dataDir string) ([]byte, error) {
	absDir, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve data directory '%s': %w", dataDir, err)
	}

	fi, err := os.Stat(absDir)
	if err != nil {
		return nil, fmt.Errorf("failed to stat data directory '%s': %w", absDir, err)
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("data path '%s' is not a directory", absDir)
	}

	var md5Entries []MD5Entry
	err = filepath.Walk(absDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(absDir, path)
		if err != nil {
			return err
		}

		cleanPath := CleanMD5Path(relPath)
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed reading file '%s' for md5 calculation: %w", path, err)
		}

		hash := md5.Sum(data)
		md5Entries = append(md5Entries, MD5Entry{
			Path: cleanPath,
			Hash: fmt.Sprintf("%x", hash),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed crawling data directory '%s': %w", dataDir, err)
	}

	sort.Slice(md5Entries, func(i, j int) bool {
		return md5Entries[i].Path < md5Entries[j].Path
	})

	var buf bytes.Buffer
	for _, e := range md5Entries {
		buf.WriteString(fmt.Sprintf("%s  %s\n", e.Hash, e.Path))
	}
	return buf.Bytes(), nil
}
