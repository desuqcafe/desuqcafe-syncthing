package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Exercises the two things the supervisor exists to get right, against the real
// binary in a throwaway home:
//
//   - it starts Syncthing and Stop() actually stops it, which is what "Quit"
//     in the menu does;
//   - it does not spawn a second instance when one is already serving that
//     home. `syncthing serve` notices an existing instance and exits 0, so a
//     supervisor without that guard restarts it forever.
//
// Point DESUQ_TRAY_TEST_BINARY at desuq-syncthing.exe to run these.
func testBinary(t *testing.T) string {
	t.Helper()
	bin := os.Getenv("DESUQ_TRAY_TEST_BINARY")
	if bin == "" {
		t.Skip("DESUQ_TRAY_TEST_BINARY not set; skipping supervisor tests")
	}
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("binary %q: %v", bin, err)
	}
	return bin
}

// generate creates a config and keys so the supervised instance has something
// to serve, without waiting for it to do so on first start.
func generateHome(t *testing.T, bin string) string {
	t.Helper()
	home := t.TempDir()
	cmd := exec.Command(bin, "generate", "--home="+home)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate: %v: %s", err, out)
	}
	return home
}

func waitFor(t *testing.T, what string, d time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Logf("timed out after %s waiting for %s", d, what)
	return false
}

func reachableFn(home string) func() bool {
	return func() bool {
		ep, err := readEndpoint(home)
		if err != nil {
			return false
		}
		return newClient(ep).ping() == nil
	}
}

func TestSupervisorStartsAndStops(t *testing.T) {
	bin := testBinary(t)
	home := generateHome(t, bin)
	reachable := reachableFn(home)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sup := newSupervisor(bin, home, reachable)
	go sup.Run(ctx)

	if !waitFor(t, "Syncthing to come up", 45*time.Second, reachable) {
		t.Fatal("supervisor never got Syncthing serving")
	}

	ep, err := readEndpoint(home)
	if err != nil {
		t.Fatalf("readEndpoint: %v", err)
	}
	t.Logf("serving on %s", ep.baseURL)

	sup.Stop(newClient(ep))
	cancel()

	if !waitFor(t, "Syncthing to stop", 20*time.Second, func() bool { return !reachable() }) {
		t.Fatal("Stop() left Syncthing running")
	}
	if _, err := os.Stat(filepath.Join(home, "config.xml")); err != nil {
		t.Errorf("config.xml went missing: %v", err)
	}
}

func TestSupervisorDoesNotRespawnOverARunningInstance(t *testing.T) {
	bin := testBinary(t)
	home := generateHome(t, bin)
	reachable := reachableFn(home)

	// Start one the way the desktop shortcut does, outside the supervisor.
	outside := exec.Command(bin, "serve", "--home="+home, "--no-browser")
	hideWindow(outside)
	if err := outside.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() {
		if ep, err := readEndpoint(home); err == nil {
			_ = newClient(ep).shutdown()
		}
		_, _ = outside.Process.Wait()
	})

	if !waitFor(t, "the outside instance to come up", 45*time.Second, reachable) {
		t.Fatal("the instance we started never came up")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sup := newSupervisor(bin, home, reachable)
	go sup.Run(ctx)

	// Long enough to cover several of the supervisor's 5s restart delays.
	time.Sleep(20 * time.Second)

	sup.mu.Lock()
	spawned := sup.cmd != nil
	sup.mu.Unlock()
	if spawned {
		t.Error("supervisor started a competing instance over a healthy one")
	}
	if !reachable() {
		t.Error("the original instance is no longer reachable")
	}
}
