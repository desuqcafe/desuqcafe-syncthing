// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// This file is an addition made by the desuqcafe fork. It lives in its own
// file, and adds a single line to api.go. See custom/CUSTOMIZATIONS.md.
//
// GET /rest/db/delivery?folder= answers "have my changes reached everybody",
// per person, including the people who are not connected.
//
// The main screen could already say that somebody was behind, from
// /rest/db/completion -- and said "Kai is still catching up, 12 MiB to go"
// about a computer that had been switched off since Tuesday. Nothing was
// catching up. Completion is computed from the peer's stored index and is the
// same number whether they are here or not; the sentence around it has to
// know which, and has to know whose changes they are missing, because "your
// latest scene is not on Kai's computer" is the thing a person acts on --
// wait before closing the lid, or phone them -- and "Kai is behind on
// something" is not.
//
// So this walks each peer's remote need -- the files whose global version
// their index does not have -- and splits it by who made that version. The
// ones this device made are "yours": the delivery a person is waiting on.
// Deleted entries count, because a deletion that has not arrived is a file
// that is still on their disk.
//
// The fork's own .desuq-claims directory is left out: a mark is a file in the
// folder, and "your latest has not reached Kai: KAIID.json" is not a
// sentence anybody can use. Claim delivery has its own report (api_claims.go).
//
// Bounded: at most deliveryMaxWalk needed entries per peer are read, and the
// counts are marked capped beyond that. A peer that needs ten thousand files
// is far enough behind that the exact number does not change what to say.

package api

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/syncthing/syncthing/lib/protocol"
)

const (
	deliveryPage    = 500
	deliveryMaxWalk = 5000
	deliveryNames   = 5
)

type deliveryCount struct {
	Files int   `json:"files"`
	Bytes int64 `json:"bytes"`
}

type deliveryPeer struct {
	Device    string `json:"device"`
	Name      string `json:"name"`
	Connected bool   `json:"connected"`
	// LastSeen is empty for a device that has never connected, rather than
	// the Unix epoch the statistics hold for one (see CLAUDE.md).
	LastSeen string `json:"lastSeen,omitempty"`
	// Yours is what this peer is missing that this device made; Names the
	// first few of those, newest first.
	Yours deliveryCount `json:"yours"`
	Names []string      `json:"names"`
	// Newest is when the newest of yours was made. The tray uses it to tell
	// a delivery that has been waiting from one that started a second ago.
	Newest string `json:"newest,omitempty"`
	// Need is everything the peer is missing, whoever made it.
	Need   deliveryCount `json:"need"`
	Capped bool          `json:"capped"`
}

type deliveryResponse struct {
	Folder string         `json:"folder"`
	Peers  []deliveryPeer `json:"peers"`
}

func (s *service) getDBDelivery(w http.ResponseWriter, r *http.Request) {
	cfg, ok := s.cfg.Folders()[r.URL.Query().Get("folder")]
	if !ok {
		forkHTTPError(w, errNoSuchFolder)
		return
	}
	devices := s.cfg.Devices()
	stats, err := s.model.DeviceStatistics()
	if err != nil {
		httpError(w, err)
		return
	}
	me := s.id.Short()

	res := deliveryResponse{Folder: cfg.ID, Peers: []deliveryPeer{}}
	for _, fd := range cfg.Devices {
		if fd.DeviceID == s.id {
			continue
		}
		p := deliveryPeer{
			Device:    fd.DeviceID.String(),
			Name:      fd.DeviceID.Short().String(),
			Connected: s.model.ConnectedTo(fd.DeviceID),
			Names:     []string{},
		}
		if d, ok := devices[fd.DeviceID]; ok && d.Name != "" {
			p.Name = d.Name
		}
		if st, ok := stats[fd.DeviceID]; ok && st.LastSeen.Unix() > 0 {
			p.LastSeen = st.LastSeen.Format(time.RFC3339)
		}

		var needed []protocol.FileInfo
		for page := 1; len(needed) < deliveryMaxWalk; page++ {
			files, err := s.model.RemoteNeedFolderFiles(cfg.ID, fd.DeviceID, page, deliveryPage)
			if err != nil {
				httpError(w, err)
				return
			}
			needed = append(needed, files...)
			if len(files) < deliveryPage {
				break
			}
		}
		p.Capped = len(needed) >= deliveryMaxWalk
		deliveryTally(&p, needed, me)
		res.Peers = append(res.Peers, p)
	}
	sort.Slice(res.Peers, func(i, j int) bool { return res.Peers[i].Name < res.Peers[j].Name })
	sendJSON(w, res)
}

// deliveryTally fills in what one peer is missing, and which of it is this
// device's doing.
func deliveryTally(p *deliveryPeer, needed []protocol.FileInfo, me protocol.ShortID) {
	var mine []protocol.FileInfo
	for _, f := range needed {
		// Directories are left out: a new one arrives with the first file in
		// it, and "Scenes has not reached Kai" names nothing anybody made.
		if deliveryOwnFile(f.Name) || f.IsDirectory() {
			continue
		}
		p.Need.Files++
		p.Need.Bytes += f.Size
		if f.ModifiedBy == me {
			p.Yours.Files++
			p.Yours.Bytes += f.Size
			mine = append(mine, f)
		}
	}
	sort.SliceStable(mine, func(i, j int) bool { return mine[i].ModTime().After(mine[j].ModTime()) })
	for i, f := range mine {
		if i == 0 {
			p.Newest = f.ModTime().Format(time.RFC3339)
		}
		if i >= deliveryNames {
			break
		}
		p.Names = append(p.Names, f.Name)
	}
}

// deliveryOwnFile is the fork's own bookkeeping inside a folder. Index names
// carry the OS separator, so both are accepted.
func deliveryOwnFile(name string) bool {
	name = strings.ReplaceAll(name, `\`, "/")
	return name == claimsDir || strings.HasPrefix(name, claimsDir+"/")
}
