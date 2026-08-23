package main

import (
	"sync"
	"testing"
	"time"
)

// recorder stands in for the OS notifier so the policy can be tested without
// putting toasts on the developer's screen.
type recorder struct {
	mu   sync.Mutex
	sent []Notification
}

func (r *recorder) Notify(n Notification) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, n)
}
func (r *recorder) Close() {}

func (r *recorder) titles() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.sent))
	for i, n := range r.sent {
		out[i] = n.Title
	}
	return out
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sent)
}

func testAlerter() (*alerter, *recorder) {
	rec := &recorder{}
	// current returns nil: every path that would call Syncthing has to cope
	// with that, which is also what happens when Syncthing is restarting.
	return newAlerter(rec, func() string { return "http://127.0.0.1:8384/" },
		func() *client { return nil }), rec
}

// The whole point of the policy is that an idle folder saying "I am idle" over
// and over is not news.
func TestIdleFolderNeverNotifies(t *testing.T) {
	a, rec := testAlerter()
	for range 50 {
		a.folderProgress("assets", "idle", 0, 0, 0)
	}
	a.announceFinished()
	if n := rec.count(); n != 0 {
		t.Fatalf("an idle folder produced %d notifications, want 0", n)
	}
}

// A folder that falls behind and catches up is exactly one notification, no
// matter how many progress reports it made on the way.
func TestSyncCompleteFiresOncePerCatchUp(t *testing.T) {
	a, rec := testAlerter()

	for _, need := range []int64{500_000_000, 300_000_000, 120_000_000, 1_000} {
		a.folderProgress("assets", "syncing", need, 4, 0)
	}
	a.folderProgress("assets", "idle", 0, 0, 0)

	if rec.count() != 0 {
		t.Fatal("notified before the settle delay elapsed")
	}
	a.announceFinished()

	if got := rec.count(); got != 1 {
		t.Fatalf("got %d notifications, want 1: %v", got, rec.titles())
	}
	n := rec.sent[0]
	if n.Title != "Sync complete" {
		t.Errorf("title = %q", n.Title)
	}
	// The high-water mark is what was received, not the last figure seen.
	if want := "500.0 MB received"; !contains(n.Body, want) {
		t.Errorf("body = %q, want it to mention %q", n.Body, want)
	}
	if n.Launch == "" {
		t.Error("notification is not click-through")
	}
}

// Several folders finishing together are one toast, not one each.
func TestSyncCompleteCoalescesFolders(t *testing.T) {
	a, rec := testAlerter()

	for _, f := range []string{"assets", "textures", "scenes"} {
		a.folderProgress(f, "syncing", 100_000_000, 2, 0)
		a.folderProgress(f, "idle", 0, 0, 0)
	}
	a.announceFinished()

	if got := rec.count(); got != 1 {
		t.Fatalf("got %d notifications, want 1: %v", got, rec.titles())
	}
	body := rec.sent[0].Body
	if !contains(body, "and 2 others") || !contains(body, "300.0 MB") {
		t.Errorf("body = %q, want all three folders and the total", body)
	}
}

// A folder that reaches zero and immediately goes again has not finished.
func TestSyncCompleteSuppressedIfFolderGoesAgain(t *testing.T) {
	a, rec := testAlerter()

	a.folderProgress("assets", "syncing", 100_000_000, 2, 0)
	a.folderProgress("assets", "idle", 0, 0, 0)
	// More changes arrive before the settle delay expires.
	a.folderProgress("assets", "syncing", 50_000_000, 1, 0)
	a.announceFinished()

	if got := rec.count(); got != 0 {
		t.Fatalf("got %d notifications, want 0: %v", got, rec.titles())
	}
}

func TestSyncCompleteRespectsCooldown(t *testing.T) {
	a, rec := testAlerter()

	a.folderProgress("assets", "syncing", 10_000_000, 1, 0)
	a.folderProgress("assets", "idle", 0, 0, 0)
	a.announceFinished()

	// A second full round immediately afterwards is inside the cooldown.
	a.folderProgress("assets", "syncing", 10_000_000, 1, 0)
	a.folderProgress("assets", "idle", 0, 0, 0)
	a.announceFinished()

	if got := rec.count(); got != 1 {
		t.Fatalf("got %d notifications, want 1: %v", got, rec.titles())
	}
}

func TestFolderErrorNotifiesOnceThenCoolsDown(t *testing.T) {
	a, rec := testAlerter()

	for range 10 {
		a.folderErrored("assets", "permission denied writing scene.blend")
	}
	if got := rec.count(); got != 1 {
		t.Fatalf("got %d notifications, want 1", got)
	}
	if !contains(rec.sent[0].Body, "permission denied") {
		t.Errorf("body = %q, want the underlying error", rec.sent[0].Body)
	}

	// Recovering clears the cooldown, so a later failure is news again.
	a.folderProgress("assets", "idle", 0, 0, 0)
	a.folderErrored("assets", "disk full")

	if got := rec.count(); got != 2 {
		t.Fatalf("after recovery got %d notifications, want 2: %v", got, rec.titles())
	}
}

// A folder in an error state must not also announce itself as up to date.
func TestErroredFolderDoesNotReportSyncComplete(t *testing.T) {
	a, rec := testAlerter()

	a.folderProgress("assets", "syncing", 100_000_000, 3, 0)
	a.folderProgress("assets", "idle", 0, 0, 2) // zero need, but two errors
	a.announceFinished()

	if got := rec.count(); got != 0 {
		t.Fatalf("got %d notifications, want 0: %v", got, rec.titles())
	}
}

func TestReconcileReportsOnlyNewKeys(t *testing.T) {
	a, _ := testAlerter()

	if got := a.reconcile(a.announcedDevices, []string{"A", "B"}); len(got) != 2 {
		t.Fatalf("first pass returned %v, want both", got)
	}
	if got := a.reconcile(a.announcedDevices, []string{"A", "B"}); len(got) != 0 {
		t.Fatalf("second pass returned %v, want none", got)
	}
	// B goes away and comes back: that is a new request and should notify.
	if got := a.reconcile(a.announcedDevices, []string{"A"}); len(got) != 0 {
		t.Fatalf("removal returned %v, want none", got)
	}
	got := a.reconcile(a.announcedDevices, []string{"A", "B"})
	if len(got) != 1 || got[0] != "B" {
		t.Fatalf("re-added returned %v, want [B]", got)
	}
}

func TestReserveBytes(t *testing.T) {
	const total = 1_000_000_000_000 // 1 TB
	for _, tc := range []struct {
		value float64
		unit  string
		want  int64
	}{
		{20, "GB", 20_000_000_000},
		{1, "%", 10_000_000_000},
		{500, "MB", 500_000_000},
		{1024, "", 1024},
		{0, "GB", 0},
	} {
		if got := reserveBytes(tc.value, tc.unit, total); got != tc.want {
			t.Errorf("reserveBytes(%v, %q) = %d, want %d", tc.value, tc.unit, got, tc.want)
		}
	}
}

func TestDiskWarningThresholdsAndHysteresis(t *testing.T) {
	const reserve = 20_000_000_000 // 20 GB, the seeded default
	a, rec := testAlerter()

	// Plenty of room: silent.
	a.judgeDrive("C:", 500_000_000_000, 1_000_000_000_000, reserve, "Assets")
	if rec.count() != 0 {
		t.Fatalf("warned with plenty of space: %v", rec.titles())
	}

	// Inside twice the reserve: nearly full.
	a.judgeDrive("C:", 30_000_000_000, 1_000_000_000_000, reserve, "Assets")
	if got := rec.count(); got != 1 {
		t.Fatalf("got %d notifications, want 1", got)
	}
	if rec.sent[0].Title != "Disk nearly full" {
		t.Errorf("title = %q", rec.sent[0].Title)
	}

	// Still low: the cooldown suppresses a repeat.
	a.judgeDrive("C:", 25_000_000_000, 1_000_000_000_000, reserve, "Assets")
	if got := rec.count(); got != 1 {
		t.Fatalf("repeated inside the cooldown: %d", got)
	}

	// Recovering clears the cooldown, so a later slide warns again.
	a.judgeDrive("C:", 500_000_000_000, 1_000_000_000_000, reserve, "Assets")
	a.judgeDrive("C:", 5_000_000_000, 1_000_000_000_000, reserve, "Assets")
	if got := rec.count(); got != 2 {
		t.Fatalf("got %d notifications, want 2: %v", got, rec.titles())
	}
	if rec.sent[1].Title != "Disk full - syncing has stopped" {
		t.Errorf("title = %q, want the full-disk wording", rec.sent[1].Title)
	}
}

// Two folders on one drive are one drive's worth of warning.
func TestDiskWarningIsPerDrive(t *testing.T) {
	a, rec := testAlerter()
	const reserve = 20_000_000_000

	a.judgeDrive("C:", 10_000_000_000, 500_000_000_000, reserve, "Assets")
	a.judgeDrive("C:", 10_000_000_000, 500_000_000_000, reserve, "Textures")
	a.judgeDrive("D:", 10_000_000_000, 500_000_000_000, reserve, "Renders")

	if got := rec.count(); got != 2 {
		t.Fatalf("got %d notifications, want one per drive: %v", got, rec.titles())
	}
}

func TestVolumeOf(t *testing.T) {
	if got := volumeOf(`C:\Users\Modeller\Assets`); got != "C:" {
		t.Errorf("volumeOf = %q, want C:", got)
	}
}

// The settle timer must actually fire on its own, not only when a test calls
// announceFinished directly.
func TestSettleTimerFires(t *testing.T) {
	a, rec := testAlerter()
	a.folderProgress("assets", "syncing", 1_000_000, 1, 0)
	a.folderProgress("assets", "idle", 0, 0, 0)

	deadline := time.Now().Add(syncSettleDelay + 5*time.Second)
	for time.Now().Before(deadline) {
		if rec.count() > 0 {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("the settle timer never fired")
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && stringIndex(haystack, needle) >= 0
}

func stringIndex(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
