package main

import (
	"os"
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

func TestIconsAreValidIcoFiles(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state State
	}{
		{"idle", StateIdle},
		{"syncing", StateSyncing},
		{"paused", StatePaused},
		{"error", StateError},
		{"offline", StateOffline},
	} {
		b := iconFor(tc.state)
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
