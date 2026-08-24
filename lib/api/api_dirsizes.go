// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// This file is an addition made by the desuqcafe fork. Like api_diskfree.go it
// lives in its own file and costs api.go a single line, so the divergence in a
// file upstream edits often stays one line. See custom/CUSTOMIZATIONS.md.
//
// WHY THIS EXISTS
//
// The selective-sync picker switches to a directories-only tree once a folder
// has more than twenty thousand files, because the full tree is a JSON
// document the browser cannot parse quickly and could not render usefully
// anyway. /rest/db/browse can produce that tree -- dirsonly=1 -- but it does
// it by skipping every file, so every directory in the result reports a size
// of zero.
//
// That leaves the picker with no idea how big anything is in exactly the case
// where it matters most: the folder is large, which is why the mode exists.
// The size gauge falls back to counting items, which is honest, but the disk
// guard cannot -- "will this fit" is a question about bytes -- so it was
// silently inert on every folder big enough to fill a disk.
//
// So: the same directory tree, with each directory carrying the totals for
// everything at or below it. Adding eight bytes a node to a document that was
// already going to be sent is free; the file-level tree that would answer the
// same question is the megabytes this mode exists to avoid.

package api

import (
	"net/http"
	"time"

	"github.com/syncthing/syncthing/lib/model"
	"github.com/syncthing/syncthing/lib/protocol"
)

// dirSizeEntry mirrors model.TreeEntry -- deliberately, so the picker's tree
// builder does not need to know which of the two endpoints it is reading --
// with the aggregates added.
type dirSizeEntry struct {
	Name    string    `json:"name"`
	ModTime time.Time `json:"modTime"`
	Type    string    `json:"type"`
	// Size and Files are for everything at or below this directory, the
	// directory's own loose files included. A parent therefore includes all of
	// its children: a consumer that sums every node in the tree will count
	// most bytes several times over.
	Size     int64           `json:"size"`
	Files    int64           `json:"files"`
	Children []*dirSizeEntry `json:"children,omitempty"`
}

type dirSizesResponse struct {
	// Bytes and Files are the whole folder.
	Bytes int64 `json:"bytes"`
	Files int64 `json:"files"`
	// RootBytes and RootFiles are the files sitting loose at the folder's
	// root. They appear nowhere in Children -- this is a directory tree -- and
	// the picker cannot offer them, so they are part of what any selection
	// costs and it needs them named separately to say so.
	RootBytes int64           `json:"rootBytes"`
	RootFiles int64           `json:"rootFiles"`
	Children  []*dirSizeEntry `json:"children"`
}

func (s *service) getDBDirSizes(w http.ResponseWriter, r *http.Request) {
	folder := r.URL.Query().Get("folder")
	if folder == "" {
		http.Error(w, "folder parameter is required", http.StatusBadRequest)
		return
	}

	// The full tree, files and all. It is built and thrown away inside this
	// handler and never serialised, which is the whole trick: the expensive
	// part of dirsonly was always the response, not the walk.
	tree, err := s.model.GlobalDirectoryTree(folder, "", -1, false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	dirs, bytes, files := foldDirSizes(tree)

	// Whatever the top-level directories do not account for is sitting loose
	// at the root.
	resp := dirSizesResponse{
		Bytes:     bytes,
		Files:     files,
		RootBytes: bytes,
		RootFiles: files,
		Children:  dirs,
	}
	for _, d := range dirs {
		resp.RootBytes -= d.Size
		resp.RootFiles -= d.Files
	}
	sendJSON(w, resp)
}

// foldDirSizes reduces a full tree to its directories, summing what it drops.
//
// It returns the directories at this level plus the totals for everything
// passed in, so a parent can take its own figures from one recursive call
// rather than walking its subtree twice.
func foldDirSizes(entries []*model.TreeEntry) (dirs []*dirSizeEntry, bytes, files int64) {
	dirs = make([]*dirSizeEntry, 0, len(entries))
	for _, e := range entries {
		if e.Type != protocol.FileInfoTypeDirectory.String() {
			// A file: it contributes its size to whoever asked, and vanishes.
			// Symlinks count as items but carry no bytes worth reporting.
			bytes += e.Size
			files++
			continue
		}

		children, subBytes, subFiles := foldDirSizes(e.Children)
		dirs = append(dirs, &dirSizeEntry{
			Name:     e.Name,
			ModTime:  e.ModTime,
			Type:     e.Type,
			Size:     subBytes,
			Files:    subFiles,
			Children: children,
		})
		bytes += subBytes
		files += subFiles
	}
	return dirs, bytes, files
}
