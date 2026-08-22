package main

import (
	"context"
	"log/slog"
	"os/exec"
	"sync"
	"time"
)

// supervisor keeps a Syncthing process running for as long as the tray is up.
//
// The tray replaces the bare binary in the sign-in shortcut, so it has to be
// the thing that starts Syncthing. Syncthing already runs its own monitor
// process that restarts the worker on a crash, so this layer only has to cope
// with the monitor itself going away -- rare, and worth backing off on rather
// than hammering.
type supervisor struct {
	binary string
	home   string
	// reachable reports whether a Syncthing is already answering on this home.
	// It is the guard against a respawn loop: `syncthing serve` notices an
	// existing instance, logs "Seems to already be running", and exits 0. A
	// supervisor that only watched for process exit would take that for a
	// crash and start another one every few seconds, for as long as the tray
	// was up.
	reachable func() bool

	mu      sync.Mutex
	cmd     *exec.Cmd
	stopped bool
}

func newSupervisor(binary, home string, reachable func() bool) *supervisor {
	return &supervisor{binary: binary, home: home, reachable: reachable}
}

func (s *supervisor) Run(ctx context.Context) {
	const (
		minBackoff = 5 * time.Second
		maxBackoff = 60 * time.Second
		// A process that stayed up this long counts as healthy, so the next
		// failure starts from the short delay again.
		healthy = 60 * time.Second
	)

	backoff := minBackoff
	for {
		if ctx.Err() != nil {
			return
		}

		// Somebody else's instance -- the desktop shortcut, a second tray --
		// is serving this home. Leave it alone and keep watching.
		if s.reachable != nil && s.reachable() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(minBackoff):
			}
			backoff = minBackoff
			continue
		}

		cmd := exec.Command(s.binary, "serve", "--home="+s.home, "--no-browser")
		hideWindow(cmd)

		if err := cmd.Start(); err != nil {
			slog.Error("could not start Syncthing", "err", err, "binary", s.binary)
		} else {
			slog.Info("started Syncthing", "pid", cmd.Process.Pid)
			s.mu.Lock()
			s.cmd = cmd
			s.mu.Unlock()

			started := time.Now()
			err := cmd.Wait()

			s.mu.Lock()
			s.cmd = nil
			quitting := s.stopped
			s.mu.Unlock()

			if quitting || ctx.Err() != nil {
				return
			}
			slog.Warn("Syncthing exited", "err", err, "ran", time.Since(started).Truncate(time.Second))
			if time.Since(started) > healthy {
				backoff = minBackoff
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

// Stop asks Syncthing to shut down through its own API, which closes the
// database cleanly, and only kills the process if that does not take.
//
// The shutdown request goes out whether or not this supervisor is the one that
// started the process. In supervised mode the tray icon *is* Syncthing as far
// as the user is concerned, so "Quit desuqcafe Syncthing" leaving a daemon
// running would be the surprising outcome, not the safe one.
func (s *supervisor) Stop(c *client) {
	s.mu.Lock()
	s.stopped = true
	cmd := s.cmd
	s.mu.Unlock()

	if c != nil {
		if err := c.shutdown(); err != nil {
			slog.Warn("shutdown request failed", "err", err)
		}
	}

	if cmd == nil || cmd.Process == nil {
		return
	}

	done := make(chan struct{})
	go func() {
		_, _ = cmd.Process.Wait()
		close(done)
	}()

	select {
	case <-done:
		slog.Info("Syncthing stopped")
	case <-time.After(10 * time.Second):
		slog.Warn("Syncthing did not stop in time; killing it")
		_ = cmd.Process.Kill()
	}
}
