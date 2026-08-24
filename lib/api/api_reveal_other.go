// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// desuqcafe fork. See api_reveal.go.

//go:build !windows

package api

import "errors"

// revealDir is not implemented off Windows.
//
// This fork ships a Windows installer and nothing else, and the plausible
// alternatives -- xdg-open, open(1) -- are launchers for whatever the desktop
// has registered, which on a headless or container install is a way to run
// something unexpected on behalf of anyone holding the API key. Saying no is
// both honest and the smaller attack surface; the route answers 501 and the
// button that calls it says so.
func revealDir(string) error {
	return errors.New("opening a folder is only supported on Windows")
}
