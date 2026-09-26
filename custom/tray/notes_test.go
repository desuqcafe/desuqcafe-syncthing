package main

import (
	"strings"
	"testing"
	"time"
)

func TestNotesMemoAnnouncesEachNoteOnce(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 9, 26, 9, 0, 0, 0, time.UTC)
	m := loadNotesMemo(home, now)

	kai := "KAI-ID"
	rows := []noteRow{
		// Written last night while this computer was off, arriving this
		// morning: older than the tray, and still news.
		{Device: kai, Name: "Kai", Path: "Scenes/cabin.blend", Text: "moved the camera", At: now.Add(-9 * time.Hour), Current: true},
		// Older than a computer that has never run before looks.
		{Device: kai, Name: "Kai", Path: "old.blend", Text: "ancient", At: now.Add(-3 * 24 * time.Hour), Current: true},
		// Your own: never announced to you.
		{Device: "ME", Name: "You", Mine: true, Path: "a.blend", Text: "mine", At: now, Current: true},
	}
	fresh := m.fresh(rows)
	if len(fresh) != 1 || fresh[0].Path != "Scenes/cabin.blend" {
		t.Fatalf("first pass announced %+v", fresh)
	}
	if again := m.fresh(rows); len(again) != 0 {
		t.Fatalf("second pass announced %+v again", again)
	}

	// A restart reads the memo back rather than starting over.
	m2 := loadNotesMemo(home, now.Add(time.Hour))
	if again := m2.fresh(rows); len(again) != 0 {
		t.Fatalf("after a restart, announced %+v again", again)
	}

	// A newer note is news; one about a version already replaced moves the
	// mark without a toast, so it cannot surface later either.
	rows = append(rows,
		noteRow{Device: kai, Name: "Kai", Path: "Scenes/cabin.blend", Text: "superseded", At: now.Add(30 * time.Minute), Current: false},
		noteRow{Device: kai, Name: "Kai", Path: "Scenes/cabin.blend", Text: "lighting back", At: now.Add(time.Hour), Current: true},
	)
	fresh = m2.fresh(rows)
	if len(fresh) != 1 || fresh[0].Text != "lighting back" {
		t.Fatalf("third pass announced %+v", fresh)
	}
}

func TestNotesMemoIsPerAuthor(t *testing.T) {
	now := time.Date(2026, 9, 26, 9, 0, 0, 0, time.UTC)
	m := loadNotesMemo("", now)
	// Mia's clock is an hour slow. Kai's later note must not hide hers.
	m.fresh([]noteRow{{Device: "KAI", Name: "Kai", Path: "a.blend", Text: "x", At: now, Current: true}})
	fresh := m.fresh([]noteRow{
		{Device: "KAI", Name: "Kai", Path: "a.blend", Text: "x", At: now, Current: true},
		{Device: "MIA", Name: "Mia", Path: "b.blend", Text: "y", At: now.Add(-time.Hour), Current: true},
	})
	if len(fresh) != 1 || fresh[0].Name != "Mia" {
		t.Fatalf("announced %+v; want Mia's note despite her clock", fresh)
	}
}

func TestNoteAnnouncement(t *testing.T) {
	one := []noteRow{{Name: "Kai", Label: "Shared Art", Path: "Scenes/cabin.blend", Text: "moved the camera", Here: false}}
	title, body := noteAnnouncement(one)
	if title != "Kai changed cabin.blend" {
		t.Errorf("title %q", title)
	}
	if !strings.Contains(body, "moved the camera") || !strings.Contains(body, "Shared Art") || !strings.Contains(body, "on its way") {
		t.Errorf("body %q", body)
	}
	one[0].Here = true
	if _, body = noteAnnouncement(one); strings.Contains(body, "on its way") {
		t.Errorf("a version that has arrived is not on its way: %q", body)
	}

	many := []noteRow{
		{Name: "Kai", Path: "a.blend", Text: "one"},
		{Name: "Kai", Path: "b.blend", Text: "two"},
		{Name: "Kai", Path: "c.blend", Text: "three"},
		{Name: "Kai", Path: "d.blend", Text: "four"},
	}
	title, body = noteAnnouncement(many)
	if title != "Kai said why they changed 4 files" || !strings.HasSuffix(body, "and 1 more") {
		t.Errorf("many: %q / %q", title, body)
	}
	many[3].Name = "Mia"
	if title, _ = noteAnnouncement(many); title != "4 new notes on changed files" {
		t.Errorf("mixed authors: %q", title)
	}
}

func TestTruncateKeepsCharactersWhole(t *testing.T) {
	s := "カメラを動かした、照明はそのまま"
	got := truncate(s, 6)
	if got != "カメラを動…" {
		t.Fatalf("truncate = %q", got)
	}
	if truncate("short", 60) != "short" {
		t.Fatal("a short string should come back as it was")
	}
}

func TestLatestNote(t *testing.T) {
	now := time.Now()
	rows := []noteRow{
		{Text: "old but replaced", At: now, Current: false},
		{Text: "current, earlier", At: now.Add(-time.Hour), Current: true},
		{Text: "current, later", At: now.Add(-time.Minute), Current: true},
	}
	n, ok := latestNote(rows)
	if !ok || n.Text != "current, later" {
		t.Fatalf("latestNote = %+v %v", n, ok)
	}
	if _, ok := latestNote(rows[:1]); ok {
		t.Fatal("a note about a replaced version is not the file's latest")
	}
}
