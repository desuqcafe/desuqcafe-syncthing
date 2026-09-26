// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// This file is an addition made by the desuqcafe fork. See
// custom/CUSTOMIZATIONS.md.
//
// WHY THIS EXISTS
//
// This build seeds staggered versioning at thirty days. That is a good safety
// net and a bad archive: the copy of a .blend that the client signed off on is
// removed on day thirty-one exactly like the forty autosaves around it. A pin
// says "keep this one until I say otherwise", and every cleanup path in this
// package asks isPinned before it removes anything.
//
// WHERE PINS LIVE, AND WHY IT IS THIS COMPUTER ONLY
//
// The archive is not synced. Every device keeps its own .stversions, filled
// when *that* device replaced a file, stamped with the time *it* did so. There
// is no shared identity for "the Tuesday version" to pin across machines, so a
// pin is a fact about this archive and lives beside this device's database,
// not in the folder. The History screen says so.
//
// Not a sidecar file inside the archive either: every walk in this package --
// retrieveVersions, clean, the trashcan's age sweep -- would see it, and the
// trashcan would list it as a restorable version and then delete it at the
// cutoff.
//
// THE KEY
//
// A pin is keyed by the archive directory's URI plus the file's normalised
// name and version tag. The URI rather than the folder ID because the cleanup
// paths are handed a filesystem and nothing else, so this costs no change to
// any versioner's struct or constructor. It also means a pin follows the
// archive rather than the folder: point versioning somewhere else and the old
// archive is not cleaned any more anyway.

package versioner

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/syncthing/syncthing/internal/slogutil"
	"github.com/syncthing/syncthing/lib/config"
	stfs "github.com/syncthing/syncthing/lib/fs"
	"github.com/syncthing/syncthing/lib/locations"
	"github.com/syncthing/syncthing/lib/osutil"
)

const pinsFileName = "desuq-pins.json"

// Pin is one kept version.
type Pin struct {
	PinnedAt time.Time `json:"pinnedAt"`
}

// pinFile is the on-disk shape: archive URI -> file name -> version tag.
type pinFile map[string]map[string]map[string]Pin

var pins = struct {
	mut    sync.Mutex
	loaded bool
	data   pinFile
	// path is swappable so the tests do not write into a real home.
	path func() string
}{
	path: func() string {
		return filepath.Join(locations.GetBaseDir(locations.DataBaseDir), pinsFileName)
	},
}

// loadPinsLocked reads the file once. A missing file is an empty store; an
// unreadable one is also treated as empty but said out loud, because the
// consequence -- pinned copies becoming ordinary again -- is exactly the thing
// a pin exists to prevent.
func loadPinsLocked() {
	if pins.loaded {
		return
	}
	pins.loaded = true
	pins.data = make(pinFile)

	bs, err := os.ReadFile(pins.path())
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err == nil {
		err = json.Unmarshal(bs, &pins.data)
	}
	if err != nil {
		slog.Warn("Could not read pinned versions; none will be protected until this is fixed", slogutil.FilePath(pins.path()), slogutil.Error(err))
		pins.data = make(pinFile)
	}
}

func savePinsLocked() error {
	bs, err := json.MarshalIndent(pins.data, "", "  ")
	if err != nil {
		return err
	}
	path := pins.path()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, bs, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func pinName(name string) string {
	return osutil.NormalizedFilename(name)
}

func pinTag(t time.Time) string {
	return t.In(time.Local).Truncate(time.Second).Format(TimeFormat)
}

// isPinned is what every cleanup path asks. archivePath is a path inside
// versionsFs as the walk reports it: tagged ("a/b~20260926-150211.png") for
// the simple and staggered versioners. mtime is only used for an untagged path
// -- the trashcan's -- whose version time is its modification time.
func isPinned(versionsFs stfs.Filesystem, archivePath string, mtime time.Time) bool {
	name, tag := UntagFilename(archivePath)
	if name == "" || tag == "" {
		name, tag = archivePath, pinTag(mtime)
	}

	pins.mut.Lock()
	defer pins.mut.Unlock()
	loadPinsLocked()
	_, ok := pins.data[versionsFs.URI()][pinName(name)][tag]
	return ok
}

// withoutPinned filters a removal list. Used by cleanVersions, which is the
// one place both the simple and the staggered versioner remove from.
func withoutPinned(versionsFs stfs.Filesystem, remove []string) []string {
	kept := remove[:0:0]
	for _, p := range remove {
		if isPinned(versionsFs, p, time.Time{}) {
			l.Debugln("Versioner: pinned, keeping", p)
			continue
		}
		kept = append(kept, p)
	}
	return kept
}

// SetPinned pins or unpins one version of one file in the folder's archive.
// name is folder-relative in the spelling GetVersions reports; versionTime is
// the VersionTime from that listing.
func SetPinned(cfg config.FolderConfiguration, name string, versionTime time.Time, pinned bool) error {
	uri := versionerFsFromFolderCfg(cfg).URI()
	name, tag := pinName(name), pinTag(versionTime)

	pins.mut.Lock()
	defer pins.mut.Unlock()
	loadPinsLocked()

	if pinned {
		if pins.data[uri] == nil {
			pins.data[uri] = make(map[string]map[string]Pin)
		}
		if pins.data[uri][name] == nil {
			pins.data[uri][name] = make(map[string]Pin)
		}
		if _, ok := pins.data[uri][name][tag]; ok {
			return nil
		}
		pins.data[uri][name][tag] = Pin{PinnedAt: time.Now()}
	} else {
		if _, ok := pins.data[uri][name][tag]; !ok {
			return nil
		}
		delete(pins.data[uri][name], tag)
		if len(pins.data[uri][name]) == 0 {
			delete(pins.data[uri], name)
		}
		if len(pins.data[uri]) == 0 {
			delete(pins.data, uri)
		}
	}
	return savePinsLocked()
}

// PinSet is one folder's pins, as the History screen reads them.
type PinSet map[string]map[string]Pin

// Has reports whether this version of this file is pinned. Matched by the
// archive's own tag -- the time to the second, in local time -- rather than
// by comparing time.Time values, which differ by location for equal instants.
func (p PinSet) Has(name string, versionTime time.Time) bool {
	_, ok := p[pinName(name)][pinTag(versionTime)]
	return ok
}

// PinnedVersions returns a copy of the folder's pins.
//
// Pins whose copy has gone -- removed by hand, say -- are deliberately not
// pruned. Pruning against a listing means trusting that listing to be
// complete, and one bad walk would then unpin a copy that is still there,
// which the next cleanup would delete. A leftover entry costs a few bytes.
func PinnedVersions(cfg config.FolderConfiguration) PinSet {
	uri := versionerFsFromFolderCfg(cfg).URI()

	pins.mut.Lock()
	defer pins.mut.Unlock()
	loadPinsLocked()

	res := make(PinSet)
	for name, tags := range pins.data[uri] {
		res[name] = make(map[string]Pin, len(tags))
		for tag, p := range tags {
			res[name][tag] = p
		}
	}
	return res
}
