package main

import (
	"testing"
	"time"
)

// The two REST shapes the "has not synced for days" check depends on, against a
// real Syncthing rather than a fixture. Neither is guessable from the docs:
// lastSeen's zero value and whether the local device appears in the listing are
// exactly the things that would make this alert fire at the wrong people.
//
// Skipped without DESUQ_TRAY_TEST_HOME. See live_test.go.
func TestLiveDeviceStatsShape(t *testing.T) {
	c, myID := liveClient(t)

	stats, err := c.deviceStats()
	if err != nil {
		t.Fatalf("device stats: %v", err)
	}
	if len(stats) == 0 {
		t.Fatal("no device statistics at all; expected at least this device")
	}

	for id, s := range stats {
		t.Logf("%s lastSeen=%v zero=%v", shortDeviceID(id), s.LastSeen, s.LastSeen.IsZero())
	}

	// This is what the test was written to check and what it actually found:
	// a device that has never connected comes back as the Unix EPOCH, not as
	// Go's zero time. IsZero is false for it. seenEver is what has to catch
	// it, and a peer that lands in the alert as "20693 days" is the failure
	// this pins down.
	for id, s := range stats {
		if s.LastSeen.Year() < 2000 && seenEver(s.LastSeen) {
			t.Errorf("%s reports lastSeen in %d and seenEver still says it has connected",
				shortDeviceID(id), s.LastSeen.Year())
		}
	}

	// The local device is in /rest/config like any other, so checkStale drops
	// it by ID. Prove the ID it drops by is the one that appears there.
	got, err := c.myID()
	if err != nil {
		t.Fatalf("myID: %v", err)
	}
	if got != myID {
		t.Errorf("myID() = %q, /rest/system/status said %q", got, myID)
	}

	cfg, err := c.config()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	var sawSelf bool
	for _, d := range cfg.Devices {
		if d.DeviceID == myID {
			sawSelf = true
		}
	}
	if !sawSelf {
		t.Error("the local device is not in /rest/config; checkStale's filter is aimed at nothing")
	}
}

// The whole check, end to end against the live instance: it must not decide
// that a peer connected right now has been away for days.
func TestLiveStalePeersSaysNothingAboutAConnectedPeer(t *testing.T) {
	c, myID := liveClient(t)

	cfg, err := c.config()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	stats, err := c.deviceStats()
	if err != nil {
		t.Fatalf("device stats: %v", err)
	}
	conns, err := c.connections()
	if err != nil {
		t.Fatalf("connections: %v", err)
	}

	var peers []peerSeen
	for _, d := range cfg.Devices {
		if d.DeviceID == myID {
			continue
		}
		peers = append(peers, peerSeen{
			id:        d.DeviceID,
			name:      d.Name,
			paused:    d.Paused,
			connected: conns.Connections[d.DeviceID].Connected,
			lastSeen:  stats[d.DeviceID].LastSeen,
		})
	}
	if len(peers) == 0 {
		t.Skip("this instance has no peers configured")
	}

	v := stalePeers(peers, time.Now(), staleAfter)
	for _, p := range v.Stale {
		if p.connected {
			t.Errorf("%s is connected and was still reported stale", p.name)
		}
	}
	title, body := staleMessage(v)
	t.Logf("verdict: %d of %d eligible stale; title=%q body=%q",
		len(v.Stale), v.Eligible, title, body)
}
