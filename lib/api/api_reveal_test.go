// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// desuqcafe fork. See api_reveal.go.
//
// The route hands a directory to the host's file manager, so the only thing
// that keeps "open this" from becoming "run that" is that the directory is
// resolved from a configured folder and proved to be inside it. That check is
// what this file is about.

package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTargetWithinStaysInsideTheFolder(t *testing.T) {
	t.Parallel()

	root := filepath.Clean(filepath.Join("C:", "sync", "assets"))
	if filepath.Separator == '/' {
		root = "/sync/assets"
	}

	cases := []struct {
		name string
		sub  string
		want string // "" means the call must be refused
	}{
		{"empty sub is the folder root", "", root},
		{"a plain sub-path", "textures", filepath.Join(root, "textures")},
		{"a nested sub-path", "textures/wood", filepath.Join(root, "textures", "wood")},
		{"dot segments that stay inside", "textures/../models", filepath.Join(root, "models")},
		{"a trailing slash", "textures/", filepath.Join(root, "textures")},

		// The point of the whole exercise.
		{"a parent escape", "..", ""},
		{"a deeper parent escape", "../../Windows/System32", ""},
		{"an escape hidden behind a real directory", "textures/../../../secrets", ""},
		{"a NUL byte", "textures\x00/wood", ""},

		// A leading slash is not an absolute path to Join, which is what makes
		// this safe rather than a way to name the filesystem root.
		{"a leading slash is still relative", "/textures", filepath.Join(root, "textures")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := targetWithin(root, tc.sub)
			if tc.want == "" {
				if err == nil {
					t.Fatalf("targetWithin(%q, %q) = %q, want refusal", root, tc.sub, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("targetWithin(%q, %q) refused: %v", root, tc.sub, err)
			}
			if got != tc.want {
				t.Errorf("targetWithin(%q, %q) = %q, want %q", root, tc.sub, got, tc.want)
			}
		})
	}
}

func TestNearestExistingDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	deep := filepath.Join(root, "textures", "wood")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(deep, "oak.png")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("an existing directory is itself", func(t *testing.T) {
		got, err := nearestExistingDir(root, deep)
		if err != nil || got != deep {
			t.Fatalf("got %q, %v; want %q", got, err, deep)
		}
	})

	// A file manager pointed at a file either does nothing or, on Windows,
	// hands it to the shell to run. Showing the containing directory is both
	// what was meant and the only safe reading.
	t.Run("a file resolves to its directory", func(t *testing.T) {
		got, err := nearestExistingDir(root, file)
		if err != nil || got != deep {
			t.Fatalf("got %q, %v; want %q", got, err, deep)
		}
	})

	t.Run("a deleted sub-path falls back to its nearest parent", func(t *testing.T) {
		got, err := nearestExistingDir(root, filepath.Join(deep, "gone", "also-gone"))
		if err != nil || got != deep {
			t.Fatalf("got %q, %v; want %q", got, err, deep)
		}
	})

	// The drive is not plugged in. There is nothing to open and the caller
	// needs to hear that rather than watch a button do nothing.
	t.Run("a missing root is an error, not a walk further up", func(t *testing.T) {
		missing := filepath.Join(root, "not-mounted")
		if _, err := nearestExistingDir(missing, filepath.Join(missing, "textures")); err == nil {
			t.Fatal("want an error for a folder root that does not exist")
		}
	})
}
