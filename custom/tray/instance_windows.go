//go:build windows

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// One tray per home directory.
//
// This only started mattering when the Start Menu and Desktop shortcuts began
// launching the tray instead of the daemon. Before that there was exactly one
// way to get a tray -- the sign-in shortcut -- so a second one was not
// reachable by accident. Now searching Windows for the app and pressing Enter
// while it is already running would put a second icon in the notification
// area, each with its own supervisor and its own event subscription, and the
// user would have no way to tell which was which or why Quit only removed one.
//
// The lock is a named mutex rather than a lock file because Windows releases
// it when the process dies, however it dies. A file would need cleaning up
// after a crash, and a stale one would leave the tray permanently unable to
// start -- a worse failure than the one being prevented.

// claimInstance reports whether this process is the only tray for home.
//
// The handle is deliberately never closed: it is released when the process
// exits, which is exactly the lifetime wanted.
func claimInstance(home string) bool {
	name, err := windows.UTF16PtrFromString(instanceMutexName(home))
	if err != nil {
		// Cannot happen for a name built out of hex, but refusing to start
		// over an impossible error would be worse than the duplicate icon
		// this is trying to avoid.
		return true
	}

	_, err = windows.CreateMutex(nil, false, name)
	return err != windows.ERROR_ALREADY_EXISTS
}

// instanceMutexName derives a mutex name from the home directory.
//
// Two things force the hash. Mutex names may not contain a backslash except
// in the leading namespace, and they are capped at MAX_PATH -- so a home
// directory cannot go in literally. Hashing it also means the name gives away
// nothing about where the user keeps their files, which matters slightly more
// than it sounds: object names are readable by anything running as the user.
//
// The "Local\" namespace scopes it to the session. Two people signed in to the
// same machine at once each get their own tray, which is right: this is a
// per-user install and their home directories are different anyway.
func instanceMutexName(home string) string {
	// Normalise first, so C:\Users\x\AppData and c:/users/x/appdata are one
	// instance rather than two. Case-folding is correct on Windows; the path
	// separator swap is what filepath.Clean does not do for a forward-slash
	// path typed on the command line.
	clean := strings.ToLower(filepath.Clean(strings.ReplaceAll(home, "/", `\`)))
	sum := sha256.Sum256([]byte(clean))
	return `Local\desuqcafe-syncthing-tray-` + hex.EncodeToString(sum[:16])
}
