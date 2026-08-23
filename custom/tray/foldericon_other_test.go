//go:build !windows

package main

import "testing"

// File attributes are a Windows concept, and writeFolderMarker is a no-op
// everywhere else, so there is nothing to assert. This exists so live_test.go
// still compiles on a developer's Mac or Linux box; the real assertion is in
// foldericon_windows_test.go.
func checkExplorerAttributes(t *testing.T, folderID, dir, ini string) {
	t.Helper()
}
