package main

import (
	"strings"
	"testing"
	"time"
)

func dreply(folder string, peers ...deliveryPeer) deliveryReply {
	return deliveryReply{Folder: folder, Peers: peers}
}

func dpeer(dev, name string, files int, names ...string) deliveryPeer {
	p := deliveryPeer{Device: dev, Name: name, Names: names}
	p.Yours.Files = files
	return p
}

func TestDeliveryTrackerQuietForQuickDeliveries(t *testing.T) {
	var tr deliveryTracker
	t0 := time.Date(2026, 9, 26, 14, 0, 0, 0, time.UTC)

	// Missing for one poll, then there: an ordinary save reaching a
	// connected peer. Not news.
	if done := tr.observe(t0, []deliveryReply{dreply("f", dpeer("T", "Kai", 1, "cabin.blend"))}); len(done) != 0 {
		t.Fatalf("announced on first sight: %+v", done)
	}
	if done := tr.observe(t0.Add(time.Minute), []deliveryReply{dreply("f", dpeer("T", "Kai", 0))}); len(done) != 0 {
		t.Fatalf("announced a one-minute delivery: %+v", done)
	}
	if len(tr.pending) != 0 {
		t.Fatalf("pending left behind: %+v", tr.pending)
	}
}

func TestDeliveryTrackerAnnouncesAWaitedDelivery(t *testing.T) {
	var tr deliveryTracker
	t0 := time.Date(2026, 9, 26, 14, 0, 0, 0, time.UTC)
	f := func(n int, names ...string) []deliveryReply {
		return []deliveryReply{dreply("assets", dpeer("T", "Kai", n, names...), dpeer("M", "Mia", 0))}
	}
	tr.observe(t0, f(3, `Scenes\cabin.blend`, "tree.blend", "rock.blend"))
	tr.observe(t0.Add(time.Minute), f(1, "rock.blend")) // part-way through
	done := tr.observe(t0.Add(3*time.Minute), f(0))
	if len(done) != 1 {
		t.Fatalf("want one completed delivery, got %+v", done)
	}
	d := done[0]
	// The delivery as a whole, not what was left in the last minute.
	if d.files != 3 || d.names[0] != `Scenes\cabin.blend` || d.name != "Kai" {
		t.Fatalf("got %+v", d)
	}
	title, body := deliveredMessage(d.name, done)
	if title != "Now on Kai's computer" {
		t.Errorf("title %q", title)
	}
	if body != "cabin.blend and 2 more of your files have reached Kai." {
		t.Errorf("body %q", body)
	}
}

func TestDeliveryTrackerDropsVanishedPeers(t *testing.T) {
	var tr deliveryTracker
	t0 := time.Now()
	tr.observe(t0, []deliveryReply{dreply("f", dpeer("T", "Kai", 2))})
	// The folder is no longer reported at all -- unshared, or paused.
	if done := tr.observe(t0.Add(time.Hour), nil); len(done) != 0 {
		t.Fatalf("announced a delivery nobody saw: %+v", done)
	}
	if len(tr.pending) != 0 {
		t.Fatal("pending kept for a folder nobody reports")
	}
}

func TestDeliveredMessage(t *testing.T) {
	one := []pendingDelivery{{files: 1, names: []string{"Scenes/cabin.blend"}}}
	if _, b := deliveredMessage("Kai", one); b != "cabin.blend has reached Kai." {
		t.Errorf("one: %q", b)
	}
	two := []pendingDelivery{{files: 2, names: []string{"a.blend", "b.png"}}}
	if _, b := deliveredMessage("Kai", two); b != "a.blend and b.png have reached Kai." {
		t.Errorf("two: %q", b)
	}
	// Two folders to the same person are one toast.
	multi := []pendingDelivery{{files: 1, names: []string{"a.blend"}}, {files: 4}}
	if _, b := deliveredMessage("Kai", multi); b != "a.blend and 4 more of your files have reached Kai." {
		t.Errorf("multi: %q", b)
	}
	if title, _ := deliveredMessage("Kai", nil); title != "" {
		t.Error("nothing delivered still made a toast")
	}
}

func TestPausedDeliveryMessage(t *testing.T) {
	if m := pausedDeliveryMessage([]deliveryReply{dreply("f", dpeer("T", "Kai", 0))}); m != "" {
		t.Errorf("everything delivered: %q", m)
	}
	m := pausedDeliveryMessage([]deliveryReply{
		dreply("a", dpeer("T", "Kai", 1, `Scenes\cabin.blend`), dpeer("M", "Mia", 2, "x.png", "y.png")),
		dreply("b", dpeer("T", "Kai", 1, "z.png")),
	})
	for _, want := range []string{
		"Mia does not have x.png and 1 more of your files yet",
		"Kai does not have cabin.blend and 1 more of your files yet",
		"Pausing keeps it from reaching them until you resume.",
	} {
		if !strings.Contains(m, want) {
			t.Errorf("missing %q in %q", want, m)
		}
	}
	single := pausedDeliveryMessage([]deliveryReply{dreply("a", dpeer("T", "Kai", 1, "cabin.blend"))})
	if !strings.HasPrefix(single, "Kai does not have your latest cabin.blend yet.") {
		t.Errorf("single: %q", single)
	}
}
