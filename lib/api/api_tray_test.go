// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

package api

import (
	"path/filepath"
	"testing"
)

func TestResolveTrayStatus(t *testing.T) {
	dir := filepath.Join("C:", "Users", "yuki", "AppData", "Local", "Programs", "desuq-syncthing")
	exe := filepath.Join(dir, "desuq-syncthing.exe")
	tray := filepath.Join(dir, "desuq-syncthing-tray.exe")
	marker := filepath.Join(dir, installMarker)

	on := func(files ...string) func(string) bool {
		return func(p string) bool {
			for _, f := range files {
				if p == f {
					return true
				}
			}
			return false
		}
	}

	cases := []struct {
		name  string
		exe   string
		goos  string
		files []string
		want  trayStatus
	}{
		{"installed, tray present", exe, "windows", []string{marker, tray},
			trayStatus{Expected: true, Present: true, Path: tray}},
		// The case this exists for: Defender took the tray, the daemon is
		// still running beside the marker.
		{"installed, tray quarantined", exe, "windows", []string{marker},
			trayStatus{Expected: true, Present: false, Path: tray}},
		// custom\dist\ has the tray but no marker; a bare build has neither.
		// Neither is an install, and neither may raise the alarm.
		{"dev build without marker", exe, "windows", []string{tray}, trayStatus{}},
		{"bare build", exe, "windows", nil, trayStatus{}},
		{"not windows", exe, "linux", []string{marker}, trayStatus{}},
		{"executable unknown", "", "windows", []string{marker}, trayStatus{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := resolveTrayStatus(c.exe, c.goos, on(c.files...))
			if got != c.want {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}

	// The tray's name follows the binary's, so a rebrand needs no edit here.
	other := filepath.Join(dir, "acme-sync.exe")
	got := resolveTrayStatus(other, "windows", on(marker))
	if want := filepath.Join(dir, "acme-sync-tray.exe"); got.Path != want {
		t.Errorf("tray path for a rebranded binary: got %q, want %q", got.Path, want)
	}
}
