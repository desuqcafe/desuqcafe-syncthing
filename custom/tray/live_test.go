package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// These exercise the tray against a real Syncthing rather than a mock, because
// the things worth getting wrong here are all about the actual API: what
// /rest/config returns, what a folder state string looks like, whether pausing
// really is visible afterwards. A mock would only assert that the code agrees
// with itself.
//
// Point DESUQ_TRAY_TEST_HOME at a Syncthing home directory whose instance is
// running, and run:
//
//	go test ./...
//
// Skipped when the variable is unset, so a plain `go test` stays clean.
func liveClient(t *testing.T) (*client, string) {
	t.Helper()

	home := os.Getenv("DESUQ_TRAY_TEST_HOME")
	if home == "" {
		t.Skip("DESUQ_TRAY_TEST_HOME not set; skipping live tests")
	}

	ep, err := readEndpoint(home)
	if err != nil {
		t.Fatalf("readEndpoint(%q): %v", home, err)
	}
	if ep.apiKey == "" {
		t.Fatal("no API key in config.xml")
	}
	t.Logf("endpoint %s", ep.baseURL)

	c := newClient(ep)
	if err := c.ping(); err != nil {
		t.Fatalf("ping %s: %v", ep.baseURL, err)
	}

	var status struct {
		MyID string `json:"myID"`
	}
	if err := c.get("/rest/system/status", nil, &status); err != nil {
		t.Fatalf("system status: %v", err)
	}
	return c, status.MyID
}

func TestPollReportsIdle(t *testing.T) {
	c, selfID := liveClient(t)

	s := poll(c, selfID)
	t.Logf("state=%v headline=%q detail=%q connected=%d remotes=%d",
		s.State, s.Headline, s.Detail, s.Connected, s.Remotes)

	if s.State == StateOffline {
		t.Fatalf("polled a running instance but got offline: %+v", s)
	}
	if s.Headline == "" {
		t.Error("headline is empty")
	}
	if tip := tooltip(appName, s); len(tip) > 120 {
		t.Errorf("tooltip is %d chars, over the Windows limit: %q", len(tip), tip)
	}
}

func TestPauseAndResumeRoundTrip(t *testing.T) {
	c, selfID := liveClient(t)

	before := poll(c, selfID)
	if before.Paused {
		t.Skip("instance is already paused; refusing to guess the original state")
	}

	if err := c.pauseAll(); err != nil {
		t.Fatalf("pauseAll: %v", err)
	}
	t.Cleanup(func() {
		if err := c.resumeAll(); err != nil {
			t.Errorf("could not resume after the test: %v", err)
		}
	})

	if got := waitForPaused(t, c, selfID, true); !got {
		t.Fatal("paused all devices but poll never reported StatePaused")
	}

	if err := c.resumeAll(); err != nil {
		t.Fatalf("resumeAll: %v", err)
	}
	if got := waitForPaused(t, c, selfID, false); got {
		t.Fatal("resumed all devices but poll still reported paused")
	}
}

func waitForPaused(t *testing.T, c *client, selfID string, want bool) bool {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var last Status
	for time.Now().Before(deadline) {
		last = poll(c, selfID)
		if last.Paused == want {
			t.Logf("paused=%v state=%v headline=%q", last.Paused, last.State, last.Headline)
			return last.Paused
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Logf("timed out waiting for paused=%v, last was %+v", want, last)
	return last.Paused
}

// TestMarkFoldersForExplorer runs the real reconcile against the real
// instance and then reads back what landed on disk. The parts that can go
// wrong here are all outside the Go: whether Syncthing accepts the appended
// ignore line, whether the file can be overwritten once it is hidden and
// system, and whether the folder ends up carrying the attribute without which
// Explorer never looks at a desktop.ini at all.
func TestMarkFoldersForExplorer(t *testing.T) {
	c, _ := liveClient(t)

	cfg, err := c.config()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if len(cfg.Folders) == 0 {
		t.Skip("the instance has no folders to mark")
	}

	m := newFolderMarker(t.TempDir())
	if err := m.ensureIcon(); err != nil {
		t.Fatalf("ensureIcon: %v", err)
	}
	if info, err := os.Stat(m.iconPath); err != nil {
		t.Fatalf("icon was not written: %v", err)
	} else if info.Size() != int64(len(iconFolder)) {
		t.Errorf("icon is %d bytes on disk, embedded is %d", info.Size(), len(iconFolder))
	}

	// Twice: the second pass must be a no-op rather than a second appended
	// ignore line, because it runs every two minutes for as long as the tray
	// is up.
	m.reconcile(c)
	first := map[string][]string{}
	for _, f := range cfg.Folders {
		ign, err := c.ignores(f.ID)
		if err != nil {
			t.Fatalf("ignores(%s): %v", f.ID, err)
		}
		first[f.ID] = ign.Ignore
	}
	// Forget everything, so the second pass really does re-read the patterns.
	m.done = map[string]string{}
	m.ignored = map[string]bool{}
	m.reconcile(c)

	marked := 0
	for _, f := range cfg.Folders {
		if _, err := os.Stat(f.Path); err != nil {
			t.Logf("%s: path not present, skipped", f.ID)
			continue
		}

		ign, err := c.ignores(f.ID)
		if err != nil {
			t.Fatalf("ignores(%s): %v", f.ID, err)
		}
		if ign.Error != "" {
			t.Logf("%s: ignores do not parse (%s), skipped", f.ID, ign.Error)
			continue
		}
		if !ignoresCoverDesktopIni(ign.Ignore) {
			t.Errorf("%s: desktop.ini is still not excluded: %q", f.ID, ign.Ignore)
		}
		if len(ign.Ignore) != len(first[f.ID]) {
			t.Errorf("%s: a second pass changed the ignore list, %d lines -> %d: %q",
				f.ID, len(first[f.ID]), len(ign.Ignore), ign.Ignore)
		}

		ini := filepath.Join(f.Path, "desktop.ini")
		raw, err := os.ReadFile(ini)
		if err != nil {
			t.Errorf("%s: no desktop.ini: %v", f.ID, err)
			continue
		}
		if len(raw) < 2 || raw[0] != 0xFF || raw[1] != 0xFE {
			t.Errorf("%s: desktop.ini has no UTF-16LE BOM: % X", f.ID, raw[:min(4, len(raw))])
		}
		if !bytes.Contains(raw, utf16LE("IconResource=" + m.iconPath)[2:]) {
			t.Errorf("%s: desktop.ini does not point at the icon", f.ID)
		}
		checkExplorerAttributes(t, f.ID, f.Path, ini)
		marked++

		t.Cleanup(func() {
			_ = os.Remove(ini)
		})
	}

	if marked == 0 {
		t.Skip("no folder was present on disk to mark")
	}
	t.Logf("marked %d folder(s)", marked)
}

func TestIconsAreValidIcoFiles(t *testing.T) {
	for _, tc := range []struct {
		name string
		ico  []byte
	}{
		{"idle", iconFor(StateIdle)},
		{"syncing", iconFor(StateSyncing)},
		{"paused", iconFor(StatePaused)},
		{"error", iconFor(StateError)},
		{"offline", iconFor(StateOffline)},
		// Not a tray state, but it is written to disk for Explorer to load and
		// a malformed one would simply show no icon at all.
		{"folder", iconFolder},
	} {
		b := tc.ico
		if len(b) < 22 {
			t.Errorf("%s: only %d bytes", tc.name, len(b))
			continue
		}
		// ICONDIR: reserved 0, type 1, then a non-zero image count.
		if b[0] != 0 || b[1] != 0 || b[2] != 1 || b[3] != 0 {
			t.Errorf("%s: not an ICO header: % x", tc.name, b[:4])
		}
		if count := int(b[4]) | int(b[5])<<8; count == 0 {
			t.Errorf("%s: ICO declares no images", tc.name)
		} else {
			t.Logf("%s: %d bytes, %d sizes", tc.name, len(b), count)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	for _, tc := range []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{999, "999 B"},
		{1000, "1.0 kB"},
		{2_252_800, "2.3 MB"},
		{5_000_000_000, "5.0 GB"},
	} {
		if got := formatBytes(tc.in); got != tc.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
