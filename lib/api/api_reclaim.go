// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// This file is an addition made by the desuqcafe fork. Like api_dirsizes.go
// and api_reveal.go it lives in its own file and costs api.go two lines, so
// the divergence in a file upstream edits often stays small. See
// custom/CUSTOMIZATIONS.md.
//
// WHY THIS EXISTS
//
// The selective-sync picker is sold as choosing what lives on this machine.
// It is not that, and never was: un-ticking a directory writes an exclusion
// into the managed ignore block, which stops Syncthing *keeping that
// directory up to date*. Every byte already downloaded stays on the disk
// forever. A modeller who un-ticks three gigabytes of reference scans to make
// room watches their free space not move, and nothing in the interface
// explains why.
//
// Those are two separate things and the fork now says so: un-ticking stops
// updates, reclaiming deletes the local copies, and reclaiming is offered on
// the folder card whenever there is something to reclaim -- not only in the
// moment after a pick, because the bytes outlive that moment.
//
// WHY THIS IS NOT JUST os.Remove
//
// Deleting a user's files is the most destructive thing this fork does, and
// the interesting part is the set of things it refuses to delete. Four rails,
// all evaluated server-side per file at the moment of deletion rather than
// trusted from whatever the browser posted:
//
//  1. The file matches the ignore patterns *currently loaded for the folder*
//     -- re-read here, not taken from the request. A pattern the browser
//     thought was in force but is not can never reach a live file.
//  2. The global index still has it, undeleted. A file nobody is offering is
//     not "held back", it is just a file.
//  3. A device that is connected *right now* has the current global version.
//     This is the rail that makes the operation reversible: re-ticking pulls
//     it back. "Somebody had it last week" is not good enough.
//  4. The copy on disk is byte-identical in size and mtime to that global
//     version. If you edited it offline and then un-ticked it, your edit is
//     not their file and does not get deleted.
//
// Rail 4 is the reason this endpoint stats the disk rather than reading the
// local index. **Marking a file ignored blanks its size in the local index**
// -- protocol.FileInfo.SetIgnored calls setLocalFlags, which calls
// setNoContent, which sets Size to 0 and drops the block hashes. So the local
// index entry for an ignored file reports zero bytes and cannot be compared
// against anything. The global entry is authoritative and is not blanked, so
// the comparison is global-index versus a real Lstat.
//
// That is the same bug class as /rest/db/browse?dirsonly=1 reporting every
// directory as zero bytes: local state that looks like an answer and is not.
// See DEPLOYMENT-3D-TEAM.md section 18.
//
// Anything failing a rail is *named in the response with its reason*, never
// silently skipped. "Deleted 3,178 files, kept 2" with the two listed is a
// report; "deleted 3,178 files" alone is a thing you have to go and check.

package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/syncthing/syncthing/lib/config"
	"github.com/syncthing/syncthing/lib/fs"
	"github.com/syncthing/syncthing/lib/ignore"
	"github.com/syncthing/syncthing/lib/protocol"
)

// errNoSuchFolder is returned by the scan for a folder id that is not in the
// configuration, and mapped to 404 by the handlers.
var errNoSuchFolder = errors.New("no such folder")

// forkHTTPError maps the fork's own error values onto status codes. Upstream's
// httpError answers 500 for anything that is not an upgrade error, and a
// mistyped folder id is a client mistake, not a server fault. Kept here rather
// than added to httpError so api.go stays a two-line diff.
func forkHTTPError(w http.ResponseWriter, err error) {
	if errors.Is(err, errNoSuchFolder) {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	httpError(w, err)
}

// reclaimEntry is one file that could be deleted, or one that could not.
type reclaimEntry struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	// Reason is empty on a deletable entry and a short human sentence on a
	// kept one. It is rendered verbatim, so it is written as a clause that
	// follows the file name.
	Reason string `json:"reason,omitempty"`
}

// reclaimResponse answers both the dry run and the commit. The dry run fills
// Deletable; the commit moves what it actually removed into Deleted and adds
// anything that failed on the way to Kept.
type reclaimResponse struct {
	Folder string `json:"folder"`
	// Bytes and Files are the deletable totals -- what the button offers to
	// free on a dry run, and what it did free on a commit.
	Bytes int64 `json:"bytes"`
	Files int   `json:"files"`
	// Peers are the names of connected devices that hold the deletable files,
	// so the confirmation can say who you are relying on by name rather than
	// "a peer". Empty means nothing is deletable.
	Peers []string `json:"peers"`
	// Deletable is populated by the dry run, Deleted by the commit. Both are
	// capped -- see reclaimListCap -- because the count and the byte total are
	// what a person decides on, and a three thousand row list is not read.
	Deletable []reclaimEntry `json:"deletable,omitempty"`
	Deleted   []reclaimEntry `json:"deleted,omitempty"`
	// Kept is every file that failed a rail, with its reason. Capped the same
	// way; KeptTotal is the true count.
	Kept      []reclaimEntry `json:"kept,omitempty"`
	KeptTotal int            `json:"keptTotal"`
	// Truncated says a list was capped, so the interface can say "and 2,904
	// more" rather than implying it showed everything.
	Truncated bool `json:"truncated"`
}

// reclaimListCap bounds the two lists in the response. The totals above them
// are exact; these are for showing a person what sort of thing is in the set.
const reclaimListCap = 200

// getDBReclaimable is the dry run: what would deleting free, and what would it
// refuse to touch. Safe to call on a timer, and the folder card does.
func (s *service) getDBReclaimable(w http.ResponseWriter, r *http.Request) {
	folder := r.URL.Query().Get("folder")
	res, err := s.reclaimScan(folder)
	if err != nil {
		forkHTTPError(w, err)
		return
	}
	sendJSON(w, res)
}

// postDBReclaim deletes. It re-runs the whole scan rather than trusting a list
// from the browser: the rails are only worth anything if they are evaluated
// against the state at the moment of deletion.
func (s *service) postDBReclaim(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Folder string `json:"folder"`
		// ExpectBytes is the total the person was shown when they decided.
		// If the folder has changed underneath them -- a sync completed, a
		// peer went away -- the numbers no longer describe what they agreed
		// to, and this refuses rather than deleting a different set. Zero
		// means "do not check", which only the tests use.
		ExpectBytes int64 `json:"expectBytes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	res, err := s.reclaimScan(req.Folder)
	if err != nil {
		forkHTTPError(w, err)
		return
	}

	if req.ExpectBytes != 0 && req.ExpectBytes != res.Bytes {
		http.Error(w,
			"the folder changed since this was calculated; nothing was deleted",
			http.StatusConflict)
		return
	}

	cfg, ok := s.cfg.Folders()[req.Folder]
	if !ok {
		http.Error(w, "no such folder", http.StatusNotFound)
		return
	}
	ffs := cfg.Filesystem()

	// Re-derive the full deletable set: res.Deletable is capped for display.
	deletable, kept, _, _, _, err := s.reclaimCandidates(cfg)
	if err != nil {
		httpError(w, err)
		return
	}

	var (
		deleted    []reclaimEntry
		freed      int64
		files      int
		keptExtra  = kept
		dirsToTidy = map[string]struct{}{}
	)
	for _, e := range deletable {
		if err := ffs.Remove(e.Name); err != nil {
			// A file that vanished between the scan and now is the outcome we
			// wanted anyway; anything else is reported.
			if fs.IsNotExist(err) {
				continue
			}
			keptExtra = append(keptExtra, reclaimEntry{
				Name: e.Name, Size: e.Size,
				Reason: "could not be deleted: " + err.Error(),
			})
			continue
		}
		deleted = append(deleted, e)
		freed += e.Size
		files++
		if dir := parentDir(e.Name); dir != "" {
			dirsToTidy[dir] = struct{}{}
		}
	}

	// Empty directories left behind by the deletions. Deepest first, and only
	// ones that are now empty -- Remove on a non-empty directory fails and is
	// ignored, which is exactly the behaviour wanted.
	for _, dir := range deepestFirst(dirsToTidy) {
		_ = ffs.Remove(dir)
	}

	// Tell the model the files are gone rather than waiting for the next scan
	// to notice. They are ignored, so this does not propagate anywhere; it
	// keeps the folder card honest immediately after the click.
	s.model.ScanFolderSubdirs(req.Folder, nil)

	out := reclaimResponse{
		Folder:    req.Folder,
		Bytes:     freed,
		Files:     files,
		Peers:     res.Peers,
		Deleted:   capEntries(deleted),
		Kept:      capEntries(keptExtra),
		KeptTotal: len(keptExtra),
		Truncated: len(deleted) > reclaimListCap || len(keptExtra) > reclaimListCap,
	}
	sendJSON(w, out)
}

// reclaimScan is the dry run proper, shaped for the response.
func (s *service) reclaimScan(folder string) (reclaimResponse, error) {
	cfg, ok := s.cfg.Folders()[folder]
	if !ok {
		return reclaimResponse{}, errNoSuchFolder
	}

	deletable, kept, bytes, peers, _, err := s.reclaimCandidates(cfg)
	if err != nil {
		return reclaimResponse{}, err
	}

	return reclaimResponse{
		Folder:    folder,
		Bytes:     bytes,
		Files:     len(deletable),
		Peers:     peers,
		Deletable: capEntries(deletable),
		Kept:      capEntries(kept),
		KeptTotal: len(kept),
		Truncated: len(deletable) > reclaimListCap || len(kept) > reclaimListCap,
	}, nil
}

// reclaimCandidates walks the global index once and applies the four rails.
//
// It returns the full uncapped sets: the caller caps for display, and the
// commit path needs every one of them.
func (s *service) reclaimCandidates(cfg config.FolderConfiguration) (deletable, kept []reclaimEntry, bytes int64, peers []string, scanned int, err error) {
	folder := cfg.ID

	// Rail 1. The patterns currently loaded for this folder, re-read here.
	// CurrentIgnores gives the lines; the matcher is rebuilt from them rather
	// than reaching into model.folderIgnores, which is private -- and this way
	// the endpoint costs model.go nothing.
	lines, _, err := s.model.CurrentIgnores(folder)
	if err != nil {
		return nil, nil, 0, nil, 0, err
	}
	if len(lines) == 0 {
		// Nothing is ignored, so nothing is held back. Not an error: this is
		// the answer for every folder that has never been through the picker,
		// which is most of them.
		return nil, nil, 0, nil, 0, nil
	}
	matcher := ignore.New(cfg.Filesystem())
	if err := matcher.Parse(strings.NewReader(strings.Join(lines, "\n")), ""); err != nil {
		return nil, nil, 0, nil, 0, err
	}

	names := s.deviceNames()

	ffs := cfg.Filesystem()
	window := cfg.ModTimeWindow()
	peerSeen := map[string]struct{}{}

	it, errFn := s.model.AllGlobalFiles(folder)
	for f := range it {
		scanned++

		// Rail 2. Directories are handled by tidying up after the files;
		// symlinks are not worth the special cases and are left alone.
		if f.Deleted || f.Type != protocol.FileInfoTypeFile {
			continue
		}
		if !matcher.Match(f.Name).IsIgnored() {
			continue
		}

		// Anything below is a file the person asked not to keep up to date.
		// From here on every exit is recorded with a reason.
		info, err := ffs.Lstat(f.Name)
		if err != nil {
			// Already gone. Not kept, not deletable, nothing to report: the
			// disk is in the state the button is trying to reach.
			continue
		}

		holders := s.reclaimHolders(folder, f.Name)

		// Rails 3 and 4, together and pure, so they can be tested without a
		// model or a disk. See reclaimVerdict.
		if reason := reclaimVerdict(
			f.Size, f.ModTime(),
			info.Size(), info.ModTime(),
			len(holders), window,
		); reason != "" {
			kept = append(kept, reclaimEntry{
				Name: f.Name, Size: info.Size(), Reason: reason,
			})
			continue
		}

		deletable = append(deletable, reclaimEntry{Name: f.Name, Size: info.Size()})
		bytes += info.Size()
		for _, id := range holders {
			who := names[id]
			if who == "" {
				who = id.Short().String()
			}
			peerSeen[who] = struct{}{}
		}
	}
	if err := errFn(); err != nil {
		return nil, nil, 0, nil, scanned, err
	}

	// Only name the peers if something is actually deletable -- the list is
	// rendered as "Kai has all of them", which is a lie about an empty set.
	if len(deletable) > 0 {
		for who := range peerSeen {
			peers = append(peers, who)
		}
		sort.Strings(peers)
	}

	sort.Slice(deletable, func(i, j int) bool { return deletable[i].Name < deletable[j].Name })
	sort.Slice(kept, func(i, j int) bool { return kept[i].Name < kept[j].Name })
	return deletable, kept, bytes, peers, scanned, nil
}

// reclaimHolders is rail 3 for one file: which of the connected devices have
// the current global version.
func (s *service) reclaimHolders(folder, name string) []protocol.DeviceID {
	fi, ok, err := s.model.CurrentGlobalFile(folder, name)
	if err != nil || !ok {
		return nil
	}
	avail, err := s.model.Availability(folder, fi, protocol.BlockInfo{})
	if err != nil {
		return nil
	}
	var out []protocol.DeviceID
	for _, a := range avail {
		// FromTemporary means a half-finished download on this machine, which
		// is not somebody else having the file.
		if a.FromTemporary {
			continue
		}
		// Rail 3 proper: connected *now*. ConnectedTo is the model's own
		// answer, so this cannot drift from what the rest of the GUI shows.
		if s.model.ConnectedTo(a.ID) {
			out = append(out, a.ID)
		}
	}
	return out
}

func (s *service) deviceNames() map[protocol.DeviceID]string {
	out := map[protocol.DeviceID]string{}
	for id, dev := range s.cfg.Devices() {
		out[id] = dev.Name
	}
	return out
}

// reclaimVerdict is rails 3 and 4 with everything else stripped away: given
// what the global index says the file is, what is actually on the disk, and
// how many connected devices are offering it, should this copy be deleted.
//
// It returns "" to delete, or the sentence shown beside the file that was
// kept. Pure on purpose -- this is the part that decides whether somebody's
// work survives, and it should be provable without a model, a filesystem or a
// running daemon.
//
// The order of the checks is the order a person would ask them in, and it is
// load-bearing for the message: a file that is both modified *and* unavailable
// should say nobody else has it, because that is the reason it can never be
// safe to delete, while "you changed it" sounds like something you could
// resolve by changing it back.
func reclaimVerdict(globalSize int64, globalMod time.Time, diskSize int64, diskMod time.Time, holders int, window time.Duration) string {
	if holders == 0 {
		return "nobody else connected has this copy"
	}
	if diskSize != globalSize {
		return "your copy is a different size to theirs"
	}
	if !modTimeEqual(diskMod, globalMod, window) {
		return "your copy has been changed since it arrived"
	}
	return ""
}

// modTimeEqual compares within the folder's configured modification-time
// window, matching how the scanner decides a file is unchanged. A window of
// zero means exact, which is the case on every filesystem this fork ships to.
func modTimeEqual(a, b time.Time, window time.Duration) bool {
	if window <= 0 {
		return a.Equal(b)
	}
	d := a.Sub(b)
	if d < 0 {
		d = -d
	}
	return d <= window
}

func capEntries(in []reclaimEntry) []reclaimEntry {
	if len(in) <= reclaimListCap {
		return in
	}
	return in[:reclaimListCap]
}

// parentDir is the directory part of a slash-separated index name, or "" for
// something at the folder root.
func parentDir(name string) string {
	if i := strings.LastIndex(name, "/"); i > 0 {
		return name[:i]
	}
	return ""
}

// deepestFirst orders directories so children are removed before parents,
// which is the only order in which a chain of now-empty directories collapses.
// Ancestors are included: deleting a/b/c.png leaves a/b empty, and if that was
// all of a, then a as well.
func deepestFirst(dirs map[string]struct{}) []string {
	all := map[string]struct{}{}
	for d := range dirs {
		for d != "" {
			all[d] = struct{}{}
			d = parentDir(d)
		}
	}
	out := make([]string, 0, len(all))
	for d := range all {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		di := strings.Count(out[i], "/")
		dj := strings.Count(out[j], "/")
		if di != dj {
			return di > dj
		}
		return out[i] < out[j]
	})
	return out
}
