// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// This file is an addition made by the desuqcafe fork. See
// custom/CUSTOMIZATIONS.md.
//
// Presence on the folder card: per person sharing a folder, when this
// computer last saw them, and the newest file whose current version they
// saved -- "Kai, last seen yesterday at 18:40, last saved tree.blend". What
// they are doing right now is their marks (api_claims.go); this is the rest.
//
// LAST SEEN
//
// /rest/stats/device reports a device that has never connected as the Unix
// epoch, not as a zero time (CLAUDE.md), so it is filtered here once rather
// than in every reader. Omitted means "never".
//
// FROM THE INDEX, NOT THE EVENT FEED
//
// /rest/events/disk would say who changed what as it happens, but it is a
// memory buffer that starts empty whenever the daemon restarts (CLAUDE.md),
// so on a Monday morning it knows nothing about Friday. The global index
// knows who saved every file's current version, for good. The price is that it
// only knows the *current* version: a file Kai saved and Mia saved over
// since is Mia's, and a deletion is nobody's save. That is the right answer
// to "what did Kai last leave in this folder".
//
// The time is the file's own modification time, which is when it was saved on
// the computer that saved it -- the puller copies it across. A file copied in
// from somewhere else keeps its old time and simply is not "recent".

package api

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/syncthing/syncthing/lib/protocol"
)

const (
	// lastSavedWindow is how far back a save counts. Past a month, "last
	// saved in August" is not presence, it is archaeology.
	lastSavedWindow = 30 * 24 * time.Hour

	// lastSavedMaxLookups bounds the per-file index reads: the walk is cheap
	// metadata, but who saved a file needs the full record. The newest few
	// hundred files cover everybody who has been working lately; somebody
	// whose newest file is older than all of them is not "active".
	lastSavedMaxLookups = 300
)

type lastSavedEntry struct {
	Device   string    `json:"device"`
	Path     string    `json:"path"`
	Modified time.Time `json:"modified"`
}

type presencePerson struct {
	Device    string          `json:"device"`
	Connected bool            `json:"connected"`
	LastSeen  *time.Time      `json:"lastSeen,omitempty"`
	LastSave  *lastSavedEntry `json:"lastSave,omitempty"`
}

type presenceResponse struct {
	Folder string           `json:"folder"`
	People []presencePerson `json:"people"`
}

// recentFile is one candidate from the metadata walk.
type recentFile struct {
	name string
	mod  time.Time
}

// newestPerDevice is the pure half: given the candidates newest first and a
// way to ask who saved each, the first file per wanted device. It stops as
// soon as every device has one, or the lookups run out.
func newestPerDevice(files []recentFile, want map[protocol.ShortID]protocol.DeviceID, whoSaved func(string) (protocol.ShortID, bool)) []lastSavedEntry {
	found := map[protocol.ShortID]bool{}
	var out []lastSavedEntry
	for i, f := range files {
		if i == lastSavedMaxLookups || len(found) == len(want) {
			break
		}
		by, ok := whoSaved(f.name)
		if !ok || found[by] {
			continue
		}
		dev, wanted := want[by]
		if !wanted {
			continue
		}
		found[by] = true
		out = append(out, lastSavedEntry{Device: dev.String(), Path: strings.ReplaceAll(f.name, `\`, "/"), Modified: f.mod.UTC()})
	}
	return out
}

func (s *service) getDBPresence(w http.ResponseWriter, r *http.Request) {
	cfg, ok := s.cfg.Folders()[r.URL.Query().Get("folder")]
	if !ok {
		forkHTTPError(w, errNoSuchFolder)
		return
	}
	want := map[protocol.ShortID]protocol.DeviceID{}
	for _, fd := range cfg.Devices {
		if fd.DeviceID != s.id {
			want[fd.DeviceID.Short()] = fd.DeviceID
		}
	}
	stats, err := s.model.DeviceStatistics()
	if err != nil {
		httpError(w, err)
		return
	}
	res := presenceResponse{Folder: cfg.ID, People: []presencePerson{}}
	byShort := map[protocol.ShortID]int{}
	for _, fd := range cfg.Devices {
		if fd.DeviceID == s.id {
			continue
		}
		p := presencePerson{Device: fd.DeviceID.String(), Connected: s.model.ConnectedTo(fd.DeviceID)}
		if st, ok := stats[fd.DeviceID]; ok && st.LastSeen.Year() >= 2000 {
			t := st.LastSeen.UTC()
			p.LastSeen = &t
		}
		byShort[fd.DeviceID.Short()] = len(res.People)
		res.People = append(res.People, p)
	}
	if len(want) == 0 {
		sendJSON(w, res)
		return
	}

	now := time.Now()
	var files []recentFile
	it, errFn := s.model.AllGlobalFiles(cfg.ID)
	for f := range it {
		if f.Deleted || f.Type != protocol.FileInfoTypeFile || isClaimsPath(f.Name) {
			continue
		}
		mod := f.ModTime()
		// A clock a little ahead on the saving machine is still a save
		// "just now"; a year ahead is a broken clock, not presence.
		if now.Sub(mod) > lastSavedWindow || mod.Sub(now) > time.Hour {
			continue
		}
		files = append(files, recentFile{name: f.Name, mod: mod})
	}
	if err := errFn(); err != nil {
		httpError(w, err)
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })

	saves := newestPerDevice(files, want, func(name string) (protocol.ShortID, bool) {
		gf, ok, err := s.model.CurrentGlobalFile(cfg.ID, name)
		if err != nil || !ok || gf.Deleted {
			return 0, false
		}
		return gf.ModifiedBy, true
	})
	for i := range saves {
		dev, _ := protocol.DeviceIDFromString(saves[i].Device)
		res.People[byShort[dev.Short()]].LastSave = &saves[i]
	}
	sendJSON(w, res)
}
