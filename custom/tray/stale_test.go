package main

import (
	"strings"
	"testing"
	"time"
)

// now is fixed so the wording assertions can name a date.
var staleNow = time.Date(2026, 8, 26, 12, 0, 0, 0, time.Local)

func daysAgo(n float64) time.Time {
	return staleNow.Add(-time.Duration(n * float64(24*time.Hour)))
}

func TestStalePeers(t *testing.T) {
	cases := []struct {
		name     string
		peers    []peerSeen
		wantIDs  []string
		wantAll  bool
		eligible int
	}{
		{
			name: "a peer away for four days",
			peers: []peerSeen{
				{id: "T", name: "Kai", lastSeen: daysAgo(4)},
				{id: "Y", name: "Yuki", lastSeen: daysAgo(0.1), connected: true},
			},
			wantIDs:  []string{"T"},
			eligible: 2,
		},
		{
			name: "a weekend is not stale",
			peers: []peerSeen{
				{id: "T", name: "Kai", lastSeen: daysAgo(2.5)},
			},
			eligible: 1,
		},
		{
			// Paused is a decision somebody made, not a machine that has
			// stopped talking.
			name: "a paused device is never stale",
			peers: []peerSeen{
				{id: "T", name: "Kai", lastSeen: daysAgo(30), paused: true},
			},
			eligible: 0,
		},
		{
			// Configured an hour ago and not yet accepted at the other end.
			// The first-run guide owns that story.
			name: "a device that has never connected is never stale",
			peers: []peerSeen{
				{id: "T", name: "Kai"},
			},
			eligible: 0,
		},
		{
			// The shape a real instance answers with, which is NOT the zero
			// time. Written from a live run: IsZero is false here, and without
			// seenEver this device is reported as silent for twenty thousand
			// days.
			name: "and neither is one whose lastSeen is the Unix epoch",
			peers: []peerSeen{
				{id: "T", name: "Kai", lastSeen: time.Unix(0, 0)},
			},
			eligible: 0,
		},
		{
			name: "connected right now is never stale, however long the gap",
			peers: []peerSeen{
				{id: "T", name: "Kai", lastSeen: daysAgo(30), connected: true},
			},
			eligible: 1,
		},
		{
			name: "everybody away points at this computer",
			peers: []peerSeen{
				{id: "T", name: "Kai", lastSeen: daysAgo(4)},
				{id: "Y", name: "Yuki", lastSeen: daysAgo(6)},
			},
			wantIDs:  []string{"Y", "T"}, // oldest contact first
			wantAll:  true,
			eligible: 2,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := stalePeers(tc.peers, staleNow, staleAfter)
			if len(v.Stale) != len(tc.wantIDs) {
				t.Fatalf("stale = %d entries, want %d", len(v.Stale), len(tc.wantIDs))
			}
			for i, want := range tc.wantIDs {
				if v.Stale[i].id != want {
					t.Errorf("stale[%d] = %q, want %q", i, v.Stale[i].id, want)
				}
			}
			if v.All != tc.wantAll {
				t.Errorf("all = %v, want %v", v.All, tc.wantAll)
			}
			if v.Eligible != tc.eligible {
				t.Errorf("eligible = %d, want %d", v.Eligible, tc.eligible)
			}
		})
	}
}

// The three sentences differ in who they blame, and that is the whole design.
// See stale.go.
func TestStaleMessage(t *testing.T) {
	t.Run("nothing stale says nothing", func(t *testing.T) {
		title, body := staleMessage(stalePeers(nil, staleNow, staleAfter))
		if title != "" || body != "" {
			t.Errorf("got %q / %q, want silence", title, body)
		}
	})

	t.Run("one away, others connected: name them", func(t *testing.T) {
		v := stalePeers([]peerSeen{
			{id: "T", name: "Kai", lastSeen: daysAgo(4)},
			{id: "Y", name: "Yuki", lastSeen: staleNow, connected: true},
		}, staleNow, staleAfter)
		title, body := staleMessage(v)
		if title != "Kai has not synced for 4 days" {
			t.Errorf("title = %q", title)
		}
		if !strings.Contains(body, "22 August") {
			t.Errorf("body does not carry the date: %q", body)
		}
		if strings.Contains(body, "this computer") {
			t.Errorf("body blames this computer when others are connected: %q", body)
		}
	})

	t.Run("one peer, nothing connected: blame nobody", func(t *testing.T) {
		v := stalePeers([]peerSeen{
			{id: "T", name: "Kai", lastSeen: daysAgo(4)},
		}, staleNow, staleAfter)
		title, _ := staleMessage(v)
		if title != "You and Kai have not synced for 4 days" {
			t.Errorf("title = %q", title)
		}
	})

	t.Run("everybody away: say it may be this computer", func(t *testing.T) {
		v := stalePeers([]peerSeen{
			{id: "T", name: "Kai", lastSeen: daysAgo(4)},
			{id: "Y", name: "Yuki", lastSeen: daysAgo(6)},
		}, staleNow, staleAfter)
		title, body := staleMessage(v)
		if title != "Nothing has synced for 6 days" {
			t.Errorf("title = %q", title)
		}
		if !strings.Contains(body, "this computer that is offline") {
			t.Errorf("body = %q", body)
		}
	})

	// joinNames collapses three or more, so the verb has to come from the
	// count and not from the string.
	t.Run("three away reads as English", func(t *testing.T) {
		v := stalePeers([]peerSeen{
			{id: "T", name: "Kai", lastSeen: daysAgo(4)},
			{id: "Y", name: "Yuki", lastSeen: daysAgo(5)},
			{id: "S", name: "Sam", lastSeen: daysAgo(6)},
			{id: "M", name: "Mika", lastSeen: staleNow, connected: true},
		}, staleNow, staleAfter)
		title, _ := staleMessage(v)
		if !strings.HasSuffix(title, "have not synced for 6 days") {
			t.Errorf("title = %q", title)
		}
	})
}

func TestStaleSpan(t *testing.T) {
	cases := map[time.Duration]string{
		4 * 24 * time.Hour:          "4 days",
		25 * time.Hour:              "1 day",
		3 * time.Hour:               "less than a day",
		90*24*time.Hour + time.Hour: "90 days",
	}
	for d, want := range cases {
		if got := staleSpan(d); got != want {
			t.Errorf("staleSpan(%v) = %q, want %q", d, got, want)
		}
	}
}
