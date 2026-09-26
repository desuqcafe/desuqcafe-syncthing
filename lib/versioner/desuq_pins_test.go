// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// desuqcafe fork: see desuq_pins.go.

package versioner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/syncthing/syncthing/lib/config"
	"github.com/syncthing/syncthing/lib/fs"
)

// usePinStore points the store at a fresh file for one test.
func usePinStore(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), pinsFileName)
	pins.mut.Lock()
	oldPath := pins.path
	pins.path = func() string { return path }
	pins.loaded = false
	pins.data = nil
	pins.mut.Unlock()
	t.Cleanup(func() {
		pins.mut.Lock()
		pins.path = oldPath
		pins.loaded = false
		pins.data = nil
		pins.mut.Unlock()
	})
	return path
}

func pinTestCfg(t *testing.T, vtype string, params map[string]string) config.FolderConfiguration {
	t.Helper()
	return config.FolderConfiguration{
		ID:             "pins",
		FilesystemType: config.FilesystemTypeBasic,
		Path:           t.TempDir(),
		Versioning: config.VersioningConfiguration{
			Type:   vtype,
			Params: params,
		},
	}
}

// writeVersion puts a tagged copy into the archive, as if archived at when.
func writeVersion(t *testing.T, vfs fs.Filesystem, name string, when time.Time) string {
	t.Helper()
	p := TagFilename(name, when.Format(TimeFormat))
	if err := vfs.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, vfs, p, when.Format(TimeFormat))
	return p
}

func exists(vfs fs.Filesystem, p string) bool {
	_, err := vfs.Lstat(p)
	return err == nil
}

func TestPinnedSurvivesSimpleCleanup(t *testing.T) {
	usePinStore(t)
	cfg := pinTestCfg(t, "simple", map[string]string{"keep": "1", "cleanoutDays": "30"})
	vfs := versionerFsFromFolderCfg(cfg)
	v := newSimple(cfg)

	now := time.Now().Truncate(time.Second)
	old := writeVersion(t, vfs, filepath.Join("Scenes", "cabin.blend"), now.Add(-60*24*time.Hour))
	mid := writeVersion(t, vfs, filepath.Join("Scenes", "cabin.blend"), now.Add(-2*time.Hour))
	newest := writeVersion(t, vfs, filepath.Join("Scenes", "cabin.blend"), now.Add(-time.Hour))

	if err := SetPinned(cfg, "Scenes/cabin.blend", now.Add(-60*24*time.Hour), true); err != nil {
		t.Fatal(err)
	}
	if err := v.Clean(context.Background()); err != nil {
		t.Fatal(err)
	}

	if !exists(vfs, old) {
		t.Error("pinned copy past both keep and cleanoutDays was removed")
	}
	if exists(vfs, mid) {
		t.Error("unpinned copy beyond keep=1 was kept; the pin should not change the rest")
	}
	if !exists(vfs, newest) {
		t.Error("newest copy was removed")
	}

	// Unpinning makes it ordinary again.
	if err := SetPinned(cfg, "Scenes/cabin.blend", now.Add(-60*24*time.Hour), false); err != nil {
		t.Fatal(err)
	}
	if err := v.Clean(context.Background()); err != nil {
		t.Fatal(err)
	}
	if exists(vfs, old) {
		t.Error("unpinned copy survived cleanup")
	}
}

func TestPinnedSurvivesStaggeredArchive(t *testing.T) {
	usePinStore(t)
	// maxAge of a day: anything older goes at the next Archive.
	cfg := pinTestCfg(t, "staggered", map[string]string{"maxAge": "86400"})
	vfs := versionerFsFromFolderCfg(cfg)
	ffs := cfg.Filesystem()
	v := newStaggered(cfg)

	now := time.Now().Truncate(time.Second)
	pinned := writeVersion(t, vfs, "tex.png", now.Add(-40*24*time.Hour))
	other := writeVersion(t, vfs, "tex.png", now.Add(-39*24*time.Hour))
	if err := SetPinned(cfg, "tex.png", now.Add(-40*24*time.Hour), true); err != nil {
		t.Fatal(err)
	}

	// Archive runs cleanVersions for that file -- the path a save takes, not
	// the periodic sweep.
	writeFile(t, ffs, "tex.png", "live")
	if err := v.Archive("tex.png"); err != nil {
		t.Fatal(err)
	}

	if !exists(vfs, pinned) {
		t.Error("pinned copy over maxAge was removed on archive")
	}
	if exists(vfs, other) {
		t.Error("unpinned copy over maxAge was kept")
	}
}

func TestPinnedSurvivesTrashcanCleanup(t *testing.T) {
	usePinStore(t)
	cfg := pinTestCfg(t, "trashcan", map[string]string{"cleanoutDays": "1"})
	vfs := versionerFsFromFolderCfg(cfg)
	ffs := cfg.Filesystem()
	v := newTrashcan(cfg)

	for _, name := range []string{"a.png", "b.png"} {
		writeFile(t, ffs, name, name)
		if err := v.Archive(name); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-10 * 24 * time.Hour).Truncate(time.Second)
	for _, name := range []string{"a.png", "b.png"} {
		if err := vfs.Chtimes(name, old, old); err != nil {
			t.Fatal(err)
		}
	}

	// Pin through the listing, the way the History screen does.
	versions, err := v.GetVersions()
	if err != nil {
		t.Fatal(err)
	}
	if len(versions["a.png"]) != 1 {
		t.Fatalf("expected one copy of a.png, got %v", versions)
	}
	if err := SetPinned(cfg, "a.png", versions["a.png"][0].VersionTime, true); err != nil {
		t.Fatal(err)
	}

	if err := v.Clean(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !exists(vfs, "a.png") {
		t.Error("pinned trashcan copy was removed")
	}
	if exists(vfs, "b.png") {
		t.Error("unpinned trashcan copy was kept")
	}
}

func TestRestoringPinnedKeepsTheCopy(t *testing.T) {
	usePinStore(t)
	cfg := pinTestCfg(t, "staggered", nil)
	vfs := versionerFsFromFolderCfg(cfg)
	ffs := cfg.Filesystem()
	v := newStaggered(cfg)

	when := time.Now().Add(-time.Hour).Truncate(time.Second)
	pinned := writeVersion(t, vfs, "chair.blend", when)
	if err := SetPinned(cfg, "chair.blend", when, true); err != nil {
		t.Fatal(err)
	}
	writeFile(t, ffs, "chair.blend", "current")

	if err := v.Restore("chair.blend", when); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, ffs, "chair.blend"); got != when.Format(TimeFormat) {
		t.Errorf("restored content = %q", got)
	}
	if !exists(vfs, pinned) {
		t.Error("restoring a pinned copy moved it out of the archive")
	}

	// An unpinned copy still moves, as upstream does.
	when2 := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
	plain := writeVersion(t, vfs, "chair.blend", when2)
	if err := v.Restore("chair.blend", when2); err != nil {
		t.Fatal(err)
	}
	if exists(vfs, plain) {
		t.Error("unpinned copy stayed in the archive after restore; upstream moves it")
	}
}

func TestPinStorePersists(t *testing.T) {
	path := usePinStore(t)
	cfg := pinTestCfg(t, "staggered", nil)
	when := time.Now().Truncate(time.Second)

	if err := SetPinned(cfg, "Scenes/cabin.blend", when, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("pin store was not written:", err)
	}

	// Forget the in-memory copy; a restart reads it back.
	pins.mut.Lock()
	pins.loaded = false
	pins.data = nil
	pins.mut.Unlock()

	set := PinnedVersions(cfg)
	if !set.Has("Scenes/cabin.blend", when) {
		t.Error("pin did not survive a reload")
	}
	// The same instant in another location is the same version.
	if !set.Has("Scenes/cabin.blend", when.UTC()) {
		t.Error("pin lookup depends on the time's location")
	}
	// Another folder's archive is not affected.
	other := pinTestCfg(t, "staggered", nil)
	if PinnedVersions(other).Has("Scenes/cabin.blend", when) {
		t.Error("pin leaked into another folder's archive")
	}
}

func TestUnreadablePinStoreIsEmpty(t *testing.T) {
	path := usePinStore(t)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := pinTestCfg(t, "staggered", nil)
	if len(PinnedVersions(cfg)) != 0 {
		t.Error("corrupt store produced pins")
	}
}
