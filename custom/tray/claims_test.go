package main

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNewPeerClaims(t *testing.T) {
	start := time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)
	cutoff := start.Add(-claimsGrace)
	seen := map[string]bool{}

	yuki := claimRow{Folder: "assets", Label: "Project Assets", Path: "Scenes/cabin.blend",
		Device: "YUKI", Name: "Yuki", Since: start.Add(time.Minute)}
	mine := claimRow{Folder: "assets", Path: "texture1.png", Device: "ME", Name: "You", Mine: true, Since: start}
	old := claimRow{Folder: "assets", Path: "old.blend", Device: "YUKI", Name: "Yuki", Since: start.Add(-72 * time.Hour)}

	got := newPeerClaims([]claimRow{yuki, mine, old}, seen, cutoff)
	if len(got) != 1 || got[0].Path != "Scenes/cabin.blend" {
		t.Fatalf("first pass: %+v", got)
	}
	// Announced once.
	if got := newPeerClaims([]claimRow{yuki, mine, old}, seen, cutoff); len(got) != 0 {
		t.Errorf("announced twice: %+v", got)
	}
	// Released, then claimed again: news again.
	newPeerClaims([]claimRow{mine, old}, seen, cutoff)
	if got := newPeerClaims([]claimRow{yuki}, seen, cutoff); len(got) != 1 {
		t.Errorf("a re-claim was not announced: %+v", got)
	}
	// A stale claim is never news, however it arrives.
	stale := yuki
	stale.Path, stale.Stale = "forgotten.blend", true
	if got := newPeerClaims([]claimRow{yuki, stale}, seen, cutoff); len(got) != 0 {
		t.Errorf("a stale claim was announced: %+v", got)
	}
}

func TestClaimAnnouncement(t *testing.T) {
	one := []claimRow{{Label: "Project Assets", Path: "Scenes/cabin.blend", Name: "Yuki"}}
	title, body := claimAnnouncement(one)
	if title != "Yuki is working on cabin.blend" || !strings.Contains(body, "Project Assets") {
		t.Errorf("one: %q / %q", title, body)
	}

	three := []claimRow{
		{Path: "a.blend", Name: "Yuki"}, {Path: "b.blend", Name: "Yuki"}, {Path: "c.blend", Name: "Yuki"},
	}
	title, body = claimAnnouncement(three)
	if title != "Yuki is working on 3 files" || !strings.Contains(body, "a.blend, b.blend and 1 more") {
		t.Errorf("three: %q / %q", title, body)
	}

	mixed := []claimRow{{Path: "a.blend", Name: "Yuki"}, {Path: "b.blend", Name: "Ana"}}
	if title, _ := claimAnnouncement(mixed); title != "2 files are being worked on" {
		t.Errorf("mixed: %q", title)
	}
}

func TestCollisions(t *testing.T) {
	rows := []claimRow{
		{Folder: "assets", Path: "Scenes/cabin.blend", Name: "Yuki"},
		{Folder: "assets", Path: "mine.blend", Name: "You", Mine: true},
	}
	ev := func(typ, folder, path, kind string) diskEvent {
		var e diskEvent
		e.Type, e.Data.Folder, e.Data.Path, e.Data.Type = typ, folder, path, kind
		return e
	}
	evs := []diskEvent{
		// The disk feed uses the OS separator.
		ev("LocalChangeDetected", "assets", filepath.FromSlash("Scenes/cabin.blend"), "file"),
		ev("LocalChangeDetected", "assets", filepath.FromSlash("Scenes/cabin.blend"), "file"),
		// Somebody else's change arriving is not a collision here.
		ev("RemoteChangeDetected", "assets", "Scenes/cabin.blend", "file"),
		// My own claim is mine to edit.
		ev("LocalChangeDetected", "assets", "mine.blend", "file"),
		ev("LocalChangeDetected", "other", "Scenes/cabin.blend", "file"),
		ev("LocalChangeDetected", "assets", "Scenes", "dir"),
	}
	hits := collisions(evs, rows)
	if len(hits) != 1 || hits[0].Path != "Scenes/cabin.blend" {
		t.Errorf("hits: %+v", hits)
	}
}

func TestWithClaimsException(t *testing.T) {
	block := []string{
		"(?d)*.blend1",
		selectiveBegin,
		"*",
		"//// desuqcafe selective sync -- end",
	}
	got, changed := withClaimsException(block)
	if !changed {
		t.Fatal("a held-back folder was not given the exception")
	}
	want := []string{"(?d)*.blend1", claimsExceptionComment, claimsException, selectiveBegin, "*",
		"//// desuqcafe selective sync -- end"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// Idempotent.
	if _, changed := withClaimsException(got); changed {
		t.Error("added twice")
	}
	// A folder that ignores specific things does not need it.
	if _, changed := withClaimsException([]string{"(?d)*.tmp", "/Renders"}); changed {
		t.Error("added to a folder that does not hold everything back")
	}
	// Somebody's own line about the directory, either way round, stands.
	if _, changed := withClaimsException([]string{"/.desuq-claims", "*"}); changed {
		t.Error("overruled an explicit line")
	}
	// A bare catch-all with no picker block still gets it, above the "*".
	got, _ = withClaimsException([]string{"*"})
	if len(got) != 3 || got[1] != claimsException || got[2] != "*" {
		t.Errorf("bare catch-all: %q", got)
	}
}

func TestFolderForPath(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("paths below are Windows paths")
	}
	folders := []restFolder{
		{ID: "assets", Path: `C:\Users\yuki\Assets`},
		{ID: "renders", Path: `C:\Users\yuki\Assets\Renders`},
		{ID: "other", Path: `D:\Other`},
	}
	cases := []struct {
		in, id, rel string
		ok          bool
	}{
		{`C:\Users\yuki\Assets\Scenes\cabin.blend`, "assets", "Scenes/cabin.blend", true},
		// The nested folder wins over its parent.
		{`C:\Users\yuki\Assets\Renders\shot01.png`, "renders", "shot01.png", true},
		// Case does not matter on Windows.
		{`c:\users\YUKI\assets\texture.png`, "assets", "texture.png", true},
		// A sibling whose name starts the same is not inside.
		{`C:\Users\yuki\AssetsBackup\x.blend`, "", "", false},
		{`C:\Users\yuki\Assets`, "", "", false},
		{`E:\nowhere.blend`, "", "", false},
	}
	for _, c := range cases {
		f, rel, ok := folderForPath(folders, c.in)
		if ok != c.ok || f.ID != c.id || rel != c.rel {
			t.Errorf("folderForPath(%q) = %q %q %v; want %q %q %v", c.in, f.ID, rel, ok, c.id, c.rel, c.ok)
		}
	}
}
