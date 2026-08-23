package main

import (
	"strings"
	"testing"
)

func TestDesktopIniShape(t *testing.T) {
	got := desktopIniFor(`C:\Users\yuki\AppData\Local\desuqcafe-syncthing\folder.ico`,
		"Project Assets")

	for _, want := range []string{
		"[.ShellClassInfo]",
		`IconResource=C:\Users\yuki\AppData\Local\desuqcafe-syncthing\folder.ico,0`,
		"InfoTip=Project Assets -- synced by " + appName,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("desktop.ini is missing %q:\n%s", want, got)
		}
	}

	// GetPrivateProfileString, not Go, reads this file.
	if strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Errorf("desktop.ini has a bare LF in it:\n%q", got)
	}

	// A folder with no label falls back to something that still says what the
	// folder is, rather than to an empty tooltip.
	if tip := desktopIniFor("x.ico", ""); !strings.Contains(tip, "InfoTip=Synced by ") {
		t.Errorf("unlabelled folder got no usable tooltip:\n%s", tip)
	}
}

func TestUTF16LEHasBOM(t *testing.T) {
	// Without the byte-order mark the shell reads the file in the system code
	// page, and any non-ASCII character in the icon path -- a user whose name
	// is not spelled in ASCII, which is the whole reason for the encoding --
	// resolves to a file that does not exist.
	got := utf16LE("AB")
	want := []byte{0xFF, 0xFE, 'A', 0x00, 'B', 0x00}
	if len(got) != len(want) {
		t.Fatalf("got %d bytes, want %d: % X", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("byte %d: got %#x want %#x (% X)", i, got[i], want[i], got)
		}
	}

	// And it must actually survive a non-ASCII path.
	jorg := utf16LE("Jörg")
	if len(jorg) != 2+4*2 {
		t.Errorf("non-ASCII path encoded to %d bytes, want %d: % X", len(jorg), 2+4*2, jorg)
	}
}

func TestIgnoreDetection(t *testing.T) {
	cases := []struct {
		name    string
		lines   []string
		covered bool
		ignored bool
	}{
		{"empty", nil, false, false},
		{"unrelated", []string{"(?d)*.blend[0-9]", "(?d)Thumbs.db"}, false, false},
		{"the seeded line", []string{"(?d)desktop.ini"}, true, true},
		{"bare", []string{"desktop.ini"}, true, true},
		{"different case", []string{"(?d)Desktop.INI"}, true, true},
		// Somebody who has written this means it, and a folder icon is not
		// worth overruling them for.
		{"deliberately synced", []string{"!desktop.ini", "*"}, true, false},
		// A comment mentioning it is not a rule.
		{"only a comment", []string{"// desktop.ini is junk", "(?d)Thumbs.db"}, false, false},
		{"among others", []string{"(?d)*.tmp", "(?d)desktop.ini", "(?d)~*"}, true, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ignoresCoverDesktopIni(c.lines); got != c.covered {
				t.Errorf("ignoresCoverDesktopIni = %v, want %v", got, c.covered)
			}
			if got := wantsIgnored(c.lines); got != c.ignored {
				t.Errorf("wantsIgnored = %v, want %v", got, c.ignored)
			}
		})
	}
}

func TestAppendedIgnoreIsWhatTheSeedWrites(t *testing.T) {
	// custom/scripts/seed-config.ps1 puts "(?d)desktop.ini" in the default
	// ignores. If the tray appended a differently-spelled equivalent, a seeded
	// install would end up with two lines saying the same thing, and the next
	// person to read the file would reasonably wonder which one mattered.
	if !ignoresCoverDesktopIni([]string{desktopIniIgnore}) {
		t.Fatal("the tray would not recognise its own appended line")
	}
	if !wantsIgnored([]string{desktopIniIgnore}) {
		t.Fatal("the tray's appended line does not actually ignore anything")
	}
	if desktopIniIgnore != "(?d)desktop.ini" {
		t.Errorf("appended line is %q; seed-config.ps1 writes (?d)desktop.ini",
			desktopIniIgnore)
	}
}
