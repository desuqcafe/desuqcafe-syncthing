package main

import (
	"strings"
	"testing"
)

// The conflict watcher against a real conflict, produced by a real Syncthing.
//
// Every other test in conflicts_test.go builds its fixtures from a filename
// this repository wrote down, which proves the parsing and proves nothing about
// whether the filename is right. The format is Syncthing's, is not part of any
// API, and is matched by a literal string in conflicts.go -- so the one thing
// worth checking against a live instance is that the string still matches what
// the product actually writes.
//
// Set DESUQ_TRAY_TEST_HOME to a running instance's home directory, having first
// made the two sides of a test pair disagree about one file. See
// custom/scripts/start-test-pair.ps1; skipped otherwise, like the rest of
// live_test.go.
func TestFindsARealConflictFile(t *testing.T) {
	c, _ := liveClient(t)

	cfg, err := c.config()
	if err != nil {
		t.Fatalf("config: %v", err)
	}

	var found []conflictFile
	for _, f := range cfg.Folders {
		if f.Path == "" {
			continue
		}
		found = append(found, conflictsIn(f.Path, f.name())...)
	}
	if len(found) == 0 {
		t.Skip("no conflicting copies in any folder of this instance")
	}

	for _, c := range found {
		t.Logf("found %s", c.name)

		// The shape Syncthing writes: name.sync-conflict-DATE-TIME-SHORTID.ext
		if !strings.Contains(c.name, conflictMarker) {
			t.Errorf("%q matched the walk but not the marker", c.name)
		}
		orig := originalName(c.name)
		if orig == c.name {
			t.Errorf("originalName(%q) changed nothing", c.name)
		}
		if strings.Contains(orig, "sync-conflict") {
			t.Errorf("originalName(%q) left the generated part in: %q", c.name, orig)
		}
		// The extension has to survive: "texture1" and "texture1.png" are not
		// equally recognisable to somebody looking at a folder listing.
		if i := strings.LastIndex(c.name, "."); i >= 0 {
			if ext := c.name[i:]; !strings.HasSuffix(orig, ext) {
				t.Errorf("originalName(%q) = %q, which lost the %q", c.name, orig, ext)
			}
		}
	}

	n := conflictNotification(found, "http://127.0.0.1:8384/")
	t.Logf("toast: %s -- %s", n.Title, n.Body)
	if strings.Contains(n.Body, "sync-conflict-2") {
		t.Errorf("the toast reads out a generated filename: %q", n.Body)
	}
}
