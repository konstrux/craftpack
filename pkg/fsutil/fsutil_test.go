// SPDX-FileCopyrightText: 2026 Marcin Kaim
// SPDX-License-Identifier: Apache-2.0

package fsutil

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAssertWithinWorkspace_Valid(t *testing.T) {
	tempDir := t.TempDir()

	subDir := filepath.Join(tempDir, "sub", "dir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	validRel := filepath.Join("sub", "dir", "file.txt")
	resolved, err := AssertWithinWorkspace(tempDir, validRel)
	if err != nil {
		t.Fatalf("expected valid path, got error: %v", err)
	}
	expected := filepath.Join(tempDir, "sub", "dir", "file.txt")
	if resolved != expected {
		t.Errorf("expected '%s', got '%s'", expected, resolved)
	}

	// Valid absolute path
	absResolved, err := AssertWithinWorkspace(tempDir, subDir)
	if err != nil {
		t.Fatalf("expected valid absolute path, got error: %v", err)
	}
	if absResolved != subDir {
		t.Errorf("expected '%s', got '%s'", subDir, absResolved)
	}
}

func TestAssertWithinWorkspace_Traversals(t *testing.T) {
	tempDir := t.TempDir()

	tests := []struct {
		name       string
		targetPath string
	}{
		{"parent relative", "../escape.txt"},
		{"nested parent relative", "sub/../../escape.txt"},
		{"deep parent relative", "../../../etc/passwd"},
		{"absolute outside", "/etc/passwd"},
		{"root dir", "/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := AssertWithinWorkspace(tempDir, tt.targetPath)
			if err == nil {
				t.Errorf("expected traversal error for '%s', got nil", tt.targetPath)
			}
			if !errors.Is(err, ErrPathTraversal) {
				t.Errorf("expected ErrPathTraversal, got: %v", err)
			}
		})
	}
}

func TestAssertWithinWorkspace_SymlinkEscape(t *testing.T) {
	tempDir := t.TempDir()
	outsideDir := t.TempDir()

	outsideFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a symlink inside tempDir pointing to outsideDir/secret.txt
	maliciousSymlink := filepath.Join(tempDir, "link-to-outside")
	if err := os.Symlink(outsideFile, maliciousSymlink); err != nil {
		t.Skip("Symlink creation not supported on this platform")
	}

	_, err := AssertWithinWorkspace(tempDir, "link-to-outside")
	if err == nil {
		t.Fatal("expected error on symlink escaping workspace, got nil")
	}
	if !errors.Is(err, ErrPathTraversal) {
		t.Errorf("expected ErrPathTraversal, got: %v", err)
	}

	// Create a safe internal symlink
	insideFile := filepath.Join(tempDir, "inside.txt")
	if err := os.WriteFile(insideFile, []byte("safe"), 0644); err != nil {
		t.Fatal(err)
	}
	safeSymlink := filepath.Join(tempDir, "link-to-inside")
	if err := os.Symlink(insideFile, safeSymlink); err == nil {
		resolved, err := AssertWithinWorkspace(tempDir, "link-to-inside")
		if err != nil {
			t.Fatalf("expected safe symlink to pass, got: %v", err)
		}
		if resolved != insideFile {
			t.Errorf("expected '%s', got '%s'", insideFile, resolved)
		}
	}
}

func TestPermissions_Normalization(t *testing.T) {
	if NormalizeFileMode(os.ModeDir|0700) != DirMode {
		t.Errorf("expected DirMode 0755, got %v", NormalizeFileMode(os.ModeDir|0700))
	}
	if NormalizeFileMode(0777) != ExecMode {
		t.Errorf("expected ExecMode 0755, got %v", NormalizeFileMode(0777))
	}
	if NormalizeFileMode(0755) != ExecMode {
		t.Errorf("expected ExecMode 0755, got %v", NormalizeFileMode(0755))
	}
	if NormalizeFileMode(0644) != FileMode {
		t.Errorf("expected FileMode 0644, got %v", NormalizeFileMode(0644))
	}
	if NormalizeFileMode(0600) != FileMode {
		t.Errorf("expected FileMode 0644, got %v", NormalizeFileMode(0600))
	}
}

func TestPermissions_NormalizeTarHeader(t *testing.T) {
	tempDir := t.TempDir()
	binPath := filepath.Join(tempDir, "mybin")
	if err := os.WriteFile(binPath, []byte("binary"), 0755); err != nil {
		t.Fatal(err)
	}
	fiBin, _ := os.Stat(binPath)

	hdrBin := &tar.Header{Name: "usr/bin/mybin"}
	NormalizeTarHeader(hdrBin, fiBin)

	if hdrBin.Uid != 0 || hdrBin.Gid != 0 || hdrBin.Uname != "root" || hdrBin.Gname != "root" {
		t.Errorf("expected root:root ownership, got %d:%d %s:%s", hdrBin.Uid, hdrBin.Gid, hdrBin.Uname, hdrBin.Gname)
	}
	if hdrBin.Mode != int64(ExecMode) {
		t.Errorf("expected mode 0755 for executable, got %o", hdrBin.Mode)
	}

	docPath := filepath.Join(tempDir, "doc.txt")
	if err := os.WriteFile(docPath, []byte("doc"), 0644); err != nil {
		t.Fatal(err)
	}
	fiDoc, _ := os.Stat(docPath)

	hdrDoc := &tar.Header{Name: "usr/share/doc.txt"}
	NormalizeTarHeader(hdrDoc, fiDoc)
	if hdrDoc.Mode != int64(FileMode) {
		t.Errorf("expected mode 0644 for file, got %o", hdrDoc.Mode)
	}
}

func TestArchiveDirToTarGz_DeterministicSorting(t *testing.T) {
	tempDir := t.TempDir()

	// Create files out of alphabetical order
	files := []string{"z-file.txt", "a-file.txt", "m-dir/sub.txt", "b-file.txt"}
	for _, f := range files {
		full := filepath.Join(tempDir, f)
		_ = os.MkdirAll(filepath.Dir(full), 0755)
		_ = os.WriteFile(full, []byte("content of "+f), 0644)
	}

	var buf bytes.Buffer
	if err := ArchiveDirToTarGz(tempDir, &buf); err != nil {
		t.Fatalf("failed to archive directory: %v", err)
	}

	headers, err := ReadTarGzHeaders(buf.Bytes())
	if err != nil {
		t.Fatalf("failed to read tar.gz headers: %v", err)
	}

	if len(headers) < 5 {
		t.Fatalf("expected at least 5 entries, got %d", len(headers))
	}

	// Check that headers are sorted alphabetically
	for i := 0; i < len(headers)-1; i++ {
		if headers[i].Name >= headers[i+1].Name {
			t.Errorf("headers not sorted: '%s' comes before '%s'", headers[i].Name, headers[i+1].Name)
		}
	}

	// Verify all headers normalized to UID/GID 0
	for _, h := range headers {
		if h.Uid != 0 || h.Gid != 0 {
			t.Errorf("header '%s' has non-zero UID/GID: %d/%d", h.Name, h.Uid, h.Gid)
		}
	}
}

func TestArchiveEntriesToTarGz(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	entries := []TarEntry{
		{Path: "usr/bin/tool", Mode: 0755, ModTime: now, Data: []byte("#!/bin/sh\necho hi")},
		{Path: "etc/app/conf", Mode: 0644, ModTime: now, Data: []byte("k=v")},
		{Path: "usr/share/man", Mode: 0755, ModTime: now, IsDir: true},
	}

	var buf bytes.Buffer
	if err := ArchiveEntriesToTarGz(entries, &buf); err != nil {
		t.Fatalf("ArchiveEntriesToTarGz failed: %v", err)
	}

	headers, err := ReadTarGzHeaders(buf.Bytes())
	if err != nil {
		t.Fatalf("ReadTarGzHeaders failed: %v", err)
	}

	expectedOrder := []string{"etc/app/conf", "usr/bin/tool", "usr/share/man/"}
	if len(headers) != len(expectedOrder) {
		t.Fatalf("expected %d headers, got %d", len(expectedOrder), len(headers))
	}

	for i, h := range headers {
		if h.Name != expectedOrder[i] {
			t.Errorf("index %d: expected header '%s', got '%s'", i, expectedOrder[i], h.Name)
		}
	}
}

func TestExtractTarGz_RoundTripAndSafety(t *testing.T) {
	sourceDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(sourceDir, "hello.txt"), []byte("hello world"), 0644)
	subDir := filepath.Join(sourceDir, "sub")
	_ = os.MkdirAll(subDir, 0755)
	_ = os.WriteFile(filepath.Join(subDir, "script.sh"), []byte("#!/bin/sh"), 0755)

	var buf bytes.Buffer
	if err := ArchiveDirToTarGz(sourceDir, &buf); err != nil {
		t.Fatal(err)
	}

	destDir := t.TempDir()
	if err := ExtractTarGz(&buf, destDir); err != nil {
		t.Fatalf("ExtractTarGz failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(destDir, "hello.txt"))
	if err != nil || string(content) != "hello world" {
		t.Errorf("expected 'hello world', got '%s' (err: %v)", string(content), err)
	}

	scriptFi, err := os.Stat(filepath.Join(destDir, "sub", "script.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if scriptFi.Mode().Perm() != 0755 {
		t.Errorf("expected 0755 for script, got %o", scriptFi.Mode().Perm())
	}
}

func TestExtractTarGz_ZipSlipDefense(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	// Craft a malicious tar entry attempting Zip Slip
	maliciousHdr := &tar.Header{
		Name:     "../slip.txt",
		Mode:     0644,
		Size:     4,
		Typeflag: tar.TypeReg,
	}
	_ = tw.WriteHeader(maliciousHdr)
	_, _ = tw.Write([]byte("evil"))
	_ = tw.Close()
	_ = gw.Close()

	destDir := t.TempDir()
	err := ExtractTarGz(&buf, destDir)
	if err == nil {
		t.Fatal("expected error on Zip Slip archive entry, got nil")
	}
}

func TestCopyFileAndDir(t *testing.T) {
	tempDir := t.TempDir()

	srcDir := filepath.Join(tempDir, "src")
	_ = os.MkdirAll(filepath.Join(srcDir, "nested"), 0755)
	_ = os.WriteFile(filepath.Join(srcDir, "file1.txt"), []byte("data1"), 0644)
	_ = os.WriteFile(filepath.Join(srcDir, "nested", "run.sh"), []byte("#!/bin/sh"), 0755)

	dstDir := filepath.Join(tempDir, "dst")
	if err := CopyDir(srcDir, dstDir, tempDir); err != nil {
		t.Fatalf("CopyDir failed: %v", err)
	}

	c1, err := os.ReadFile(filepath.Join(dstDir, "file1.txt"))
	if err != nil || string(c1) != "data1" {
		t.Errorf("copy failed for file1.txt: %v", err)
	}

	c2, err := os.ReadFile(filepath.Join(dstDir, "nested", "run.sh"))
	if err != nil || string(c2) != "#!/bin/sh" {
		t.Errorf("copy failed for run.sh: %v", err)
	}
}
