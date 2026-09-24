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
// This build seeds staggered versioning on, thirty days, on every folder it
// creates. That is the right default for two modellers overwriting each
// other's .blend files -- but until now the history it writes had no entry
// point outside upstream's folder-detail region, so the fork was diligently
// archiving old copies that nobody could reach. Versioning that cannot be
// browsed is a disk-space leak with good intentions.
//
// WHY NOT JUST USE /rest/folder/versions
//
// Upstream's endpoint answers with the entire archive in one document:
// map[filename][]FileVersion, every version of every file, no paging and no
// filter. For the folder this fork is aimed at -- an asset library where a
// .blend is saved twenty times an afternoon -- staggered versioning at thirty
// days keeps roughly fifty copies per file. Five thousand files is a quarter
// of a million entries, tens of megabytes of JSON, to render a list whose
// first screen is twenty rows.
//
// So this is the same data in two shapes:
//
//   - Without ?file=, a *summary*: one row per file with its version count,
//     newest version time and total archived bytes. That is everything the
//     collapsed list shows, and it is a few tens of bytes per file rather
//     than a few thousand.
//   - With ?file=, the full version list for that one file, which is what
//     expanding a row needs and is never more than a few dozen entries.
//
// The aggregate is computed server-side from the same map upstream would have
// sent, and the big part is simply never serialised. Exactly the reasoning
// behind /rest/db/dirsizes, and the second time this fork has needed it.
//
// Restoring still goes through upstream's POST /rest/folder/versions, which is
// already the right shape: a map of filename to the version timestamp wanted.

package api

import (
	"net/http"
	"sort"
	"time"

	"github.com/syncthing/syncthing/lib/versioner"
)

// historyFile is one row of the summary: a file that has at least one archived
// copy. Name is the path within the folder, as the versioner stores it.
type historyFile struct {
	Name string `json:"name"`
	// Versions is how many archived copies exist.
	Versions int `json:"versions"`
	// Newest and Oldest bracket the archive for this file. Newest is what the
	// collapsed row shows and what the list sorts on.
	Newest time.Time `json:"newest"`
	Oldest time.Time `json:"oldest"`
	// Bytes is the total archived size, which is what a person deciding
	// whether the archive is worth keeping actually wants.
	Bytes int64 `json:"bytes"`
	// Deleted says the file is gone from the folder and only exists in the
	// archive. Those are the interesting ones -- restoring a deleted file is
	// undelete, restoring an existing one is rollback -- so the interface
	// labels them differently and this is how it knows.
	Deleted bool `json:"deleted"`
}

// historyResponse is the summary shape.
type historyResponse struct {
	Folder string `json:"folder"`
	// Versioning is false when the folder keeps no old copies at all. That is
	// a setting, not a failure, and it is a different thing to report from an
	// archive that exists and happens to be empty -- one says "nothing has
	// been overwritten yet", the other says "nothing will be kept if it is".
	Versioning bool `json:"versioning"`
	// Files, Versions and Bytes are the whole archive, exact, regardless of
	// how much of it is in Rows.
	Files    int   `json:"files"`
	Versions int   `json:"versions"`
	Bytes    int64 `json:"bytes"`
	// Rows is the page, newest-first. Sorting happens over the whole archive
	// before the page is cut, so page one really is the most recent activity.
	Rows []historyFile `json:"rows"`
	// Total is len(rows) before paging, after any ?prefix= filter.
	Total int `json:"total"`
}

// historyVersionsResponse is the ?file= shape: one file's archive, newest
// first.
type historyVersionsResponse struct {
	Folder   string                  `json:"folder"`
	Name     string                  `json:"name"`
	Deleted  bool                    `json:"deleted"`
	Versions []versioner.FileVersion `json:"versions"`
}

// historyPageCap bounds a summary page. The browser asks for more by paging;
// this exists so a first paint is a first paint and not a quarter of a million
// rows.
const historyPageCap = 500

// getFolderHistory serves both shapes -- see the file comment for why there
// are two.
func (s *service) getFolderHistory(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	folder := qs.Get("folder")

	if _, ok := s.cfg.Folders()[folder]; !ok {
		forkHTTPError(w, errNoSuchFolder)
		return
	}

	// A folder with versioning switched off is not an error, it is an answer,
	// and it is a different answer from "the archive is empty". This build
	// seeds versioning on for the folders it creates, but a folder added any
	// other way -- or one somebody deliberately turned it off for -- reaches
	// here, and the model reports that as a plain error. Rendered through the
	// generic path it becomes "could not read the archive", which describes a
	// fault rather than a setting and sends the reader looking for a problem
	// that does not exist.
	if !s.folderHasVersioning(folder) {
		sendJSON(w, historyResponse{Folder: folder, Rows: []historyFile{}})
		return
	}

	versions, err := s.model.GetFolderVersions(folder)
	if err != nil {
		forkHTTPError(w, err)
		return
	}

	if name := qs.Get("file"); name != "" {
		list := versions[name]
		// Newest first, matching the summary and the way the list reads.
		sort.Slice(list, func(i, j int) bool {
			return list[i].VersionTime.After(list[j].VersionTime)
		})
		sendJSON(w, historyVersionsResponse{
			Folder:   folder,
			Name:     name,
			Deleted:  s.historyFileIsGone(folder, name),
			Versions: list,
		})
		return
	}

	res, rows := historySummarise(versions, qs.Get("prefix"), func(name string) bool {
		return s.historyFileIsGone(folder, name)
	})
	res.Folder = folder
	res.Versioning = true

	res.Total = len(rows)
	page, perpage := historyPaging(qs.Get("page"), qs.Get("perpage"))
	start := (page - 1) * perpage
	if start > len(rows) {
		start = len(rows)
	}
	end := start + perpage
	if end > len(rows) {
		end = len(rows)
	}
	res.Rows = rows[start:end]
	sendJSON(w, res)
}

// historySummarise turns the whole archive into the header totals plus the
// sorted, filtered row list. Split out of the handler so the aggregation can
// be tested without a model: this is where a wrong number would be least
// visible and most misleading, because a total that is quietly too small still
// looks like a total.
//
// gone answers "is this file absent from the live folder" and is a callback
// only so the tests do not need a database.
//
// The totals deliberately cover the whole archive and ignore the filter. A
// header that shrank as you typed in the search box would be reporting on the
// search rather than on the folder, and "you are keeping 4 GB of old copies"
// is a fact about the folder.
func historySummarise(versions map[string][]versioner.FileVersion, prefix string, gone func(string) bool) (historyResponse, []historyFile) {
	res := historyResponse{Rows: []historyFile{}}
	rows := make([]historyFile, 0, len(versions))

	for name, list := range versions {
		if len(list) == 0 {
			continue
		}
		// The "I'm working on this" files (api_claims.go) are versioned like
		// anything else in the folder, one copy per mark and unmark. They are
		// bookkeeping, not somebody's work, and a History screen listing them
		// beside the textures would only be noise.
		if isClaimsPath(name) {
			continue
		}
		res.Files++
		res.Versions += len(list)

		row := historyFile{Name: name, Versions: len(list)}
		for _, v := range list {
			res.Bytes += v.Size
			row.Bytes += v.Size
			if row.Newest.IsZero() || v.VersionTime.After(row.Newest) {
				row.Newest = v.VersionTime
			}
			if row.Oldest.IsZero() || v.VersionTime.Before(row.Oldest) {
				row.Oldest = v.VersionTime
			}
		}

		if prefix != "" && !historyMatches(name, prefix) {
			continue
		}
		if gone != nil {
			row.Deleted = gone(name)
		}
		rows = append(rows, row)
	}

	sort.Slice(rows, func(i, j int) bool {
		if !rows[i].Newest.Equal(rows[j].Newest) {
			return rows[i].Newest.After(rows[j].Newest)
		}
		return rows[i].Name < rows[j].Name
	})
	return res, rows
}

// folderHasVersioning reports whether the folder keeps old copies at all.
//
// Read from the configuration rather than inferred from GetFolderVersions
// failing: the model returns a plain error for this, and matching on an error
// string is the kind of thing that stops working silently when upstream
// rewords it.
func (s *service) folderHasVersioning(folder string) bool {
	cfg, ok := s.cfg.Folders()[folder]
	if !ok {
		return false
	}
	return cfg.Versioning.Type != ""
}

// historyFileIsGone reports whether the live folder still has this file, which
// is what separates "restore an older copy" from "undelete".
//
// The global index is the right question to ask -- not the disk -- because a
// file another device deleted is gone from the folder even while its bytes are
// still here waiting to be removed.
func (s *service) historyFileIsGone(folder, name string) bool {
	fi, ok, err := s.model.CurrentGlobalFile(folder, name)
	if err != nil || !ok {
		return true
	}
	return fi.Deleted
}

// historyMatches is the search box: case-insensitive substring over the whole
// path, so typing "chair" finds refs/chairs/leg.blend and typing "refs/" finds
// the directory. Deliberately not a glob -- nobody types globs into a search
// box, and a stray * would silently match nothing.
func historyMatches(name, needle string) bool {
	return containsFold(name, needle)
}

func containsFold(hay, needle string) bool {
	if needle == "" {
		return true
	}
	h, n := []rune(lowerASCII(hay)), []rune(lowerASCII(needle))
	if len(n) > len(h) {
		return false
	}
outer:
	for i := 0; i+len(n) <= len(h); i++ {
		for j := range n {
			if h[i+j] != n[j] {
				continue outer
			}
		}
		return true
	}
	return false
}

// lowerASCII lowercases the ASCII range only. File names here are compared
// against something a person typed, and full Unicode case folding would make
// the match depend on locale for no gain a modeller would notice.
func lowerASCII(s string) string {
	b := []rune(s)
	for i, r := range b {
		if r >= 'A' && r <= 'Z' {
			b[i] = r + ('a' - 'A')
		}
	}
	return string(b)
}

func historyPaging(pageStr, perpageStr string) (int, int) {
	page := atoiDefault(pageStr, 1)
	if page < 1 {
		page = 1
	}
	perpage := atoiDefault(perpageStr, historyPageCap)
	if perpage < 1 || perpage > historyPageCap {
		perpage = historyPageCap
	}
	return page, perpage
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return def
		}
		n = n*10 + int(r-'0')
		if n > 1<<30 {
			return def
		}
	}
	return n
}
