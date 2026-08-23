// Copyright (C) 2026 desuqcafe.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// The panic-log upload is the reporter upstream leaves ON by default:
// Options.CREnabled is `default:"true"` and, unlike usage reports and failure
// reports, nothing ever asks. So this is the one that would actually be
// sending today. See lib/build/desuq_telemetry.go.

package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/syncthing/syncthing/lib/locations"
)

func TestNoPanicLogIsUploaded(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hits.Add(1)
	}))
	defer srv.Close()

	dir := t.TempDir()

	// A config that asks as loudly as it can for the crash to be reported.
	cfgXML := `<configuration version="38">
    <options>
        <crashReportingEnabled>true</crashReportingEnabled>
        <crashReportingURL>` + srv.URL + `</crashReportingURL>
        <urAccepted>3</urAccepted>
    </options>
</configuration>
`
	if err := os.WriteFile(filepath.Join(dir, "config.xml"), []byte(cfgXML), 0o600); err != nil {
		t.Fatal(err)
	}

	panicLog := filepath.Join(dir, "panic-20260823-120000.log")
	if err := os.WriteFile(panicLog, []byte("panic: nothing at all\n\ngoroutine 1 [running]:\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := locations.SetBaseDir(locations.ConfigBaseDir, dir); err != nil {
		t.Fatal(err)
	}
	if err := locations.SetBaseDir(locations.DataBaseDir, dir); err != nil {
		t.Fatal(err)
	}

	maybeReportPanics()

	if n := hits.Load(); n != 0 {
		t.Errorf("crash reporting server was contacted %d times; want 0", n)
	}

	// An independent witness: a log that was uploaded gets renamed to
	// .reported.log, so the original still being there says the upload was
	// never attempted rather than merely having failed.
	if _, err := os.Stat(panicLog); err != nil {
		t.Errorf("panic log was renamed or removed, so an upload was attempted: %v", err)
	}
}
