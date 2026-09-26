// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// This file is an addition made by the desuqcafe fork. It lives in its own
// file, and adds two lines to api.go. See custom/CUSTOMIZATIONS.md.
//
// "I'm working on this file." Two modellers opening the same .blend is the
// one loss of work this setup produces by design: Syncthing cannot merge a
// binary, so it keeps both and renames one aside (see api_conflicts.go). The
// conflicts screen makes that recoverable. This makes it avoidable, by letting
// somebody say, before they start, which file they have open.
//
// THE CLAIMS TRAVEL AS FILES IN THE FOLDER ITSELF
//
// Each device writes exactly one file, .desuq-claims/<its device ID>.json, and
// nothing else ever writes it. One writer per file means the claims can never
// conflict among themselves -- the property the whole feature is for would be
// embarrassing to lose in its own storage. And because they are ordinary files
// in the folder, they reach exactly the people the folder is shared with, over
// the connection that already exists, with no new protocol and nothing to
// configure.
//
// A claim is advisory. Nothing is locked: a claimed file can still be opened,
// saved and synced by anybody. What changes is that people are told.
//
// WHO WROTE A CLAIM IS TAKEN FROM THE INDEX, NOT THE FILE NAME
//
// A file name is a claim about authorship, and anybody sharing the folder can
// create any file in it. The global index's ModifiedBy is not something a peer
// can forge for somebody else's device, so a peer's claims file is only
// believed when the index says that peer last wrote it. The same lesson as the
// conflict copies, whose names carry the wrong device (CLAUDE.md).

package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/syncthing/syncthing/lib/config"
	"github.com/syncthing/syncthing/lib/fs"
	"github.com/syncthing/syncthing/lib/protocol"
)

const (
	// claimsDir is where the claims live, relative to the folder root. The
	// selective-sync picker hides it and exempts it from its hold-back, and
	// the history, conflicts and activity views leave it out.
	claimsDir = ".desuq-claims"

	// claimsFileVersion is written into every claims file, so a later format
	// can recognise an earlier one rather than misreading it.
	claimsFileVersion = 1

	// claimStaleAfter is when a claim starts being shown as possibly
	// forgotten. Not an expiry: a file somebody is still working on after a
	// long weekend is still theirs, and silently dropping it would be the
	// worse mistake. The GUI dims it and says how old it is.
	claimStaleAfter = 3 * 24 * time.Hour

	// claimsMaxBytes bounds how much of a peer's claims file is read. A real
	// one is a few hundred bytes; this is a folder anybody sharing it can
	// write into.
	claimsMaxBytes = 64 << 10

	// claimsMaxPerDevice bounds how many claims one device may hold. Nobody
	// has a hundred files open.
	claimsMaxPerDevice = 100
)

var (
	errClaimReceiveOnly = errors.New("this folder only receives on this computer, so nobody else would see the mark")
	errClaimPaused      = errors.New("this folder is paused, so nobody else would see the mark until it runs again")
	errClaimBadPath     = errors.New("path must be a file inside the folder")
	errClaimNoSuchFile  = errors.New("there is no such file in this folder")
	errClaimTooMany     = errors.New("too many files are marked on this computer already")
)

// claimsMut serialises read-modify-write of this device's own claims files.
// Two tabs, or the tray's Send To and a click in the GUI, landing together
// would otherwise each read the old list and one would win.
var claimsMut sync.Mutex

// claimsFile is the on-disk shape.
type claimsFile struct {
	Version int          `json:"version"`
	Device  string       `json:"device"`
	Claims  []claimEntry `json:"claims"`
	// Accepted is the hand-overs this device has taken (api_handoff.go).
	// Kept after the mark itself is released, until the giver's side of the
	// hand-over is gone. Omitted when empty, so a file with none reads
	// exactly as it did.
	Accepted []handoffAck `json:"accepted,omitempty"`
}

type claimEntry struct {
	Path  string    `json:"path"`
	Since time.Time `json:"since"`
	// Auto is a mark the tray made because the file was saved here, rather
	// than one somebody asked for. The tray takes these off again once the
	// file has been left alone for a while; a mark somebody made by hand is
	// only ever taken off by hand. Omitted when false, so a file written by
	// an older build reads exactly as it did.
	Auto bool `json:"auto,omitempty"`
	// To is set on a mark this device is handing to somebody else, and
	// From on a mark this device was handed. HandedAt identifies the
	// hand-over on both sides. See api_handoff.go.
	To       string    `json:"to,omitempty"`
	From     string    `json:"from,omitempty"`
	HandedAt time.Time `json:"handedAt,omitzero"`
}

// claimRow is one claim as the API reports it.
type claimRow struct {
	Folder string    `json:"folder"`
	Label  string    `json:"label"`
	Path   string    `json:"path"`
	Device string    `json:"device"`
	Name   string    `json:"name"`
	Mine   bool      `json:"mine"`
	Since  time.Time `json:"since"`
	Stale  bool      `json:"stale"`
	Auto   bool      `json:"auto"`
	// Unseen is, on this device's own marks, everybody the folder is shared
	// with whose copy does not have them yet. Empty means everybody has.
	Unseen []claimPeer `json:"unseen,omitempty"`
	// HandingTo is who this mark is being handed to, while they have not
	// taken it yet. From is who handed it over, on a mark that was.
	HandingTo *claimRef `json:"handingTo,omitempty"`
	From      *claimRef `json:"from,omitempty"`
}

// claimRef names a device in a claim row. Mine says it is this computer, so
// the interface can say "you" without comparing IDs.
type claimRef struct {
	Device string `json:"device"`
	Name   string `json:"name"`
	Mine   bool   `json:"mine,omitempty"`
}

// claimPeer is one person a mark has not reached, and why. The why is what
// makes it worth saying: "Kai is offline" is something to phone about,
// "on its way" is a second's wait, and "not taking it" will never fix itself.
type claimPeer struct {
	Device string `json:"device"`
	Name   string `json:"name"`
	// State is "offline", "sending" or "heldBack".
	State string `json:"state"`
}

// claimsFolder says, per folder, whether this computer can mark files there
// and how much of the folder's file count is claims files. The main screen
// subtracts the latter: "1 of 1 files chosen" about a folder where nothing
// was chosen and a peer's claim arrived would be the local-state lie again.
type claimsFolder struct {
	Folder   string `json:"folder"`
	CanClaim bool   `json:"canClaim"`
	Reason   string `json:"reason,omitempty"`
	Files    int    `json:"files"`
	Bytes    int64  `json:"bytes"`
}

type claimsResponse struct {
	Claims  []claimRow     `json:"claims"`
	Folders []claimsFolder `json:"folders"`
}

type claimRequest struct {
	Folder  string `json:"folder"`
	Path    string `json:"path"`
	Release bool   `json:"release"`
	// Auto marks the claim as made by the tray on a save. See claimEntry.
	Auto bool `json:"auto"`
	// HandTo hands this computer's mark to another device the folder is
	// shared with. See api_handoff.go.
	HandTo string `json:"handTo"`
}

// cleanClaimPath turns what a caller sent into the slash-separated,
// folder-relative form claims are stored in, or reports that it is not one.
// Backslashes are accepted because the tray's Send To hands over Windows
// paths; everything else is rejected rather than repaired.
func cleanClaimPath(p string) (string, bool) {
	p = strings.ReplaceAll(strings.TrimSpace(p), `\`, "/")
	p = strings.TrimPrefix(p, "/")
	if p == "" || strings.Contains(p, ":") {
		return "", false
	}
	p = path.Clean(p)
	if p == "." || p == ".." || strings.HasPrefix(p, "../") {
		return "", false
	}
	if p == claimsDir || strings.HasPrefix(p, claimsDir+"/") {
		return "", false
	}
	return p, true
}

// applyClaim adds or removes one path. Adding a path already held keeps the
// original time: re-marking a file you have had open since Monday should not
// make it look like you only just started.
//
// Marking by hand a file the tray marked automatically makes it a hand-made
// mark, which the tray will then leave alone. The other way round changes
// nothing: a save does not demote somebody's deliberate mark to one that
// clears itself.
func applyClaim(list []claimEntry, p string, release, auto bool, now time.Time) ([]claimEntry, bool, error) {
	out := make([]claimEntry, 0, len(list)+1)
	found, changed := false, false
	for _, c := range list {
		if c.Path == p {
			found = true
			if release {
				continue
			}
			if c.Auto && !auto {
				c.Auto = false
				changed = true
			}
			// Marking by hand a file you are handing over takes it back.
			// A save does not: the tray marks on every save, and saving once
			// more before the other person has it must not cancel the
			// hand-over.
			if c.To != "" && !auto {
				c.To, c.HandedAt = "", time.Time{}
				changed = true
			}
		}
		out = append(out, c)
	}
	if release {
		return out, found, nil
	}
	if found {
		return out, changed, nil
	}
	if len(out) >= claimsMaxPerDevice {
		return list, false, errClaimTooMany
	}
	return append(out, claimEntry{Path: p, Since: now.UTC(), Auto: auto}), true, nil
}

// parseClaimsFile reads one device's file. It is strict about the things that
// would let a file speak for somebody else -- the device inside must be the
// one the file is named for -- and forgiving about the rest: an entry with a
// path that does not clean up is dropped, not the whole file.
func parseClaimsFile(data []byte, want protocol.DeviceID) ([]claimEntry, bool) {
	doc, ok := parseClaimsDoc(data, want)
	return doc.Claims, ok
}

// parseClaimsDoc is parseClaimsFile with the accepted hand-overs as well.
// A hand-over field naming something that is not a device ID is dropped
// rather than the mark: the mark is still true.
func parseClaimsDoc(data []byte, want protocol.DeviceID) (claimsFile, bool) {
	var f claimsFile
	if err := json.Unmarshal(data, &f); err != nil {
		return claimsFile{}, false
	}
	dev, err := protocol.DeviceIDFromString(f.Device)
	if err != nil || dev != want {
		return claimsFile{}, false
	}
	out := claimsFile{Version: f.Version, Device: f.Device, Claims: make([]claimEntry, 0, len(f.Claims))}
	for _, c := range f.Claims {
		p, ok := cleanClaimPath(c.Path)
		if !ok || c.Since.IsZero() {
			continue
		}
		e := claimEntry{Path: p, Since: c.Since, Auto: c.Auto}
		if !c.HandedAt.IsZero() {
			e.To, e.From = cleanDeviceString(c.To), cleanDeviceString(c.From)
			if e.To != "" || e.From != "" {
				e.HandedAt = c.HandedAt
			}
		}
		out.Claims = append(out.Claims, e)
		if len(out.Claims) == claimsMaxPerDevice {
			break
		}
	}
	for _, a := range f.Accepted {
		p, ok := cleanClaimPath(a.Path)
		from := cleanDeviceString(a.From)
		if !ok || from == "" || a.HandedAt.IsZero() {
			continue
		}
		out.Accepted = append(out.Accepted, handoffAck{From: from, Path: p, HandedAt: a.HandedAt})
		if len(out.Accepted) == claimsMaxPerDevice {
			break
		}
	}
	return out, true
}

// cleanDeviceString is a device ID in its canonical spelling, or "".
func cleanDeviceString(s string) string {
	if s == "" {
		return ""
	}
	id, err := protocol.DeviceIDFromString(s)
	if err != nil {
		return ""
	}
	return id.String()
}

// claimFileName is where a device's claims live, relative to the folder root.
func claimFileName(dev protocol.DeviceID) string {
	return claimsDir + "/" + dev.String() + ".json"
}

// canClaim is whether a mark made on this computer would reach anybody.
func canClaim(cfg config.FolderConfiguration) (bool, string) {
	switch {
	case cfg.Type == config.FolderTypeReceiveOnly || cfg.Type == config.FolderTypeReceiveEncrypted:
		return false, errClaimReceiveOnly.Error()
	case cfg.Paused:
		return false, errClaimPaused.Error()
	}
	return true, ""
}

// getFolderClaims lists every claim in every folder, or in one with ?folder=.
func (s *service) getFolderClaims(w http.ResponseWriter, r *http.Request) {
	folders := s.cfg.Folders()
	var ids []string
	if want := r.URL.Query().Get("folder"); want != "" {
		if _, ok := folders[want]; !ok {
			forkHTTPError(w, errNoSuchFolder)
			return
		}
		ids = []string{want}
	} else {
		for id := range folders {
			ids = append(ids, id)
		}
		sort.Strings(ids)
	}

	res := claimsResponse{Claims: []claimRow{}, Folders: []claimsFolder{}}
	now := time.Now()
	for _, id := range ids {
		// Take anything handed to this computer, and tidy away what the other
		// side has taken, before listing. Here rather than on a timer of its
		// own because both the tray and the main screen already ask this
		// every few seconds -- whichever is running keeps hand-overs moving.
		s.reconcileHandoffs(folders[id])
		rows, info := s.claimsIn(folders[id], now)
		res.Claims = append(res.Claims, rows...)
		res.Folders = append(res.Folders, info)
	}
	sendJSON(w, res)
}

// claimsIn reads one folder's claims off disk. The disk rather than the index
// for the contents, because the index holds hashes, not bytes; the index for
// the author, because the disk does not know who wrote anything.
func (s *service) claimsIn(cfg config.FolderConfiguration, now time.Time) ([]claimRow, claimsFolder) {
	ok, reason := canClaim(cfg)
	info := claimsFolder{Folder: cfg.ID, CanClaim: ok, Reason: reason}
	label := cfg.Label
	if label == "" {
		label = cfg.ID
	}

	docs := s.readClaimDocs(cfg)
	info.Files, info.Bytes = docs.files, docs.bytes

	var rows []claimRow
	for _, dev := range docs.order {
		doc := docs.byDevice[dev]
		mine := dev == s.id
		name := s.claimName(dev)
		var unseen []claimPeer
		if mine {
			unseen = s.claimsUnseenBy(cfg, claimFileName(dev))
		}
		for _, c := range doc.Claims {
			row := claimRow{
				Folder: cfg.ID,
				Label:  label,
				Path:   c.Path,
				Device: dev.String(),
				Name:   name,
				Mine:   mine,
				Since:  c.Since,
				Stale:  now.Sub(c.Since) > claimStaleAfter,
				Auto:   c.Auto,
			}
			if c.To != "" {
				// Once the other side has taken it, the giver's half is only
				// waiting for the giver's computer to tidy it away -- which
				// may be switched off for the weekend. It is not a mark any
				// more, and showing it would put two names on one file.
				if docs.accepted(c.To, dev, c) {
					continue
				}
				row.HandingTo = s.claimRefFor(c.To)
			}
			if c.From != "" {
				row.From = s.claimRefFor(c.From)
			}
			if mine {
				row.Unseen = unseen
			}
			rows = append(rows, row)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Since.After(rows[j].Since) })
	return rows, info
}

// claimDocs is every believable claims file in one folder.
type claimDocs struct {
	byDevice map[protocol.DeviceID]claimsFile
	// order is byDevice's keys, sorted, so rows come out in a stable order.
	order []protocol.DeviceID
	// present is every device that has a claims file on disk, believable or
	// not. A file that is there but cannot be read right now is not the same
	// as no file: see pruneAcks.
	present map[protocol.DeviceID]bool
	// files and bytes count every claims file, for the main screen to
	// subtract from the folder's totals.
	files int
	bytes int64
}

// readClaimDocs reads one folder's claims off disk. The disk rather than the
// index for the contents, because the index holds hashes, not bytes; the index
// for the author, because the disk does not know who wrote anything.
func (s *service) readClaimDocs(cfg config.FolderConfiguration) claimDocs {
	docs := claimDocs{byDevice: map[protocol.DeviceID]claimsFile{}, present: map[protocol.DeviceID]bool{}}
	ffs := cfg.Filesystem()
	names, err := ffs.DirNames(claimsDir)
	if err != nil {
		return docs
	}
	sort.Strings(names)

	devices := s.cfg.Devices()
	for _, n := range names {
		if fs.IsTemporary(n) {
			continue
		}
		rel := claimsDir + "/" + n
		st, err := ffs.Lstat(rel)
		if err != nil || !st.IsRegular() {
			continue
		}
		// Every file here is bookkeeping, and all of it comes off the main
		// screen's totals -- notes files (api_notes.go) included, or a folder
		// where only a note arrived would read "1 of 1 files chosen".
		docs.files++
		docs.bytes += st.Size()
		if !strings.HasSuffix(n, ".json") || isNotesName(n) {
			continue
		}
		dev, err := protocol.DeviceIDFromString(strings.TrimSuffix(n, ".json"))
		if err != nil {
			continue
		}
		docs.present[dev] = true

		mine := dev == s.id
		if _, known := devices[dev]; !mine && !known {
			// Somebody the folder is not shared with, as far as this
			// computer knows. Nothing to name them by, and no reason to
			// believe them.
			continue
		}
		if !s.claimAuthoredBy(cfg.ID, rel, dev, mine) {
			continue
		}
		data, err := readClaimsBytes(ffs, rel)
		if err != nil {
			continue
		}
		doc, valid := parseClaimsDoc(data, dev)
		if !valid {
			continue
		}
		docs.byDevice[dev] = doc
		docs.order = append(docs.order, dev)
	}
	return docs
}

// claimName is what a claim row calls a device.
func (s *service) claimName(dev protocol.DeviceID) string {
	if dev == s.id {
		return "You"
	}
	if d, ok := s.cfg.Devices()[dev]; ok && d.Name != "" {
		return d.Name
	}
	return dev.Short().String()
}

func (s *service) claimRefFor(device string) *claimRef {
	dev, err := protocol.DeviceIDFromString(device)
	if err != nil {
		return nil
	}
	return &claimRef{Device: dev.String(), Name: s.claimName(dev), Mine: dev == s.id}
}

// claimDelivery is satisfied by the real model; see
// lib/model/desuq_claimdelivery.go. Optional for the same reason as
// peerHeldBacker: a model without it reports nobody as unreached, which is
// what this API said before it could tell.
type claimDelivery interface {
	DesuqPeerHasLocal(folder string, device protocol.DeviceID, name string) (has, heldBack bool, err error)
}

// claimsUnseenBy is everybody the folder is shared with whose index does not
// yet hold this device's current claims file.
//
// Asked of the index rather than of the connection, because "connected" is
// not "has it": this is the whole difference between a mark that was made
// and a mark that was seen. canClaim used to be the only check, and it
// refused paused and receive-only folders but said nothing about a peer who
// was simply switched off -- so a mark made while they were away toasted
// success and reached nobody.
func (s *service) claimsUnseenBy(cfg config.FolderConfiguration, rel string) []claimPeer {
	cd, ok := s.model.(claimDelivery)
	if !ok {
		return nil
	}
	devices := s.cfg.Devices()
	var out []claimPeer
	for _, fd := range cfg.Devices {
		if fd.DeviceID == s.id {
			continue
		}
		has, heldBack, err := cd.DesuqPeerHasLocal(cfg.ID, fd.DeviceID, rel)
		if err != nil || has {
			continue
		}
		p := claimPeer{Device: fd.DeviceID.String(), Name: fd.DeviceID.Short().String()}
		if d, ok := devices[fd.DeviceID]; ok && d.Name != "" {
			p.Name = d.Name
		}
		connected := s.model.ConnectedTo(fd.DeviceID)
		switch {
		case !connected:
			p.State = "offline"
		case heldBack:
			p.State = "heldBack"
		default:
			p.State = "sending"
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// claimAuthoredBy is whether the index says dev last wrote this claims file.
//
// Our own file is checked too. Anybody sharing the folder can overwrite it,
// and a forged one would otherwise read as "You are working on..." here --
// and, worse, be kept and re-sent the next time we mark something, since the
// read-modify-write starts from what is on disk. The one allowance is a file
// of ours the index has not seen yet: written a moment ago and not scanned,
// which is ours by construction.
func (s *service) claimAuthoredBy(folder, rel string, dev protocol.DeviceID, mine bool) bool {
	fi, ok, err := s.model.CurrentGlobalFile(folder, rel)
	if err != nil {
		return false
	}
	if !ok {
		return mine
	}
	if fi.Deleted {
		return false
	}
	return fi.ModifiedBy == dev.Short()
}

func readClaimsBytes(ffs fs.Filesystem, rel string) ([]byte, error) {
	fd, err := ffs.Open(rel)
	if err != nil {
		return nil, err
	}
	defer fd.Close()
	return io.ReadAll(io.LimitReader(fd, claimsMaxBytes))
}

// postFolderClaim marks or unmarks one file as being worked on here.
//
// Body: {"folder": id, "path": "Scenes/cabin.blend", "release": false}.
// Answers with the folder's claims afterwards, so the caller can redraw
// without a second request.
func (s *service) postFolderClaim(w http.ResponseWriter, r *http.Request) {
	var req claimRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cfg, ok := s.cfg.Folders()[req.Folder]
	if !ok {
		forkHTTPError(w, errNoSuchFolder)
		return
	}
	p, ok := cleanClaimPath(req.Path)
	if !ok {
		http.Error(w, errClaimBadPath.Error(), http.StatusBadRequest)
		return
	}
	// Releasing is always allowed, so a claim made before the folder was
	// switched to receive-only or paused can still be taken back.
	if !req.Release {
		if ok, reason := canClaim(cfg); !ok {
			http.Error(w, reason, http.StatusConflict)
			return
		}
		if !s.claimTargetExists(cfg, p) {
			http.Error(w, errClaimNoSuchFile.Error(), http.StatusNotFound)
			return
		}
	}

	var handTo protocol.DeviceID
	if req.HandTo != "" && !req.Release {
		var herr error
		if handTo, herr = s.handoffTarget(cfg, req.HandTo); herr != nil {
			http.Error(w, herr.Error(), http.StatusBadRequest)
			return
		}
	}

	claimsMut.Lock()
	var err error
	if req.HandTo != "" && !req.Release {
		err = s.writeOwnDoc(cfg, func(doc *claimsFile) (bool, error) {
			next, err := applyHandoff(doc.Claims, p, handTo, time.Now())
			doc.Claims = next
			return err == nil, err
		})
	} else {
		err = s.writeOwnClaim(cfg, p, req.Release, req.Auto)
	}
	claimsMut.Unlock()
	if errors.Is(err, errClaimTooMany) {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if err != nil {
		httpError(w, err)
		return
	}

	// Scan just the claims directory, so the change leaves now rather than
	// at the next full rescan -- which on a large library is an hour away.
	s.model.ScanFolderSubdirs(cfg.ID, []string{claimsDir})

	rows, info := s.claimsIn(cfg, time.Now())
	if rows == nil {
		rows = []claimRow{}
	}
	sendJSON(w, claimsResponse{Claims: rows, Folders: []claimsFolder{info}})
}

// claimTargetExists is whether there is a file by that name, here or in the
// shared view. The index as well as the disk, because somebody who has only
// picked part of the folder can still reasonably say "I'm about to open
// that" about a file they have not pulled yet.
func (s *service) claimTargetExists(cfg config.FolderConfiguration, p string) bool {
	if st, err := cfg.Filesystem().Lstat(p); err == nil && st.IsRegular() {
		return true
	}
	fi, ok, err := s.model.CurrentGlobalFile(cfg.ID, p)
	return err == nil && ok && !fi.Deleted && fi.Type == protocol.FileInfoTypeFile
}

// writeOwnClaim does the read-modify-write on this device's file. An empty
// list removes the file rather than leaving "claims: []" behind, so a folder
// where nobody is working on anything carries nothing extra at all.
func (s *service) writeOwnClaim(cfg config.FolderConfiguration, p string, release, auto bool) error {
	return s.writeOwnDoc(cfg, func(doc *claimsFile) (bool, error) {
		next, changed, err := applyClaim(doc.Claims, p, release, auto, time.Now())
		if err != nil || !changed {
			return false, err
		}
		doc.Claims = next
		return true, nil
	})
}

// writeOwnDoc is the read-modify-write every change to this device's claims
// file goes through. The caller holds claimsMut. mutate reports whether it
// changed anything; nothing is written if not.
func (s *service) writeOwnDoc(cfg config.FolderConfiguration, mutate func(*claimsFile) (bool, error)) error {
	ffs := cfg.Filesystem()
	rel := claimFileName(s.id)

	var doc claimsFile
	// Start from what is on disk only if the index agrees we wrote it. A file
	// of ours that somebody else has overwritten is replaced, not extended:
	// extending it would re-send their forgery under our name.
	if s.claimAuthoredBy(cfg.ID, rel, s.id, true) {
		if data, err := readClaimsBytes(ffs, rel); err == nil {
			// A file of ours we cannot parse is replaced, not refused: nobody
			// else should write it, so the only way to get it back is to
			// write it.
			doc, _ = parseClaimsDoc(data, s.id)
		}
	}

	changed, err := mutate(&doc)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	next := doc.Claims

	if len(next) == 0 && len(doc.Accepted) == 0 {
		if err := ffs.Remove(rel); err != nil && !fs.IsNotExist(err) {
			return err
		}
		return nil
	}

	if err := ffs.MkdirAll(claimsDir, 0o755); err != nil {
		return err
	}
	// Hidden, like .stfolder: it is not something a modeller browsing the
	// folder in Explorer needs to see. Best effort -- a filesystem that cannot
	// hide things still syncs them.
	_ = ffs.Hide(claimsDir)

	if next == nil {
		// "claims": [] rather than null, for a file kept only for its
		// accepted hand-overs: an older build reading it expects a list.
		next = []claimEntry{}
	}
	data, err := json.MarshalIndent(claimsFile{
		Version:  claimsFileVersion,
		Device:   s.id.String(),
		Claims:   next,
		Accepted: doc.Accepted,
	}, "", "  ")
	if err != nil {
		return err
	}
	// Written beside and renamed over, under Syncthing's own temporary-name
	// prefix, which the scanner always skips -- so a half-written claims file
	// is never indexed and sent.
	tmp := fs.TempName(rel)
	fd, err := ffs.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := fd.Write(data); err != nil {
		fd.Close()
		_ = ffs.Remove(tmp)
		return err
	}
	if err := fd.Close(); err != nil {
		_ = ffs.Remove(tmp)
		return err
	}
	return ffs.Rename(tmp, rel)
}

// isClaimsPath is whether a folder-relative name is inside the claims
// directory. The history and conflicts views use it to leave the claims out:
// they are bookkeeping, not somebody's work.
func isClaimsPath(name string) bool {
	name = strings.ReplaceAll(name, `\`, "/")
	return name == claimsDir || strings.HasPrefix(name, claimsDir+"/")
}
