// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// This file is an addition made by the desuqcafe fork. See
// custom/CUSTOMIZATIONS.md.
//
// "Kai, this one's yours now." Handing a file over passes the "I'm working
// on this" mark (api_claims.go) to somebody else, so the file is never
// unmarked in between -- which is exactly when a third person would open it.
//
// ONE WRITER PER FILE STILL HOLDS
//
// Nobody writes another device's claims file, so a hand-over is three steps,
// each written by the device it belongs to:
//
//  1. The giver marks its own entry "to: Kai". It is still the giver's mark,
//     and everybody else still sees the file as taken.
//  2. Kai's computer sees a mark addressed to it, and writes a mark of its
//     own, "from: the giver", plus an acknowledgement of that hand-over.
//  3. The giver's computer sees the acknowledgement and drops its entry.
//
// Between 2 and 3 the giver's entry is hidden everywhere, because the
// acknowledgement says it has been taken. That gap can be a whole weekend --
// the giver hands the file over and switches off -- so it is not a race to
// be won but a state to be shown correctly.
//
// WHY THE ACKNOWLEDGEMENT OUTLIVES THE MARK
//
// Kai takes the file, works on it and presses Done before the giver's
// computer has been on again. Without a record, the giver's "to: Kai" is
// still there, so Kai's computer would take it again at the next pass, and
// the file would be marked as Kai's for ever. So the acknowledgement is kept
// in Kai's file until the giver's entry is gone, and only then pruned.
//
// Steps 2 and 3 run whenever /rest/folder/claims is asked, which both the
// tray and the main screen do every few seconds.

package api

import (
	"errors"
	"log/slog"
	"time"

	"github.com/syncthing/syncthing/internal/slogutil"
	"github.com/syncthing/syncthing/lib/config"
	"github.com/syncthing/syncthing/lib/protocol"
)

// handoffAck is one hand-over this device has taken, in its own file.
type handoffAck struct {
	From     string    `json:"from"`
	Path     string    `json:"path"`
	HandedAt time.Time `json:"handedAt"`
}

var (
	errHandoffNotShared = errors.New("that person does not share this folder")
	errHandoffSelf      = errors.New("a file cannot be handed to yourself")
)

// handoffTarget is the device a hand-over may go to: somebody else the folder
// is shared with.
func (s *service) handoffTarget(cfg config.FolderConfiguration, device string) (protocol.DeviceID, error) {
	id, err := protocol.DeviceIDFromString(device)
	if err != nil {
		return protocol.EmptyDeviceID, errHandoffNotShared
	}
	if id == s.id {
		return protocol.EmptyDeviceID, errHandoffSelf
	}
	for _, d := range cfg.Devices {
		if d.DeviceID == id {
			return id, nil
		}
	}
	return protocol.EmptyDeviceID, errHandoffNotShared
}

// applyHandoff sets this device's mark on p to be handed to `to`, marking it
// first if it was not marked. A hand-over is deliberate, so an automatic mark
// becomes a hand-made one: the tray must not quietly take it off after a few
// quiet hours while the other person has not picked it up yet.
func applyHandoff(list []claimEntry, p string, to protocol.DeviceID, now time.Time) ([]claimEntry, error) {
	out := make([]claimEntry, 0, len(list)+1)
	found := false
	for _, c := range list {
		if c.Path == p {
			found = true
			c.To, c.HandedAt, c.From, c.Auto = to.String(), now.UTC(), "", false
		}
		out = append(out, c)
	}
	if found {
		return out, nil
	}
	if len(out) >= claimsMaxPerDevice {
		return list, errClaimTooMany
	}
	return append(out, claimEntry{Path: p, Since: now.UTC(), To: to.String(), HandedAt: now.UTC()}), nil
}

// accepted is whether recipient's file acknowledges giver's hand-over e.
func (d claimDocs) accepted(recipient string, giver protocol.DeviceID, e claimEntry) bool {
	rid, err := protocol.DeviceIDFromString(recipient)
	if err != nil {
		return false
	}
	doc, ok := d.byDevice[rid]
	if !ok {
		return false
	}
	return hasAck(doc.Accepted, giver.String(), e.Path, e.HandedAt)
}

func hasAck(acks []handoffAck, from, path string, at time.Time) bool {
	for _, a := range acks {
		if a.From == from && a.Path == path && a.HandedAt.Equal(at) {
			return true
		}
	}
	return false
}

// planHandoffs is steps 2 and 3, and the pruning, as a pure function of this
// device's own file and everybody's. It answers the file as it should be and
// whether that differs.
//
// canAccept is false where a mark made here would reach nobody -- a paused or
// receive-only folder. A hand-over to such a folder is left waiting rather
// than acknowledged: acknowledging it would hide the giver's mark and put no
// mark of ours anywhere anybody can see, leaving the file unmarked.
func planHandoffs(me protocol.DeviceID, mine claimsFile, docs claimDocs, canAccept bool, now time.Time) (claimsFile, bool) {
	changed := false
	claims := append([]claimEntry(nil), mine.Claims...)
	acks := append([]handoffAck(nil), mine.Accepted...)

	// 2. Take what has been handed here.
	if canAccept {
		for _, giver := range docs.order {
			if giver == me {
				continue
			}
			for _, e := range docs.byDevice[giver].Claims {
				if e.To != me.String() || hasAck(acks, giver.String(), e.Path, e.HandedAt) {
					continue
				}
				took := false
				for i := range claims {
					if claims[i].Path == e.Path {
						// Already marked here: it is ours now either way, and
						// it says where it came from.
						claims[i].From, claims[i].HandedAt = giver.String(), e.HandedAt
						claims[i].To, claims[i].Auto = "", false
						took = true
					}
				}
				if !took {
					if len(claims) >= claimsMaxPerDevice {
						continue
					}
					claims = append(claims, claimEntry{Path: e.Path, Since: now.UTC(), From: giver.String(), HandedAt: e.HandedAt})
				}
				acks = append(acks, handoffAck{From: giver.String(), Path: e.Path, HandedAt: e.HandedAt})
				changed = true
			}
		}
	}

	// 3. Drop what we handed over and has been taken.
	kept := claims[:0]
	for _, c := range claims {
		if c.To != "" && docs.accepted(c.To, me, c) {
			changed = true
			continue
		}
		kept = append(kept, c)
	}
	claims = kept

	// Prune acknowledgements whose hand-over is gone from the giver's file.
	// A giver whose file is on disk but not believable right now -- being
	// rewritten, say -- is not evidence either way, and dropping the
	// acknowledgement on a bad read would take the file back at the next pass.
	keptAcks := acks[:0]
	for _, a := range acks {
		gid, err := protocol.DeviceIDFromString(a.From)
		if err != nil {
			changed = true
			continue
		}
		doc, readable := docs.byDevice[gid]
		switch {
		case readable && givesTo(doc, me, a):
			keptAcks = append(keptAcks, a)
		case !readable && docs.present[gid]:
			keptAcks = append(keptAcks, a)
		default:
			changed = true
		}
	}
	acks = keptAcks

	mine.Claims, mine.Accepted = claims, acks
	if len(mine.Accepted) == 0 {
		mine.Accepted = nil
	}
	return mine, changed
}

// givesTo is whether doc still holds the hand-over a acknowledges.
func givesTo(doc claimsFile, me protocol.DeviceID, a handoffAck) bool {
	for _, c := range doc.Claims {
		if c.To == me.String() && c.Path == a.Path && c.HandedAt.Equal(a.HandedAt) {
			return true
		}
	}
	return false
}

// reconcileHandoffs applies planHandoffs to this device's file in one folder,
// and sends the result straight away if it changed anything.
func (s *service) reconcileHandoffs(cfg config.FolderConfiguration) {
	if ok, _ := canClaim(cfg); !ok {
		// Nothing written here would reach anybody, and in a receive-only
		// folder writing at all would be a local change somebody then has to
		// revert.
		return
	}
	claimsMut.Lock()
	defer claimsMut.Unlock()

	docs := s.readClaimDocs(cfg)
	wrote := false
	err := s.writeOwnDoc(cfg, func(doc *claimsFile) (bool, error) {
		next, changed := planHandoffs(s.id, *doc, docs, true, time.Now())
		*doc = next
		wrote = changed
		return changed, nil
	})
	if err != nil {
		slog.Warn("Could not update hand-overs", slogutil.Error(err))
		return
	}
	if wrote {
		s.model.ScanFolderSubdirs(cfg.ID, []string{claimsDir})
	}
}
