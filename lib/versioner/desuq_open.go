// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// This file is an addition made by the desuqcafe fork. It is a new file in an
// upstream package, which is the cheapest kind of divergence there is -- it
// cannot conflict on merge, and being inside the package it can use the
// unexported archive layout instead of guessing at it from outside. See
// custom/CUSTOMIZATIONS.md.
//
// WHY THIS EXISTS
//
// The Versioner interface can list an archive and restore from it, and that is
// all. Restoring is a destructive act that puts a file back into the folder,
// so the only way to find out what an archived copy actually is, is to restore
// it and look. For a list of thirty timestamps of the same texture that is a
// bad trade: the question "which one is the version I want" is exactly the
// question a person cannot answer from a timestamp.
//
// The fork's History screen shows thumbnails, so it needs to read an archived
// copy's bytes without moving anything. That is a read, and it is a read of a
// path this package already knows how to compute. Doing it outside the package
// would mean reimplementing versionerFsFromFolderCfg -- which handles a
// configured versions path, a tilde in it, a relative one, and a non-basic
// filesystem type -- and getting a copy of that wrong is silent: it looks like
// "no versions here" rather than an error.
//
// The lookup deliberately mirrors restoreFile: the tagged name first, then the
// untagged one whose mtime matches, which is how the trashcan versioner stores
// things. Anything restoreFile can put back, this can show.

package versioner

import (
	"time"

	"github.com/syncthing/syncthing/lib/config"
	"github.com/syncthing/syncthing/lib/fs"
	"github.com/syncthing/syncthing/lib/osutil"
)

// ErrVersionNotFound is returned when the archive has no copy of that file at
// that time. Exported because a caller wants to answer 404 rather than 500 for
// it -- an archive that has been cleaned since the list was drawn is ordinary,
// not a fault.
var ErrVersionNotFound = errNotFound

// OpenVersion opens the archived copy of filePath taken at versionTime, for
// reading. The caller closes it.
//
// filePath is folder-relative, in the same spelling GetVersions reports.
// versionTime is the VersionTime from that listing; it is matched to the
// second, as the archive names it.
func OpenVersion(cfg config.FolderConfiguration, filePath string, versionTime time.Time) (fs.File, fs.FileInfo, error) {
	versionsFs := versionerFsFromFolderCfg(cfg)
	filePath = osutil.NativeFilename(filePath)

	tag := versionTime.In(time.Local).Truncate(time.Second).Format(TimeFormat)
	candidate := TagFilename(filePath, tag)

	if info, err := versionsFs.Lstat(candidate); err != nil || !info.IsRegular() {
		// Untagged, which is how the trashcan versioner keeps a copy: the file
		// keeps its own name and its mtime is the version time.
		candidate = ""
		if info, err := versionsFs.Lstat(filePath); err == nil && info.IsRegular() &&
			info.ModTime().Truncate(time.Second).Equal(versionTime.Truncate(time.Second)) {
			candidate = filePath
		}
	}

	if candidate == "" {
		return nil, nil, ErrVersionNotFound
	}

	info, err := versionsFs.Lstat(candidate)
	if err != nil {
		return nil, nil, err
	}
	fd, err := versionsFs.Open(candidate)
	if err != nil {
		return nil, nil, err
	}
	return fd, info, nil
}
