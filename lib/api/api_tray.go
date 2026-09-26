// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// This file is an addition made by the desuqcafe fork. It lives in its own
// file, and adds a single line to api.go. See custom/CUSTOMIZATIONS.md.
//
// Windows Defender has quarantined the fork's tray executable on install as
// Trojan:Win32/Bearfoos.A!ml -- a machine-learning false positive, see
// DEPLOYMENT-3D-TEAM.md section 20. It takes both shortcuts with it, and the
// sign-in shortcut is what starts Syncthing, so the machine syncs normally
// until the next restart and then silently never again.
//
// Nothing the tray does can report that: it is the file that was removed.
// This binary has never once been flagged, and when the tray is eaten out
// from under it, it keeps running -- so it is the thing left standing to say
// so. The main screen asks here and turns a missing tray into its loudest
// headline.

package api

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// trayStatus is the answer to GET /rest/system/tray.
type trayStatus struct {
	// Expected is true only when this binary is running from an installed
	// copy, where a tray belongs beside it. A development build, a `go run`,
	// or any non-Windows host answers false and is never alarmed about.
	Expected bool `json:"expected"`
	// Present is whether the tray executable is on disk. Meaningful only
	// when Expected is true.
	Present bool `json:"present"`
	// Path is where the tray should be, for the technical details.
	Path string `json:"path,omitempty"`
	// BlenderAddon is the optional Blender add-on the installer ships beside
	// the binaries (custom/blender), when it is there. The main screen tells
	// modellers where to find it; Blender's "Install from Disk" needs a path.
	BlenderAddon string `json:"blenderAddon,omitempty"`
}

// installMarker is shipped into the install directory by the installer and by
// nothing else (custom/installer/installer.iss, [Files]). Its presence beside
// the binary is what distinguishes an installed copy from custom\dist\, which
// has a tray but no marker, and from a bare build, which has neither.
const installMarker = "seed-config.ps1"

// resolveTrayStatus is the whole decision, kept free of os.Executable so it
// can be tested. The tray's name is derived from this binary's own --
// desuq-syncthing.exe beside desuq-syncthing-tray.exe -- so a rebrand in
// custom/branding.ps1 flows through without an edit here.
func resolveTrayStatus(exe, goos string, exists func(string) bool) trayStatus {
	if goos != "windows" || exe == "" {
		return trayStatus{}
	}
	dir := filepath.Dir(exe)
	if !exists(filepath.Join(dir, installMarker)) {
		return trayStatus{}
	}
	base := strings.TrimSuffix(filepath.Base(exe), filepath.Ext(exe))
	tray := filepath.Join(dir, base+"-tray.exe")
	st := trayStatus{Expected: true, Present: exists(tray), Path: tray}
	if addon := filepath.Join(dir, "blender", "desuq_syncthing.zip"); exists(addon) {
		st.BlenderAddon = addon
	}
	return st
}

func (*service) getSystemTray(w http.ResponseWriter, _ *http.Request) {
	exe, err := os.Executable()
	if err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
	} else {
		exe = ""
	}
	sendJSON(w, resolveTrayStatus(exe, runtime.GOOS, func(p string) bool {
		_, err := os.Stat(p)
		return err == nil
	}))
}
