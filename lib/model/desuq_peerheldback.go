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
func (m *model) DesuqPeerHeldBack(folder string, device protocol.DeviceID) (files int, bytes int64, err error) {
	var names []string
	it, errFn := m.sdb.AllLocalFiles(folder, device)
	for f := range it {
		if f.Deleted || f.Type != protocol.FileInfoTypeFile {
			continue
		}
		if f.LocalFlags&protocol.FlagLocalRemoteInvalid == 0 {
			continue
		}
		files++
		names = append(names, f.Name)
	}
	if err := errFn(); err != nil {
		return 0, 0, err
	}
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
