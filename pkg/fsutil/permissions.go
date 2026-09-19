// SPDX-FileCopyrightText: 2026 Marcin Kaim
// SPDX-License-Identifier: GPL-3.0-only

package fsutil

import (
	"archive/tar"
	"os"
)

const (
	// DirMode represents normalized directory permissions (0755).
	DirMode os.FileMode = 0755

	// ExecMode represents normalized executable file permissions (0755).
	ExecMode os.FileMode = 0755

	// FileMode represents normalized standard non-executable file permissions (0644).
	FileMode os.FileMode = 0644

	// DefaultOwnerUID is the deterministic root UID (0).
	DefaultOwnerUID = 0

	// DefaultOwnerGID is the deterministic root GID (0).
	DefaultOwnerGID = 0

	// DefaultOwnerName is the deterministic root owner name ("root").
	DefaultOwnerName = "root"

	// DefaultGroupName is the deterministic root group name ("root").
	DefaultGroupName = "root"
)

// IsExecutable returns true if any of the execute permission bits are set.
func IsExecutable(mode os.FileMode) bool {
	return mode&0111 != 0
}

// NormalizeFileMode returns the normalized permission bits for a given mode:
// 0755 for directories and executables, 0644 for regular files.
func NormalizeFileMode(mode os.FileMode) os.FileMode {
	if mode.IsDir() || IsExecutable(mode) {
		return ExecMode
	}
	return FileMode
}

// NormalizeTarHeader normalizes ownership to root:root (UID/GID 0) and
// resets permission bits to 0755 (dirs/executables) or 0644 (regular files),
// while preserving file modification timestamps for auditing and reproducibility.
func NormalizeTarHeader(hdr *tar.Header, fi os.FileInfo) {
	hdr.Uid = DefaultOwnerUID
	hdr.Gid = DefaultOwnerGID
	hdr.Uname = DefaultOwnerName
	hdr.Gname = DefaultGroupName

	if fi.IsDir() {
		hdr.Typeflag = tar.TypeDir
		hdr.Mode = int64(DirMode)
	} else {
		hdr.Typeflag = tar.TypeReg
		if IsExecutable(fi.Mode()) {
			hdr.Mode = int64(ExecMode)
		} else {
			hdr.Mode = int64(FileMode)
		}
	}

	hdr.ModTime = fi.ModTime()
}
