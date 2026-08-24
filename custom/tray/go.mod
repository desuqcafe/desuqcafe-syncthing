// Deliberately a separate module from the Syncthing tree above it.
//
// The tray needs a systray dependency that upstream has no use for. Adding it
// to the root go.mod would put a line in upstream's go.mod and go.sum that
// conflicts on every dependency bump they make, for a program that is not part
// of Syncthing at all. A nested module keeps both files untouched: the parent
// build ignores this directory entirely, and this one is built separately by
// custom/build-windows.ps1.
//
// The module path is never fetched -- this is a main package that nothing
// imports -- but it matches the fork's repository so it reads correctly.
module github.com/desuqcafe/desuqcafe-syncthing/custom/tray

go 1.25.0

require (
	fyne.io/systray v1.12.2
	golang.org/x/sys v0.47.0
)

require github.com/godbus/dbus/v5 v5.1.0 // indirect
