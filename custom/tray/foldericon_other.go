//go:build !windows

package main

// desktop.ini means nothing outside Explorer, so there is nothing to write.
// This exists for the same reason platform_other.go does: so the module still
// builds and vets on a developer's Mac or Linux box, where the rest of the
// tray's logic and its tests are perfectly runnable.
func writeFolderMarker(_, _ string) error { return nil }

func removeFolderMarker(_ string) error { return nil }
