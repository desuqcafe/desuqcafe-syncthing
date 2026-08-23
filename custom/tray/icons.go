package main

import _ "embed"

// The icons are committed rather than generated at build time; see
// icons/make-icons.ps1 for how they are drawn and how to change them.

//go:embed icons/idle.ico
var iconIdle []byte

//go:embed icons/syncing.ico
var iconSyncing []byte

//go:embed icons/paused.ico
var iconPaused []byte

//go:embed icons/error.ico
var iconError []byte

//go:embed icons/offline.ico
var iconOffline []byte

// Not a tray state: this is the icon written into each synced folder's
// desktop.ini so the folder is recognisable in Explorer. It carries the sizes
// Explorer asks for rather than the tray's, which is why it is far the largest
// of them. See foldericon.go.
//
//go:embed icons/folder.ico
var iconFolder []byte

func iconFor(s State) []byte {
	switch s {
	case StateIdle:
		return iconIdle
	case StateSyncing:
		return iconSyncing
	case StatePaused:
		return iconPaused
	case StateError:
		return iconError
	default:
		return iconOffline
	}
}
