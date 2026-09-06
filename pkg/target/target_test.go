// SPDX-FileCopyrightText: 2026 Marcin Kaim
// SPDX-License-Identifier: Apache-2.0

package target

import (
	"context"
	"testing"
)

type dummyPackager struct {
	name string
}

func (d *dummyPackager) TargetName() string {
	return d.name
}

func (d *dummyPackager) Build(ctx context.Context, opts PackageOptions) (*PackageResult, error) {
	return &PackageResult{
		PackageFile: "/tmp/dummy.pkg",
		Filename:    "dummy.pkg",
		Size:        100,
		TargetType:  d.name,
	}, nil
}

func TestTargetRegistry(t *testing.T) {
	// Register a mock packager
	Register("mock", func() TargetPackager {
		return &dummyPackager{name: "mock"}
	})

	// Retrieve existing target
	pkg, err := Get("mock")
	if err != nil {
		t.Fatalf("unexpected error getting mock target: %v", err)
	}
	if pkg.TargetName() != "mock" {
		t.Errorf("expected target name 'mock', got '%s'", pkg.TargetName())
	}

	// Verify build invocation
	res, err := pkg.Build(context.Background(), PackageOptions{})
	if err != nil {
		t.Fatalf("unexpected error during build: %v", err)
	}
	if res.TargetType != "mock" {
		t.Errorf("expected target type 'mock', got '%s'", res.TargetType)
	}

	// Retrieve unsupported target
	_, err = Get("unsupported-target")
	if err == nil {
		t.Errorf("expected error for unsupported target, got nil")
	}

	// Available targets includes registered target
	targets := AvailableTargets()
	found := false
	for _, tgt := range targets {
		if tgt == "mock" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'mock' in available targets: %v", targets)
	}
}
