package main

import (
	"bytes"
	"encoding/binary"
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

// TestFolderIconUsesPngAtLargeSizes guards a 320 KB regression.
//
// An .ico entry may hold a DIB or a whole PNG file. A DIB is uncompressed, so
// the 256 and 128 pixel entries cost 264 KB and 66 KB respectively -- they
// were 90% of what used to be a 365 KB folder.ico, embedded in every copy of
// the tray. make-icons.ps1 writes those two as PNG, which Explorer has
// understood since Vista.
//
// The assertion that this still *works* is TestShellResolvesOurFolderIcon,
// which asks the shell. This one only asserts it is still being done, because
// regenerating the icons with the PNG branch dropped would look like nothing
// at all except a much larger binary.
func TestFolderIconUsesPngAtLargeSizes(t *testing.T) {
	const maxBytes = 128 * 1024
	if len(iconFolder) > maxBytes {
		t.Errorf("folder.ico is %d bytes, over the %d byte guard -- are the "+
			"128 and 256 entries DIB again? See custom/tray/icons/make-icons.ps1",
			len(iconFolder), maxBytes)
	}

	// ICONDIR: reserved(2) type(2) count(2), then count x 16-byte entries.
	if len(iconFolder) < 6 {
		t.Fatal("folder.ico is too short to be an icon")
	}
	count := int(binary.LittleEndian.Uint16(iconFolder[4:6]))
	pngSig := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

	var checked int
	for i := 0; i < count; i++ {
		e := 6 + 16*i
		if e+16 > len(iconFolder) {
			t.Fatalf("entry %d runs past the end of the file", i)
		}
		// A 256 pixel entry records its dimension as 0.
		w := int(iconFolder[e])
		if w == 0 {
			w = 256
		}
		if w < 128 {
			continue
		}
		size := int(binary.LittleEndian.Uint32(iconFolder[e+8 : e+12]))
		off := int(binary.LittleEndian.Uint32(iconFolder[e+12 : e+16]))
		if off+size > len(iconFolder) {
			t.Fatalf("%dpx entry claims %d bytes at %d, past the end", w, size, off)
		}
		if !bytes.HasPrefix(iconFolder[off:off+size], pngSig) {
			t.Errorf("the %dpx entry is not a PNG; as a DIB it costs %d KB on its own",
				w, w*w*4/1024)
		}
		checked++
	}
	if checked != 2 {
		t.Errorf("checked %d entries at 128px or above; expected 2 (128 and 256)", checked)
	}
}

// The hover text is read by somebody deciding whether to open a file, so
// other people's marks come first, and it must never break the ini: file
// names are chosen by other people and pass through oneLine like the label.
func TestFolderTipIsLive(t *testing.T) {
	rows := []claimRow{
		{Name: "Kai", Path: "scenes/cabin.blend"},
		{Name: "Kai", Path: "rig.blend"},
		{Mine: true, Path: "tree.blend"},
	}
	tip := folderTip("Project Assets", rows, &folderStatus{State: "idle"})
	want := "Project Assets -- synced by " + appName +
		". Kai is working on cabin.blend and rig.blend. You are working on tree.blend. Up to date"
	if tip != want {
		t.Errorf("tip =\n%q\nwant\n%q", tip, want)
	}
	if tip := folderTip("X", nil, &folderStatus{State: "syncing", NeedItems: 3}); !strings.HasSuffix(tip, "3 changes still to come") {
		t.Errorf("busy tip = %q", tip)
	}
	if tip := folderTip("X", nil, nil); tip != baseTip("X") {
		t.Errorf("unknown status was guessed at: %q", tip)
	}
	ini := desktopIniWithTip(`C:\i.ico`, folderTip("X", []claimRow{{Name: "Eve", Path: "a\r\nCLSID={x}.blend"}}, nil))
	if strings.Count(ini, "\r\n") != 3 {
		t.Errorf("a file name broke the ini onto a new line:\n%q", ini)
	}
}
