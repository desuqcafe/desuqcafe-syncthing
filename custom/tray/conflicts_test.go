package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// touch writes a file and stamps it, so "was this written after the tray
// started" can be asked of a real stat rather than a fake clock.
func touch(t *testing.T, path string, age time.Duration) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
	return path
}

// stamped names a conflict copy the way Syncthing would have, age ago. The
// stamp, not the file's mtime, is what says when a conflict happened.
func stamped(stem string, age time.Duration) string {
	return stem + conflictMarker + time.Now().Add(-age).Format(conflictStampLayout) + "-K3PLM9Q.blend"
}

func TestOriginalNameStripsTheGeneratedPart(t *testing.T) {
	cases := map[string]string{
		"scene.sync-conflict-20260824-142233-K3PLM9Q.blend": "scene.blend",
		"notes.sync-conflict-20260824-142233-K3PLM9Q":       "notes",
		"a.b.sync-conflict-20260101-000000-AAAAAAA.tar.gz":  "a.b.tar.gz",
		"nothing-to-strip.blend":                            "nothing-to-strip.blend",
	}
	for in, want := range cases {
		if got := originalName(in); got != want {
			t.Errorf("originalName(%q) = %q, want %q", in, got, want)
		}
	}
}

// The walk has to find conflicts anywhere in the tree, and has to not find the
// archived ones -- .stversions is full of old conflicts by construction, and
// announcing those would announce the archive rather than the event.
func TestConflictsInSkipsSyncthingsOwnDirectories(t *testing.T) {
	root := t.TempDir()
	want := touch(t, filepath.Join(root, "models", "scene.sync-conflict-20260824-142233-K3PLM9Q.blend"), 0)
	touch(t, filepath.Join(root, "models", "scene.blend"), 0)
	touch(t, filepath.Join(root, ".stversions", "old.sync-conflict-20260101-000000-AAAAAAA.blend"), 0)
	touch(t, filepath.Join(root, ".stfolder", "x.sync-conflict-20260101-000000-AAAAAAA"), 0)

	got := conflictsIn(root, "Models")
	if len(got) != 1 {
		t.Fatalf("found %d conflicts, want 1: %v", len(got), got)
	}
	if got[0].path != want {
		t.Errorf("found %q, want %q", got[0].path, want)
	}
	if got[0].folder != "Models" {
		t.Errorf("folder = %q, want the display name", got[0].folder)
	}
}

func TestConflictsInSurvivesAMissingFolder(t *testing.T) {
	if got := conflictsIn(filepath.Join(t.TempDir(), "not-plugged-in"), "External"); len(got) != 0 {
		t.Fatalf("a missing folder produced %d conflicts, want 0", len(got))
	}
}

// The rule that keeps this from being a nuisance: a conflict that was already
// there when the tray started is somebody's existing situation, and being told
// about it at every sign-in is how people learn to ignore notifications.
func TestOldConflictsAreNotAnnounced(t *testing.T) {
	root := t.TempDir()
	old := touch(t, filepath.Join(root, stamped("old", 48*time.Hour)), 48*time.Hour)
	fresh := touch(t, filepath.Join(root, stamped("new", 0)), 0)

	a, _ := testAlerter()
	got := a.freshConflicts([]conflictFile{
		{folder: "Models", path: old, name: filepath.Base(old)},
		{folder: "Models", path: fresh, name: filepath.Base(fresh)},
	})
	if len(got) != 1 || got[0].path != fresh {
		t.Fatalf("announced %v, want only the new one", got)
	}
}

// A conflict written just before the tray came up is still news: Syncthing is
// started by this process and pulls straight away.
func TestConflictInsideTheGracePeriodIsAnnounced(t *testing.T) {
	root := t.TempDir()
	p := touch(t, filepath.Join(root, stamped("x", conflictGrace/2)), conflictGrace/2)

	a, _ := testAlerter()
	if got := a.freshConflicts([]conflictFile{{folder: "M", path: p, name: filepath.Base(p)}}); len(got) != 1 {
		t.Fatalf("a conflict %v old was not announced", conflictGrace/2)
	}
}

func TestEachConflictIsAnnouncedOnce(t *testing.T) {
	root := t.TempDir()
	p := touch(t, filepath.Join(root, stamped("x", 0)), 0)
	list := []conflictFile{{folder: "M", path: p, name: filepath.Base(p)}}

	a, _ := testAlerter()
	if got := a.freshConflicts(list); len(got) != 1 {
		t.Fatalf("first pass announced %d, want 1", len(got))
	}
	for range 5 {
		if got := a.freshConflicts(list); len(got) != 0 {
			t.Fatalf("a later pass announced the same conflict again")
		}
	}
}

// Resolving a conflict and hitting the same filename again is a second event,
// not a repeat of the first.
func TestAResolvedConflictCanNotifyAgain(t *testing.T) {
	root := t.TempDir()
	p := touch(t, filepath.Join(root, stamped("x", 0)), 0)
	list := []conflictFile{{folder: "M", path: p, name: filepath.Base(p)}}

	a, _ := testAlerter()
	a.freshConflicts(list)
	// The user deletes it.
	a.freshConflicts(nil)
	if len(a.seenConflicts) != 0 {
		t.Fatalf("a conflict that is gone is still remembered: %v", a.seenConflicts)
	}
	// And it happens again.
	touch(t, p, 0)
	if got := a.freshConflicts(list); len(got) != 1 {
		t.Fatalf("the second conflict was swallowed")
	}
}

// The offline case. The edit was made on Monday with the other computer off,
// the tray restarted on Tuesday, and the two met on Tuesday afternoon: the
// copy keeps Monday's mtime, but the conflict is new. Judged by mtime, as it
// was until 2026-09-26, this was never announced.
func TestAConflictOverAnOldEditIsStillNews(t *testing.T) {
	root := t.TempDir()
	p := touch(t, filepath.Join(root, stamped("scene", 0)), 30*time.Hour)

	a, _ := testAlerter()
	if got := a.freshConflicts([]conflictFile{{folder: "M", path: p, name: filepath.Base(p)}}); len(got) != 1 {
		t.Fatalf("a conflict made just now over a day-old edit was not announced")
	}
}

// And the other way round: a new mtime does not make an old conflict news.
// Touching a conflict copy -- opening it to compare -- must not re-announce it
// at the next sign-in.
func TestATouchedOldConflictIsNotNews(t *testing.T) {
	root := t.TempDir()
	p := touch(t, filepath.Join(root, stamped("scene", 48*time.Hour)), 0)

	a, _ := testAlerter()
	if got := a.freshConflicts([]conflictFile{{folder: "M", path: p, name: filepath.Base(p)}}); len(got) != 0 {
		t.Fatalf("a two-day-old conflict was announced because its file was touched")
	}
}

func TestConflictTimeReadsTheStamp(t *testing.T) {
	got, ok := conflictTime("scene.sync-conflict-20260926-150211-OHQN3WH.blend")
	want := time.Date(2026, 9, 26, 15, 2, 11, 0, time.Local)
	if !ok || !got.Equal(want) {
		t.Errorf("conflictTime = %v, %v; want %v", got, ok, want)
	}
	for _, bad := range []string{"scene.blend", "scene.sync-conflict-2026.blend", "scene.sync-conflict-garbage-xx-K.blend"} {
		if _, ok := conflictTime(bad); ok {
			t.Errorf("conflictTime(%q) claimed a stamp", bad)
		}
	}
}

// Edit against delete: the delete keeps the name and the edit survives only
// as the copy (verified on a pair, 2026-09-26). The toast must not talk about
// "a second copy" of a file that is no longer there.
func TestConflictNotificationForADeletedOriginal(t *testing.T) {
	root := t.TempDir()
	p := touch(t, filepath.Join(root, stamped("texture2", 0)), 0)
	found := conflictsIn(root, "Project Assets")
	if len(found) != 1 || !found[0].gone {
		t.Fatalf("a copy with no original beside it was not seen as gone: %+v", found)
	}
	touch(t, filepath.Join(root, "texture2.blend"), 0)
	if again := conflictsIn(root, "Project Assets"); again[0].gone {
		t.Fatalf("a copy with its original beside it was seen as gone")
	}

	n := conflictNotification([]conflictFile{{folder: "Project Assets", path: p, name: filepath.Base(p), gone: true}}, "")
	if !strings.Contains(n.Title, "deleted") || strings.Contains(n.Body, "second cop") {
		t.Errorf("deleted-original toast reads wrong: %q / %q", n.Title, n.Body)
	}
	if !strings.Contains(n.Body, "texture2.blend") || !strings.Contains(n.Body, "Rename it back") {
		t.Errorf("deleted-original toast does not say what to do: %q", n.Body)
	}
}

// The toast has one job: make somebody who does not know what a conflict is
// understand that their afternoon may be in the other file.
func TestConflictNotificationNamesTheRealFile(t *testing.T) {
	n := conflictNotification([]conflictFile{{
		folder: "Models",
		path:   `C:\Models\scene.sync-conflict-20260824-142233-K3PLM9Q.blend`,
		name:   "scene.sync-conflict-20260824-142233-K3PLM9Q.blend",
	}}, "http://127.0.0.1:8384/")

	if strings.Contains(n.Body, "K3PLM9Q") {
		t.Errorf("the toast reads out the generated name: %q", n.Body)
	}
	for _, want := range []string{"scene.blend", "Models", "sync-conflict"} {
		if !strings.Contains(n.Body, want) {
			t.Errorf("the toast never mentions %q: %q", want, n.Body)
		}
	}
	if n.Launch == "" {
		t.Error("the toast is not clickable")
	}
}

func TestConflictNotificationCountsPastTwo(t *testing.T) {
	var fresh []conflictFile
	for _, base := range []string{"a", "b", "c", "d"} {
		fresh = append(fresh, conflictFile{
			folder: "Models",
			name:   base + ".sync-conflict-20260824-142233-K3PLM9Q.blend",
			path:   `C:\Models\` + base + ".sync-conflict-20260824-142233-K3PLM9Q.blend",
		})
	}
	body := conflictNotification(fresh, "").Body
	if !strings.Contains(body, "2 other files") {
		t.Errorf("four conflicts did not become two names and a count: %q", body)
	}
}

// Two folders means the toast cannot say "in X", because it would be lying
// about half of them.
func TestConflictNotificationOmitsTheFolderWhenThereAreSeveral(t *testing.T) {
	body := conflictNotification([]conflictFile{
		{folder: "Models", name: "a.sync-conflict-20260824-142233-K3PLM9Q.blend", path: "a"},
		{folder: "Textures", name: "b.sync-conflict-20260824-142233-K3PLM9Q.png", path: "b"},
	}, "").Body
	if strings.Contains(body, " in Models") || strings.Contains(body, " in Textures") {
		t.Errorf("the toast claims one folder for conflicts in two: %q", body)
	}
}
