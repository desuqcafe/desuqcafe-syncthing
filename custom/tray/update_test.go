package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseForkVersion(t *testing.T) {
	cases := []struct {
		in   string
		ok   bool
		want forkVersion
	}{
		{"v2.1.4-desuq.1", true, forkVersion{2, 1, 4, 1, 0}},
		{"v2.1.4-desuq.2", true, forkVersion{2, 1, 4, 2, 0}},
		{"2.1.4-desuq.2", true, forkVersion{2, 1, 4, 2, 0}},
		{"v2.1.4-desuq.12", true, forkVersion{2, 1, 4, 12, 0}},
		{"v10.0.3-desuq.7", true, forkVersion{10, 0, 3, 7, 0}},

		// What `git describe` produces between tags, which is what the
		// developer's own machine reports. Both separators upstream has used.
		{"v2.1.4-desuq.1-23-gd6c6dde6", true, forkVersion{2, 1, 4, 1, 23}},
		{"v2.1.4-desuq.1-23-gd6c6dde6-dirty", true, forkVersion{2, 1, 4, 1, 23}},
		{"v2.1.4-desuq.1+23-gd6c6dde6", true, forkVersion{2, 1, 4, 1, 23}},

		// Everything that is not this fork. These must not parse, because a
		// version that parses is a version that gets compared -- and a peer on
		// stock Syncthing is routinely ahead of the base version we forked.
		{"v2.1.4", false, forkVersion{}},
		{"v2.3.0", false, forkVersion{}},
		{"v2.1.4-rc.1", false, forkVersion{}},
		{"syncthing v2.1.4", false, forkVersion{}},
		{"v1.27.12", false, forkVersion{}},
		{"", false, forkVersion{}},
		{"unknown-dev", false, forkVersion{}},
		{"v2.1.4-desuq", false, forkVersion{}},
		{"v2.1-desuq.1", false, forkVersion{}},
	}

	for _, c := range cases {
		got, ok := parseForkVersion(c.in)
		if ok != c.ok {
			t.Errorf("parseForkVersion(%q) ok = %v, want %v", c.in, ok, c.ok)
			continue
		}
		if ok && got != c.want {
			t.Errorf("parseForkVersion(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestForkVersionOrder(t *testing.T) {
	mustParse := func(s string) forkVersion {
		v, ok := parseForkVersion(s)
		if !ok {
			t.Fatalf("parseForkVersion(%q) failed", s)
		}
		return v
	}

	cases := []struct {
		a, b string
		want bool
	}{
		{"v2.1.4-desuq.2", "v2.1.4-desuq.1", true},
		{"v2.1.4-desuq.1", "v2.1.4-desuq.2", false},
		{"v2.1.4-desuq.1", "v2.1.4-desuq.1", false},
		{"v2.1.4-desuq.10", "v2.1.4-desuq.9", true},
		{"v2.2.0-desuq.1", "v2.1.4-desuq.9", true},
		{"v3.0.0-desuq.1", "v2.9.9-desuq.9", true},

		// A build made partway between two tags sorts after the tag it came
		// from and before the next one, which is what git describe means.
		{"v2.1.4-desuq.1-23-gd6c6dde6", "v2.1.4-desuq.1", true},
		{"v2.1.4-desuq.2", "v2.1.4-desuq.1-23-gd6c6dde6", true},
		{"v2.1.4-desuq.1-23-gd6c6dde6", "v2.1.4-desuq.2", false},
	}

	for _, c := range cases {
		if got := mustParse(c.a).newerThan(mustParse(c.b)); got != c.want {
			t.Errorf("%s newerThan %s = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestNewestPeerAhead(t *testing.T) {
	t.Run("picks the newest peer that is ahead", func(t *testing.T) {
		got, ok := newestPeerAhead("v2.1.4-desuq.1", []peerVersion{
			{name: "amy", version: "v2.1.4-desuq.2"},
			{name: "ben", version: "v2.1.4-desuq.4"},
			{name: "cat", version: "v2.1.4-desuq.3"},
		})
		if !ok {
			t.Fatal("expected a peer ahead")
		}
		if got.name != "ben" || got.version != "v2.1.4-desuq.4" {
			t.Errorf("got %+v, want ben on desuq.4", got)
		}
	})

	t.Run("quiet when everybody is level or behind", func(t *testing.T) {
		if _, ok := newestPeerAhead("v2.1.4-desuq.3", []peerVersion{
			{name: "amy", version: "v2.1.4-desuq.3"},
			{name: "ben", version: "v2.1.4-desuq.1"},
		}); ok {
			t.Error("expected no peer ahead")
		}
	})

	// The rule the whole feature turns on. Upstream's release train runs ahead
	// of whatever base version this fork sits on, so a peer on stock Syncthing
	// would otherwise produce a permanent, unactionable "update available".
	t.Run("ignores a peer on stock Syncthing", func(t *testing.T) {
		if _, ok := newestPeerAhead("v2.1.4-desuq.1", []peerVersion{
			{name: "stock", version: "v2.9.0"},
			{name: "also stock", version: "syncthing v3.0.0"},
			{name: "android", version: "syncthing-android v2.5.0"},
		}); ok {
			t.Error("a non-fork peer must never raise an update notice")
		}
	})

	t.Run("quiet when this build is not the fork", func(t *testing.T) {
		if _, ok := newestPeerAhead("v2.1.4", []peerVersion{
			{name: "amy", version: "v2.1.4-desuq.9"},
		}); ok {
			t.Error("expected nothing to compare against")
		}
	})

	// The developer, running something they built themselves five minutes ago.
	t.Run("quiet on a dev build", func(t *testing.T) {
		if _, ok := newestPeerAhead("v2.1.4-desuq.1-23-gd6c6dde6-dirty", []peerVersion{
			{name: "amy", version: "v2.1.4-desuq.2"},
		}); ok {
			t.Error("a build made between tags must not be nagged")
		}
	})

	t.Run("empty and unparseable peer versions are skipped", func(t *testing.T) {
		if _, ok := newestPeerAhead("v2.1.4-desuq.1", []peerVersion{
			{name: "never connected", version: ""},
			{name: "nonsense", version: "???"},
		}); ok {
			t.Error("expected no peer ahead")
		}
	})

	t.Run("no peers at all", func(t *testing.T) {
		if _, ok := newestPeerAhead("v2.1.4-desuq.1", nil); ok {
			t.Error("expected no peer ahead")
		}
	})
}

// --- the alerter path, against the real REST shapes -----------------------

// fakeSyncthing serves the three endpoints checkPeerVersions reads, with the
// JSON field names Syncthing actually uses. It is here rather than in
// alerts_test.go's nil-client helper because the decoding is half of what can
// break: clientVersion has been served by upstream all along, but nothing else
// in the tray reads it.
func fakeSyncthing(t *testing.T, ourVersion string, peers map[string]string) *client {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/rest/system/version":
			w.Write([]byte(`{"version":"` + ourVersion + `"}`))
		case "/rest/system/connections":
			var b strings.Builder
			b.WriteString(`{"connections":{`)
			first := true
			for id, v := range peers {
				if !first {
					b.WriteString(",")
				}
				first = false
				b.WriteString(`"` + id + `":{"connected":true,"clientVersion":"` + v + `"}`)
			}
			b.WriteString(`}}`)
			w.Write([]byte(b.String()))
		case "/rest/config":
			var b strings.Builder
			b.WriteString(`{"devices":[`)
			first := true
			for id := range peers {
				if !first {
					b.WriteString(",")
				}
				first = false
				b.WriteString(`{"deviceID":"` + id + `","name":"` + id + `-name"}`)
			}
			b.WriteString(`],"folders":[]}`)
			w.Write([]byte(b.String()))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	return newClient(endpoint{baseURL: srv.URL, apiKey: "test"})
}

func TestCheckPeerVersionsToastsOnceThenCoolsDown(t *testing.T) {
	rec := &recorder{}
	cl := fakeSyncthing(t, "v2.1.4-desuq.1", map[string]string{
		"AAAAAAA-BBBBBBB": "v2.1.4-desuq.3",
	})
	a := newAlerter(rec, func() string { return "http://127.0.0.1:8384/" },
		func() *client { return cl })

	a.checkPeerVersions()
	if rec.count() != 1 {
		t.Fatalf("expected one notification, got %d", rec.count())
	}
	n := rec.sent[0]
	if n.Title != "An update is available" {
		t.Errorf("title = %q", n.Title)
	}
	if !strings.Contains(n.Body, "v2.1.4-desuq.3") || !strings.Contains(n.Body, "v2.1.4-desuq.1") {
		t.Errorf("body should name both versions, got %q", n.Body)
	}
	if !strings.Contains(n.Body, "AAAAAAA-BBBBBBB-name") {
		t.Errorf("body should name the device, got %q", n.Body)
	}
	if n.Launch != releasesURL {
		t.Errorf("launch = %q, want the releases page", n.Launch)
	}

	// Every reconnect calls this. It must not toast again.
	a.checkPeerVersions()
	a.checkPeerVersions()
	if rec.count() != 1 {
		t.Fatalf("expected the cooldown to hold, got %d notifications", rec.count())
	}

	// A newer version still gets through inside the cooldown.
	a.mu.Lock()
	a.naggedVersion = "v2.1.4-desuq.9"
	a.lastNag = time.Now()
	a.mu.Unlock()
	a.checkPeerVersions()
	if rec.count() != 2 {
		t.Errorf("a different version should notify, got %d", rec.count())
	}
}

func TestCheckPeerVersionsSilentWhenLevel(t *testing.T) {
	rec := &recorder{}
	cl := fakeSyncthing(t, "v2.1.4-desuq.3", map[string]string{
		"AAAAAAA-BBBBBBB": "v2.1.4-desuq.3",
		"CCCCCCC-DDDDDDD": "v2.9.0", // stock Syncthing, deliberately ahead
	})
	a := newAlerter(rec, func() string { return "http://127.0.0.1:8384/" },
		func() *client { return cl })

	a.checkPeerVersions()
	if rec.count() != 0 {
		t.Errorf("expected silence, got %v", rec.titles())
	}
}

// A tray whose Syncthing has gone away must not panic or toast.
func TestCheckPeerVersionsSurvivesNoClient(t *testing.T) {
	a, rec := testAlerter()
	a.checkPeerVersions()
	if rec.count() != 0 {
		t.Errorf("expected silence, got %v", rec.titles())
	}
}
