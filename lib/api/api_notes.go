// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// This file is an addition made by the desuqcafe fork. See
// custom/CUSTOMIZATIONS.md.
//
// "Why I changed this." A line of text attached to one version of one file,
// by the person who saved that version. The history screen can already say
// *that* cabin.blend changed and who changed it; this is the only place that
// can say *why*, which is the question somebody restoring an old copy is
// actually asking ("which one still had the good lighting?").
//
// STORED LIKE THE MARKS, IN THE SAME DIRECTORY
//
// One file per device, .desuq-claims/<device ID>.notes.json, written only by
// that device -- so, like the marks (api_claims.go), notes can never conflict
// among themselves, and they reach exactly the people the folder is shared
// with. The same directory rather than a .desuq-notes of their own because
// every place that has to treat bookkeeping specially already knows that one:
// the selective-sync picker's exception line (which is already written into
// ignore files on machines in the field, and would hold a new directory back
// until somebody reopened the picker), the history, conflicts and activity
// views, and the tray's auto-mark. A second directory would have been a second
// row in every one of those, and a gap in whichever was missed.
//
// A NOTE IS ABOUT A VERSION, NOT A FILE
//
// Keyed by path plus the version's modification time and size, because those
// are the two things that survive everywhere the version goes: in the index,
// on every peer's disk (the puller sets the mtime), and in the archive, where
// a replaced version is renamed into .stversions with its mtime intact. So a
// note written today still labels the right row in History a month from now,
// after three newer saves -- and a note is never shown against a version it was
// not written about.
//
// Only the person who saved the current version can write a note about it.
// Anybody else's note about your work would be a guess dressed as an
// explanation. Who wrote a notes file is taken from the index, as for marks.

package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/syncthing/syncthing/lib/config"
	"github.com/syncthing/syncthing/lib/fs"
	"github.com/syncthing/syncthing/lib/protocol"
)

const (
	// notesSuffix names a device's notes file beside its claims file.
	notesSuffix = ".notes.json"

	notesFileVersion = 1

	// noteMaxRunes is how long one note may be. A sentence or two: this is
	// "moved the camera, lighting untouched", not a changelog.
	noteMaxRunes = 500

	// notesMaxPerDevice is how many notes one device keeps. The oldest go
	// first. Two hundred saves with a reason each is months of work, and the
	// archive they label is thirty days deep.
	notesMaxPerDevice = 200

	// notesKeepFor is how long a note is kept at all. Well past the archive,
	// so a pinned version (lib/versioner/desuq_pins.go) keeps its reason.
	notesKeepFor = 400 * 24 * time.Hour

	// notesMaxBytes bounds how much of a peer's notes file is read.
	notesMaxBytes = 512 << 10

	// notesRecentFor is how far back "your recent changes" looks when
	// offering files to write a note about.
	notesRecentFor = 14 * 24 * time.Hour

	// notesRecentMax is how many of them are offered.
	notesRecentMax = 8
)

var (
	errNoteNotYours = errors.New("the latest version of this file was saved by somebody else, so the note would be about their work")
	errNoteTooLong  = errors.New("that note is too long; keep it to a sentence or two")
	errNoteNoFile   = errors.New("there is no such file in this folder")
	errNoteBehind   = errors.New("a newer version of this file from somebody else is on its way to this computer")
)

type notesFile struct {
	Version int         `json:"version"`
	Device  string      `json:"device"`
	Notes   []noteEntry `json:"notes"`
}

type noteEntry struct {
	Path string `json:"path"`
	// Modified and Size identify the version. Modified is stored to the
	// second, UTC: the archive keeps nanoseconds on one filesystem and not on
	// another, and a second is plenty to tell two saves of a .blend apart.
	Modified time.Time `json:"modified"`
	Size     int64     `json:"size"`
	Text     string    `json:"text"`
	At       time.Time `json:"at"`
}

// noteRow is one note as the API reports it.
type noteRow struct {
	Folder   string    `json:"folder"`
	Label    string    `json:"label"`
	Path     string    `json:"path"`
	Device   string    `json:"device"`
	Name     string    `json:"name"`
	Mine     bool      `json:"mine"`
	Modified time.Time `json:"modified"`
	Size     int64     `json:"size"`
	Text     string    `json:"text"`
	At       time.Time `json:"at"`
	// Current is whether the version the note is about is still the file's
	// latest. A note about a version that has since been replaced is history,
	// and is shown there rather than on the main screen.
	Current bool `json:"current"`
	// Here is whether this computer has that version. The notes file is a few
	// hundred bytes and the .blend it explains may be a gigabyte, so the note
	// usually arrives first: "on its way" is the honest word until then.
	Here bool `json:"here"`
}

type notesFolder struct {
	Folder  string `json:"folder"`
	CanNote bool   `json:"canNote"`
	Reason  string `json:"reason,omitempty"`
}

// noteCandidate is one of your own recent saves, offered for a note.
type noteCandidate struct {
	Path     string    `json:"path"`
	Modified time.Time `json:"modified"`
	Size     int64     `json:"size"`
	Noted    bool      `json:"noted"`
}

type notesResponse struct {
	Notes   []noteRow       `json:"notes"`
	Folders []notesFolder   `json:"folders"`
	Yours   []noteCandidate `json:"yours,omitempty"`
}

type noteRequest struct {
	Folder string `json:"folder"`
	Path   string `json:"path"`
	// Text is the note. Empty removes it.
	Text string `json:"text"`
	// Modified and Size pick one of your existing notes to change or remove,
	// when it is about an older version. Omitted, the note is about the
	// version of the file you have now.
	Modified *time.Time `json:"modified,omitempty"`
	Size     *int64     `json:"size,omitempty"`
}

// noteStamp is the stored form of a version's modification time.
func noteStamp(t time.Time) time.Time { return t.UTC().Truncate(time.Second) }

// sameVersion is whether a note is about the version with this mtime and size.
func sameVersion(n noteEntry, mod time.Time, size int64) bool {
	return sameStamp(n.Modified, n.Size, mod, size)
}

// sameStamp is whether two (mtime, size) pairs name the same version.
func sameStamp(aMod time.Time, aSize int64, bMod time.Time, bSize int64) bool {
	return aSize == bSize && noteStamp(aMod).Equal(noteStamp(bMod))
}

// cleanNoteText trims a note and drops control characters other than line
// breaks. It reports whether what is left is short enough.
func cleanNoteText(s string) (string, bool) {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	return s, utf8.RuneCountInString(s) <= noteMaxRunes
}

func notesFileName(dev protocol.DeviceID) string {
	return claimsDir + "/" + dev.String() + notesSuffix
}

// isNotesName is whether a name in the claims directory is a notes file.
func isNotesName(name string) bool { return strings.HasSuffix(name, notesSuffix) }

// parseNotesDoc reads one device's notes file. Strict about whose it is,
// forgiving about individual entries, like parseClaimsDoc.
func parseNotesDoc(data []byte, want protocol.DeviceID) (notesFile, bool) {
	var f notesFile
	if err := json.Unmarshal(data, &f); err != nil {
		return notesFile{}, false
	}
	dev, err := protocol.DeviceIDFromString(f.Device)
	if err != nil || dev != want {
		return notesFile{}, false
	}
	out := notesFile{Version: f.Version, Device: f.Device, Notes: make([]noteEntry, 0, len(f.Notes))}
	for _, n := range f.Notes {
		p, ok := cleanClaimPath(n.Path)
		if !ok || n.Modified.IsZero() || n.At.IsZero() || n.Size < 0 {
			continue
		}
		text, ok := cleanNoteText(n.Text)
		if !ok || text == "" {
			continue
		}
		out.Notes = append(out.Notes, noteEntry{Path: p, Modified: noteStamp(n.Modified), Size: n.Size, Text: text, At: n.At})
		if len(out.Notes) == notesMaxPerDevice {
			break
		}
	}
	return out, true
}

// applyNote sets, replaces or (with empty text) removes the note about one
// version, then drops what is past keeping. It reports whether anything
// changed.
func applyNote(list []noteEntry, p string, mod time.Time, size int64, text string, now time.Time) ([]noteEntry, bool) {
	mod = noteStamp(mod)
	out := make([]noteEntry, 0, len(list)+1)
	changed, found := false, false
	for _, n := range list {
		if n.Path == p && sameVersion(n, mod, size) {
			found = true
			if text == "" {
				changed = true
				continue
			}
			if n.Text != text {
				n.Text, n.At = text, now.UTC()
				changed = true
			}
		}
		if now.Sub(n.At) > notesKeepFor {
			changed = true
			continue
		}
		out = append(out, n)
	}
	if !found && text != "" {
		out = append(out, noteEntry{Path: p, Modified: mod, Size: size, Text: text, At: now.UTC()})
		changed = true
	}
	if len(out) > notesMaxPerDevice {
		// Oldest written first out. Sorted only when trimming, so a file
		// that is under the cap keeps the order it was written in.
		sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
		out = out[:notesMaxPerDevice]
		changed = true
	}
	return out, changed
}

// noteDocs is every believable notes file in one folder.
type noteDocs struct {
	byDevice map[protocol.DeviceID]notesFile
	order    []protocol.DeviceID
}

func (s *service) readNoteDocs(cfg config.FolderConfiguration) noteDocs {
	docs := noteDocs{byDevice: map[protocol.DeviceID]notesFile{}}
	ffs := cfg.Filesystem()
	names, err := ffs.DirNames(claimsDir)
	if err != nil {
		return docs
	}
	sort.Strings(names)
	devices := s.cfg.Devices()
	for _, n := range names {
		if !isNotesName(n) || fs.IsTemporary(n) {
			continue
		}
		dev, err := protocol.DeviceIDFromString(strings.TrimSuffix(n, notesSuffix))
		if err != nil {
			continue
		}
		mine := dev == s.id
		if _, known := devices[dev]; !mine && !known {
			continue
		}
		rel := claimsDir + "/" + n
		if st, err := ffs.Lstat(rel); err != nil || !st.IsRegular() {
			continue
		}
		if !s.claimAuthoredBy(cfg.ID, rel, dev, mine) {
			continue
		}
		data, err := readLimited(ffs, rel, notesMaxBytes)
		if err != nil {
			continue
		}
		doc, ok := parseNotesDoc(data, dev)
		if !ok {
			continue
		}
		docs.byDevice[dev] = doc
		docs.order = append(docs.order, dev)
	}
	return docs
}

func readLimited(ffs fs.Filesystem, rel string, limit int64) ([]byte, error) {
	fd, err := ffs.Open(rel)
	if err != nil {
		return nil, err
	}
	defer fd.Close()
	return io.ReadAll(io.LimitReader(fd, limit))
}

// versionState answers Current and Here for one note. Looked up once per
// path, not per note: a file with thirty notes is still two index reads.
type versionState struct {
	global, local     protocol.FileInfo
	globalOK, localOK bool
	diskMod           time.Time
	diskSize          int64
	diskOK            bool
}

func (s *service) versionStateOf(cfg config.FolderConfiguration, p string) versionState {
	var v versionState
	if gf, ok, err := s.model.CurrentGlobalFile(cfg.ID, p); err == nil && ok && !gf.Deleted {
		v.global, v.globalOK = gf, true
	}
	if lf, ok, err := s.model.CurrentFolderFile(cfg.ID, p); err == nil && ok && !lf.Deleted {
		v.local, v.localOK = lf, true
	}
	if st, err := cfg.Filesystem().Lstat(p); err == nil && st.IsRegular() {
		v.diskMod, v.diskSize, v.diskOK = st.ModTime(), st.Size(), true
	}
	return v
}

// placeNote works out Current and Here. The author's own note about a save
// the scanner has not reached yet is about what is on disk: it is current,
// and it is here, even though the index does not know it yet.
func placeNote(n noteEntry, author protocol.DeviceID, mine bool, v versionState) (current, here bool) {
	if v.globalOK && sameVersion(n, v.global.ModTime(), v.global.Size) && v.global.ModifiedBy == author.Short() {
		current = true
		here = v.localOK && v.local.Version.Equal(v.global.Version)
	}
	if mine && !current && v.diskOK && sameVersion(n, v.diskMod, v.diskSize) &&
		(!v.localOK || !sameStamp(v.local.ModTime(), v.local.Size, v.diskMod, v.diskSize)) {
		current, here = true, true
	}
	return current, here
}

// notesIn lists one folder's notes, newest first, optionally for one file.
func (s *service) notesIn(cfg config.FolderConfiguration, only string) []noteRow {
	label := cfg.Label
	if label == "" {
		label = cfg.ID
	}
	docs := s.readNoteDocs(cfg)
	states := map[string]versionState{}
	rows := []noteRow{}
	for _, dev := range docs.order {
		mine := dev == s.id
		name := s.claimName(dev)
		for _, n := range docs.byDevice[dev].Notes {
			if only != "" && n.Path != only {
				continue
			}
			v, ok := states[n.Path]
			if !ok {
				v = s.versionStateOf(cfg, n.Path)
				states[n.Path] = v
			}
			current, here := placeNote(n, dev, mine, v)
			rows = append(rows, noteRow{
				Folder: cfg.ID, Label: label, Path: n.Path,
				Device: dev.String(), Name: name, Mine: mine,
				Modified: n.Modified, Size: n.Size, Text: n.Text, At: n.At,
				Current: current, Here: here,
			})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].At.After(rows[j].At) })
	return rows
}

// getFolderNotes lists notes in every folder, or one with ?folder=, or one
// file with ?folder=&file=. With ?yours=1 and a folder, it also lists your own
// recent saves there, for the "say why" picker.
func (s *service) getFolderNotes(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	folders := s.cfg.Folders()
	var ids []string
	if want := qs.Get("folder"); want != "" {
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
	only := ""
	if f := qs.Get("file"); f != "" {
		p, ok := cleanClaimPath(f)
		if !ok {
			http.Error(w, errClaimBadPath.Error(), http.StatusBadRequest)
			return
		}
		only = p
	}

	res := notesResponse{Notes: []noteRow{}, Folders: []notesFolder{}}
	for _, id := range ids {
		cfg := folders[id]
		ok, reason := canClaim(cfg)
		res.Folders = append(res.Folders, notesFolder{Folder: id, CanNote: ok, Reason: reason})
		res.Notes = append(res.Notes, s.notesIn(cfg, only)...)
	}
	sort.SliceStable(res.Notes, func(i, j int) bool { return res.Notes[i].At.After(res.Notes[j].At) })
	if qs.Get("yours") == "1" && len(ids) == 1 {
		res.Yours = s.recentOwnSaves(folders[ids[0]], res.Notes, time.Now())
		if res.Yours == nil {
			res.Yours = []noteCandidate{}
		}
	}
	sendJSON(w, res)
}

// recentOwnSaves is the files whose latest version this computer saved in
// the last two weeks, newest first. Filtered by time from the cheap metadata
// walk first, and only then asked who saved them, so a folder of five thousand
// files costs one walk and a few dozen lookups.
func (s *service) recentOwnSaves(cfg config.FolderConfiguration, notes []noteRow, now time.Time) []noteCandidate {
	type meta struct {
		name string
		mod  time.Time
		size int64
	}
	var recent []meta
	it, errFn := s.model.AllGlobalFiles(cfg.ID)
	for f := range it {
		if f.Deleted || f.Type != protocol.FileInfoTypeFile || isClaimsPath(f.Name) {
			continue
		}
		mod := f.ModTime()
		if now.Sub(mod) > notesRecentFor {
			continue
		}
		recent = append(recent, meta{name: f.Name, mod: mod, size: f.Size})
	}
	if errFn() != nil {
		return nil
	}
	sort.Slice(recent, func(i, j int) bool { return recent[i].mod.After(recent[j].mod) })

	short := s.id.Short()
	var out []noteCandidate
	for i, m := range recent {
		if len(out) == notesRecentMax || i == 5*notesRecentMax {
			break
		}
		gf, ok, err := s.model.CurrentGlobalFile(cfg.ID, m.name)
		if err != nil || !ok || gf.ModifiedBy != short {
			continue
		}
		name := strings.ReplaceAll(m.name, `\`, "/")
		c := noteCandidate{Path: name, Modified: noteStamp(m.mod), Size: m.size}
		for _, n := range notes {
			if n.Mine && n.Folder == cfg.ID && n.Path == name && n.Current {
				c.Noted = true
				break
			}
		}
		out = append(out, c)
	}
	return out
}

// noteTarget is the version a new note is about: the one on this computer
// now, which has to be this computer's own work.
//
// A save the scanner has not reached yet is on disk and not in the index.
// That is still this computer's work -- nobody else writes to this disk -- so
// the disk answers, and the scan that would have happened within seconds is
// asked for now.
func (s *service) noteTarget(cfg config.FolderConfiguration, p string) (time.Time, int64, error) {
	v := s.versionStateOf(cfg, p)
	if !v.diskOK {
		return time.Time{}, 0, errNoteNoFile
	}
	if !v.localOK || !sameStamp(v.local.ModTime(), v.local.Size, v.diskMod, v.diskSize) {
		go func() { _ = s.model.ScanFolderSubdirs(cfg.ID, []string{p}) }()
		return v.diskMod, v.diskSize, nil
	}
	if v.globalOK && !v.global.Version.Equal(v.local.Version) {
		// Somebody else has saved since; whatever is here is not the latest.
		if v.global.ModifiedBy != s.id.Short() {
			return time.Time{}, 0, errNoteBehind
		}
	}
	if v.local.ModifiedBy != s.id.Short() {
		return time.Time{}, 0, errNoteNotYours
	}
	return v.local.ModTime(), v.local.Size, nil
}

// postFolderNote writes, changes or removes one of this computer's notes.
func (s *service) postFolderNote(w http.ResponseWriter, r *http.Request) {
	var req noteRequest
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
	text, ok := cleanNoteText(req.Text)
	if !ok {
		http.Error(w, errNoteTooLong.Error(), http.StatusBadRequest)
		return
	}
	// Removing is always allowed, like taking a mark off: a folder switched
	// to receive-only since should not trap a note somebody regrets.
	if text != "" {
		if ok, reason := canClaim(cfg); !ok {
			http.Error(w, strings.Replace(reason, "the mark", "the note", 1), http.StatusConflict)
			return
		}
	}

	claimsMut.Lock()
	defer claimsMut.Unlock()

	rel := notesFileName(s.id)
	ffs := cfg.Filesystem()
	var doc notesFile
	if s.claimAuthoredBy(cfg.ID, rel, s.id, true) {
		if data, err := readLimited(ffs, rel, notesMaxBytes); err == nil {
			doc, _ = parseNotesDoc(data, s.id)
		}
	}

	var mod time.Time
	var size int64
	if req.Modified != nil && req.Size != nil {
		// One of your existing notes, about whichever version it names.
		mod, size = *req.Modified, *req.Size
		exists := false
		for _, n := range doc.Notes {
			if n.Path == p && sameVersion(n, mod, size) {
				exists = true
				break
			}
		}
		if !exists {
			http.Error(w, "there is no note of yours about that version", http.StatusNotFound)
			return
		}
	} else {
		var err error
		if mod, size, err = s.noteTarget(cfg, p); err != nil {
			status := http.StatusConflict
			if errors.Is(err, errNoteNoFile) {
				status = http.StatusNotFound
			}
			http.Error(w, err.Error(), status)
			return
		}
	}

	next, changed := applyNote(doc.Notes, p, mod, size, text, time.Now())
	if changed {
		if err := s.writeNotesDoc(cfg, next); err != nil {
			httpError(w, err)
			return
		}
		s.model.ScanFolderSubdirs(cfg.ID, []string{claimsDir})
	}

	okc, reason := canClaim(cfg)
	sendJSON(w, notesResponse{
		Notes:   s.notesIn(cfg, ""),
		Folders: []notesFolder{{Folder: cfg.ID, CanNote: okc, Reason: reason}},
	})
}

// writeNotesDoc writes this device's notes file the way writeOwnDoc writes
// its claims file: beside and renamed over, removed when empty.
func (s *service) writeNotesDoc(cfg config.FolderConfiguration, notes []noteEntry) error {
	ffs := cfg.Filesystem()
	rel := notesFileName(s.id)
	if len(notes) == 0 {
		if err := ffs.Remove(rel); err != nil && !fs.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := ffs.MkdirAll(claimsDir, 0o755); err != nil {
		return err
	}
	_ = ffs.Hide(claimsDir)
	data, err := json.MarshalIndent(notesFile{Version: notesFileVersion, Device: s.id.String(), Notes: notes}, "", "  ")
	if err != nil {
		return err
	}
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

// historyNotesFor is the notes about one archived version. An archived copy
// keeps the mtime of the save it preserves, which is what makes this a match
// rather than a guess.
func historyNotesFor(notes []noteRow, mod time.Time, size int64) []noteRow {
	var out []noteRow
	for _, n := range notes {
		if sameStamp(n.Modified, n.Size, mod, size) {
			out = append(out, n)
		}
	}
	return out
}
