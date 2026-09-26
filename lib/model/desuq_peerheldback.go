// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// This file is an addition made by the desuqcafe fork: a new file inside an
// upstream package, so it can reach the database without widening the Model
// interface (and with it the generated mocks). lib/api finds the method by an
// optional interface. See custom/CUSTOMIZATIONS.md.

package model

import (
	"github.com/syncthing/syncthing/lib/protocol"
)

// DesuqPeerHeldBack counts the files a peer is holding back from a folder:
// entries in that peer's index for files that exist in the folder but which
// the peer has marked invalid -- which is what ignoring a file does, and so
// what the selective-sync picker does to everything left unticked.
//
// Completion cannot say this. An ignored file is not needed, so a peer that
// has taken nothing at all reports 100% and reads as "has the same files as
// you" -- the fourth instance of a sentence built from counts that leave
// ignored files out. The peer's own index is the only place the answer lives.
//
// Deleted entries do not count: a file gone for everybody is not held back.
// This walks the peer's whole index for the folder, so it is for a slow probe,
// not a per-second one.
//
// The size is the global entry's. The peer's own entry for an ignored file
// has its Size blanked to zero (setNoContent, the same thing that happens to
// local ignored files -- CLAUDE.md), so summing it reports every held-back
// byte as nothing. Verified on a pair: five files, zero bytes.
//
// Not every invalid entry is a held-back file. A receive-only folder announces
// its own local edits invalid too, so that nobody pulls them -- and those keep
// their size and block hash, where an ignored file has both blanked. Those are
// counted apart, as changed: see DesuqPeerChangedThere.
func (m *model) DesuqPeerHeldBack(folder string, device protocol.DeviceID) (files int, bytes int64, err error) {
	names, _, err := m.desuqPeerInvalid(folder, device)
	if err != nil {
		return 0, 0, err
	}
	files = len(names)
	// Looked up after the walk rather than inside it: the iterator holds a
	// read on the database, and a second query inside it is asking for a
	// lock-order problem on a database that serialises its readers.
	for _, name := range names {
		if g, ok, err := m.sdb.GetGlobalFile(folder, name); err == nil && ok && !g.Deleted {
			bytes += g.Size
		}
	}
	return files, bytes, nil
}

// DesuqPeerChangedThere counts the files a peer has changed on their own
// computer and is not sending: the local edits and deletions of a folder
// that only receives there.
//
// Completion reads these as the peer being behind -- they "need" the global
// version of every file they changed -- so without this the main screen said
// "Sending 2 MiB to Yuki, theirs is catching up" about a copy that will never
// catch up on its own. Verified on a pair, 2026-09-26: one edit and one
// deletion on a receive-only B read as 60%, two items needed, remote state
// valid.
func (m *model) DesuqPeerChangedThere(folder string, device protocol.DeviceID) (int, error) {
	_, changed, err := m.desuqPeerInvalid(folder, device)
	return changed, err
}

// desuqPeerInvalid walks a peer's index once and sorts its invalid file
// entries into held back (ignored: size and block hash blanked) and changed
// there (a receive-only edit, which keeps both, or a receive-only deletion of
// a file that still exists for everyone else).
func (m *model) desuqPeerInvalid(folder string, device protocol.DeviceID) (held []string, changed int, err error) {
	var deleted []string
	it, errFn := m.sdb.AllLocalFiles(folder, device)
	for f := range it {
		if f.Type != protocol.FileInfoTypeFile {
			continue
		}
		if f.LocalFlags&protocol.FlagLocalRemoteInvalid == 0 {
			continue
		}
		switch {
		case f.Deleted:
			deleted = append(deleted, f.Name)
		case f.Size == 0 && len(f.BlocksHash) == 0:
			held = append(held, f.Name)
		default:
			changed++
		}
	}
	if err := errFn(); err != nil {
		return nil, 0, err
	}
	// A deletion is only a change of theirs if the file is still there for
	// everybody else. Looked up after the walk, for the same lock-order
	// reason as the sizes in DesuqPeerHeldBack.
	for _, name := range deleted {
		if g, ok, err := m.sdb.GetGlobalFile(folder, name); err == nil && ok && !g.Deleted {
			changed++
		}
	}
	return held, changed, nil
}
