// Copyright (C) 2026 desuqcafe.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// Asserts that no telemetry leaves the machine, by watching the wire rather
// than by re-reading the constant that is supposed to prevent it. See
// lib/build/desuq_telemetry.go.

package ur

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/syncthing/syncthing/lib/build"
	"github.com/syncthing/syncthing/lib/config"
	"github.com/syncthing/syncthing/lib/connections"
	"github.com/syncthing/syncthing/internal/db"
	"github.com/syncthing/syncthing/lib/events"
	"github.com/syncthing/syncthing/lib/protocol"
	"github.com/syncthing/syncthing/lib/ur/contract"
)

func TestTelemetryIsCompiledOff(t *testing.T) {
	if build.TelemetryEnabled {
		t.Fatal("build.TelemetryEnabled is true; this fork ships with it false")
	}
}

// A default configuration must not claim telemetry is on, even though nothing
// reads these values any more. A config.xml that says urAccepted 0 invites the
// next reader to conclude the question is still open.
func TestTelemetryDefaultsAreOff(t *testing.T) {
	opts := config.New(protocol.EmptyDeviceID).Options
	if opts.URAccepted != -1 {
		t.Errorf("URAccepted = %d, want -1 (declined)", opts.URAccepted)
	}
	if opts.CREnabled {
		t.Error("CREnabled is true; upstream's default, which uploads panic logs unasked")
	}
}

// The one that matters: with usage reporting turned all the way up in the
// config, and the initial delay removed, nothing is posted.
func TestNoUsageReportIsPosted(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	defer srv.Close()

	cfg := config.New(protocol.EmptyDeviceID)
	cfg.Options.URAccepted = Version // the most anyone could ever have agreed to
	cfg.Options.URSeen = Version
	cfg.Options.URUniqueID = "testtest"
	cfg.Options.URURL = srv.URL
	cfg.Options.URInitialDelayS = 0
	w := config.Wrap("/dev/null", cfg, protocol.EmptyDeviceID, events.NoopLogger)

	svc := New(w, &stubModel{}, stubConnections{}, true)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); _ = svc.Serve(ctx) }()

	// Poke it the way a config change would, in case the timer alone is not
	// enough to make an upstream build report.
	svc.CommitConfiguration(config.Configuration{}, cfg)

	<-ctx.Done()
	<-done

	if n := hits.Load(); n != 0 {
		t.Fatalf("usage report server was contacted %d times; want 0", n)
	}
}

// The failure reporter has a ten second aggregation delay, so watching the
// wire would make this test slow for no extra confidence. Instead: prove the
// guard runs before the handler does anything at all, by checking it never
// even subscribes to the config.
func TestFailureReporterDoesNothing(t *testing.T) {
	t.Parallel()

	cfg := config.New(protocol.EmptyDeviceID)
	cfg.Options.URAccepted = Version
	cfg.Options.CREnabled = true
	cfg.Options.CRURL = "http://127.0.0.1:1/should-never-be-used"
	w := &countingWrapper{Wrapper: config.Wrap("/dev/null", cfg, protocol.EmptyDeviceID, events.NoopLogger)}

	h := NewFailureHandler(w, events.NoopLogger)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := h.Serve(ctx); err != context.DeadlineExceeded {
		t.Fatalf("Serve returned %v; want it to sit inert until the context ends", err)
	}
	if n := w.subscribes.Load(); n != 0 {
		t.Fatalf("failure handler subscribed to the config %d times; want 0", n)
	}
}

type countingWrapper struct {
	config.Wrapper
	subscribes atomic.Int32
}

func (w *countingWrapper) Subscribe(c config.Committer) config.Configuration {
	w.subscribes.Add(1)
	return w.Wrapper.Subscribe(c)
}

// Only NATType is reached while building a report, but the parameter is an
// interface, so embed it. If a future upstream report asks for more, the nil
// embed panics loudly rather than quietly reporting a zero.
type stubConnections struct{ connections.Service }

func (stubConnections) NATType() string { return "unknown" }

type stubModel struct{}

func (stubModel) GlobalSize(string) (db.Counts, error) { return db.Counts{}, nil }

func (stubModel) UsageReportingStats(*contract.Report, int, bool) {}
