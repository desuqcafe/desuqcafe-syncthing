// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// desuqcafe fork. See api_reveal.go.

package api

import (
	"os/exec"
)

// revealDir opens dir in Explorer.
//
// Started and not waited for, deliberately. explorer.exe is a launcher: it
// hands the request to the already-running desktop shell and exits, and it
// exits **1 on success** -- a documented-by-folklore quirk that has bitten
// every "check the exit code" wrapper ever written. There is nothing useful in
// its status either way, so the request answers on a successful start.
//
// The argument is passed as its own element of the argument slice, never
// through a shell, and dir has already been resolved from a configured folder
// and proved to be a directory. That combination is what makes this safe:
// explorer.exe handed a path to an *executable* would run it.
func revealDir(dir string) error {
	cmd := exec.Command("explorer.exe", dir)
	if err := cmd.Start(); err != nil {
		return err
	}
	// Reap it rather than leaving the process entry behind.
	go func() { _ = cmd.Wait() }()
	return nil
}
