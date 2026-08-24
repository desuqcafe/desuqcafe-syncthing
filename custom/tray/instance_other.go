//go:build !windows

package main

// The tray ships on Windows only; this exists so the module still builds and
// vets elsewhere. See instance_windows.go for what the real one does.

func claimInstance(string) bool { return true }
