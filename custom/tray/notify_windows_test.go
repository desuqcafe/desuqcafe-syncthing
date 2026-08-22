//go:build windows

package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestToastXMLEscapes(t *testing.T) {
	got := toastXML(Notification{
		Title:  `Ben & "Jo" <3`,
		Body:   "line one\nline two",
		Launch: "http://127.0.0.1:8384/?a=1&b=2",
	})

	for _, want := range []string{
		`launch="http://127.0.0.1:8384/?a=1&amp;b=2"`,
		`Ben &amp; &quot;Jo&quot; &lt;3`,
		"line one\nline two", // a real newline, not &#xA;
		`activationType="protocol"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("toastXML() missing %q\ngot: %s", want, got)
		}
	}
}

// TestLiveToast raises a real toast. There is no way to assert that Windows
// drew it -- the notification database is the user's, not ours -- so this
// asserts the whole WinRT call chain returns success, which is what actually
// breaks. Watch the corner of the screen while it runs.
//
//	DESUQ_TRAY_TEST_TOAST=1 go test -run TestLiveToast -v ./...
func TestLiveToast(t *testing.T) {
	if os.Getenv("DESUQ_TRAY_TEST_TOAST") == "" {
		t.Skip("DESUQ_TRAY_TEST_TOAST not set; skipping the live toast")
	}

	if err := registerAppID(toastAppID, appName); err != nil {
		t.Fatalf("registerAppID: %v", err)
	}

	tr := &winToaster{appID: toastAppID, reqs: make(chan Notification, 1), done: make(chan struct{})}

	// Call show() on this goroutine rather than through run(), so a failing
	// HRESULT is a test failure instead of a line in the log.
	done := make(chan error, 1)
	go func() {
		r, _, _ := procRoInitialize.Call(roInitMultithreaded)
		if err := check(r); err != nil && uint32(r) != 0x80010106 {
			done <- err
			return
		}
		defer procRoUninitialize.Call()
		done <- tr.show(Notification{
			Title:  "desuqcafe Syncthing",
			Body:   "If you can read this, toasts work. Click me to open the GUI.",
			Launch: "http://127.0.0.1:8384/",
		})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("show: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("show did not return")
	}
	t.Log("toast dispatched; look at the notification area")
}
