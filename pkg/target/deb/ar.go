// SPDX-FileCopyrightText: 2026 Marcin Kaim
// SPDX-License-Identifier: Apache-2.0

package deb

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"craftpack/pkg/fsutil"
	"github.com/blakesmith/ar"
)

// DebianBinaryContent defines the canonical format version record ("2.0\n").
const DebianBinaryContent = "2.0\n"

// AssembleDeb writes a valid Debian package (.deb) to targetWriter using standard Unix ar containerization.
// In accordance with Section 2.4 of the specification, the members are sequenced strictly as:
// 1. debian-binary (content "2.0\n", mode 0644)
// 2. control.tar.gz (mode 0644)
// 3. data.tar.gz (mode 0644)
// Owner UID/GID are set to 0 (root:root) and timestamps are deterministically populated.
func AssembleDeb(targetWriter io.Writer, controlTarGz, dataTarGz []byte, modTime time.Time) error {
	if targetWriter == nil {
		return errors.New("target writer cannot be nil")
	}
	if len(controlTarGz) == 0 {
		return errors.New("control.tar.gz content cannot be empty")
	}
	if len(dataTarGz) == 0 {
		return errors.New("data.tar.gz content cannot be empty")
	}

	if modTime.IsZero() {
		modTime = time.Unix(0, 0).UTC()
	} else {
		modTime = modTime.UTC().Truncate(time.Second)
	}

	aw := ar.NewWriter(targetWriter)
	if err := aw.WriteGlobalHeader(); err != nil {
		return fmt.Errorf("failed writing ar global header: %w", err)
	}

	type arMember struct {
		name string
		data []byte
	}

	members := []arMember{
		{name: "debian-binary", data: []byte(DebianBinaryContent)},
		{name: "control.tar.gz", data: controlTarGz},
		{name: "data.tar.gz", data: dataTarGz},
	}

	for _, m := range members {
		hdr := &ar.Header{
			Name:    m.name,
			Size:    int64(len(m.data)),
			Mode:    int64(fsutil.FileMode),
			ModTime: modTime,
			Uid:     0,
			Gid:     0,
		}
		if err := aw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("failed writing ar header for '%s': %w", m.name, err)
		}
		if _, err := aw.Write(m.data); err != nil {
			return fmt.Errorf("failed writing ar body for '%s': %w", m.name, err)
		}
	}

	return nil
}

// DebArchive encapsulates the parsed in-memory members of a Debian .deb archive.
type DebArchive struct {
	DebianBinary []byte
	ControlTarGz []byte
	DataTarGz    []byte
}

// ReadDeb parses a .deb container using pure-Go ar reader and asserts member sequencing.
func ReadDeb(r io.Reader) (*DebArchive, error) {
	if r == nil {
		return nil, errors.New("reader cannot be nil")
	}

	reader := ar.NewReader(r)
	deb := &DebArchive{}

	var order []string
	for {
		hdr, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed reading ar entry: %w", err)
		}

		data, err := io.ReadAll(reader)
		if err != nil {
			return nil, fmt.Errorf("failed reading entry payload for '%s': %w", hdr.Name, err)
		}

		cleanName := strings.TrimSpace(hdr.Name)
		order = append(order, cleanName)

		switch cleanName {
		case "debian-binary":
			deb.DebianBinary = data
		case "control.tar.gz":
			deb.ControlTarGz = data
		case "data.tar.gz":
			deb.DataTarGz = data
		}
	}

	// Strictly verify the 3-member sequence required by Debian packaging standards
	if len(order) != 3 || order[0] != "debian-binary" || order[1] != "control.tar.gz" || order[2] != "data.tar.gz" {
		return nil, fmt.Errorf("invalid .deb member sequence: got %v, expected [debian-binary, control.tar.gz, data.tar.gz]", order)
	}

	return deb, nil
}
