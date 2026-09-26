// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

package api

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"image"
	"os"
	"strings"
	"testing"
)

// fakeBlend builds a .blend with a REND block, optionally a TEST block of
// w x h where pixel (x, y) counting from the top is {x, y, 7, 255}, and a
// GLOB block. layout is 20, 24 or 32, as in blendThumbnailStream.
func fakeBlend(t *testing.T, layout int, order binary.ByteOrder, w, h int, withThumb bool) []byte {
	t.Helper()
	var b bytes.Buffer
	endian := byte('v')
	if order == binary.BigEndian {
		endian = 'V'
	}
	switch layout {
	case 20:
		b.WriteString("BLENDER_")
		b.WriteByte(endian)
		b.WriteString("279")
	case 24:
		b.WriteString("BLENDER-")
		b.WriteByte(endian)
		b.WriteString("405")
	case 32:
		b.WriteString("BLENDER17-01")
		b.WriteByte(endian)
		b.WriteString("0502")
	}
	block := func(code string, body []byte) {
		bh := make([]byte, layout)
		copy(bh, code)
		switch layout {
		case 20, 24:
			order.PutUint32(bh[4:], uint32(len(body)))
		case 32:
			order.PutUint64(bh[16:], uint64(len(body)))
		}
		b.Write(bh)
		b.Write(body)
	}
	block("REND", make([]byte, 72))
	block("REND", make([]byte, 72))
	if withThumb {
		body := make([]byte, 8+w*h*4)
		order.PutUint32(body[0:], uint32(w))
		order.PutUint32(body[4:], uint32(h))
		for y := 0; y < h; y++ {
			row := body[8+(h-1-y)*w*4:] // stored bottom row first
			for x := 0; x < w; x++ {
				copy(row[x*4:], []byte{byte(x), byte(y), 7, 255})
			}
		}
		block("TEST", body)
	}
	block("GLOB", make([]byte, 200))
	block("ENDB", nil)
	return b.Bytes()
}

// rawZstd wraps data in a valid zstd frame made of raw (stored) blocks. It
// exercises the whole decoder path through debug/elf without needing an
// encoder, which the standard library does not have.
func rawZstd(data []byte) []byte {
	var b bytes.Buffer
	b.Write([]byte{0x28, 0xb5, 0x2f, 0xfd})
	// Frame header descriptor: no content size, not single segment, no
	// checksum, no dictionary. Window descriptor: exponent 10 -> 1 MiB.
	b.Write([]byte{0x00, 0x50})
	const maxBlock = 64 << 10
	for len(data) > 0 || b.Len() == 6 {
		n := len(data)
		if n > maxBlock {
			n = maxBlock
		}
		last := 0
		if n == len(data) {
			last = 1
		}
		hdr := uint32(n)<<3 | 0<<1 | uint32(last) // type 0 = raw
		b.Write([]byte{byte(hdr), byte(hdr >> 8), byte(hdr >> 16)})
		b.Write(data[:n])
		data = data[n:]
		if last == 1 {
			break
		}
	}
	// A skippable frame after it, as Blender's seek table is.
	b.Write([]byte{0x5e, 0x2a, 0x4d, 0x18, 4, 0, 0, 0, 1, 2, 3, 4})
	return b.Bytes()
}

func gzipped(t *testing.T, data []byte) []byte {
	var b bytes.Buffer
	gw := gzip.NewWriter(&b)
	gw.Write(data)
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestBlendThumbnail(t *testing.T) {
	type tc struct {
		name   string
		layout int
		order  binary.ByteOrder
		wrap   func([]byte) []byte
	}
	plain := func(b []byte) []byte { return b }
	cases := []tc{
		{"2.7x 32-bit", 20, binary.LittleEndian, plain},
		{"4.x", 24, binary.LittleEndian, plain},
		{"4.x big-endian", 24, binary.BigEndian, plain},
		{"5.x", 32, binary.LittleEndian, plain},
		{"5.x zstd", 32, binary.LittleEndian, rawZstd},
		{"4.x zstd", 24, binary.LittleEndian, rawZstd},
		{"2.7x gzip", 20, binary.LittleEndian, func(b []byte) []byte { return gzipped(t, b) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// 200 x 180 makes the TEST block larger than one 64 KiB zstd
			// block, so the stream crosses a block boundary.
			data := c.wrap(fakeBlend(t, c.layout, c.order, 200, 180, true))
			img, err := blendThumbnail(bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatal(err)
			}
			if img.Bounds() != image.Rect(0, 0, 200, 180) {
				t.Fatalf("bounds %v", img.Bounds())
			}
			// Top-left must be the row written as y=0, i.e. flipped back.
			for _, p := range []image.Point{{0, 0}, {199, 0}, {3, 179}, {150, 90}} {
				r, g, bb, a := img.At(p.X, p.Y).RGBA()
				if r>>8 != uint32(p.X&0xff) || g>>8 != uint32(p.Y&0xff) || bb>>8 != 7 || a>>8 != 255 {
					t.Fatalf("pixel %v = %d %d %d %d", p, r>>8, g>>8, bb>>8, a>>8)
				}
			}

			none := c.wrap(fakeBlend(t, c.layout, c.order, 0, 0, false))
			if _, err := blendThumbnail(bytes.NewReader(none), int64(len(none))); !errors.Is(err, errBlendNoThumb) {
				t.Fatalf("no thumbnail: got %v", err)
			}
		})
	}
}

func TestBlendThumbnailRefuses(t *testing.T) {
	good := fakeBlend(t, 32, binary.LittleEndian, 16, 16, true)
	testAt := bytes.Index(good, []byte("TEST"))

	// Dimensions that disagree with the block length.
	lie := bytes.Clone(good)
	binary.LittleEndian.PutUint32(lie[testAt+32:], 17)
	// A thumbnail claiming to be enormous.
	huge := bytes.Clone(good)
	binary.LittleEndian.PutUint32(huge[testAt+32:], 1<<20)
	binary.LittleEndian.PutUint32(huge[testAt+36:], 1<<20)

	for name, data := range map[string][]byte{
		"not a blend":      []byte("PNG and some bytes that go on for a while"),
		"truncated header": []byte("BLENDER"),
		"unknown format":   []byte("BLENDER17-02v0600rest"),
		"size mismatch":    lie,
		"huge":             huge,
		"truncated pixels": good[:testAt+32+8+100],
	} {
		if img, err := blendThumbnail(bytes.NewReader(data), int64(len(data))); err == nil {
			t.Errorf("%s: accepted, %v", name, img.Bounds())
		}
	}
}

// TestBlendThumbnailReal reads real files named in DESUQ_BLEND_FILES
// (semicolon-separated), for checking against what Blender actually writes.
// Every 5.x file saved on a desktop is zstd with a real entropy-coded
// stream, which the synthetic raw-block frames above do not exercise.
func TestBlendThumbnailReal(t *testing.T) {
	list := os.Getenv("DESUQ_BLEND_FILES")
	if list == "" {
		t.Skip("set DESUQ_BLEND_FILES to run against real .blend files")
	}
	for _, p := range strings.Split(list, ";") {
		f, err := os.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		st, _ := f.Stat()
		img, err := blendThumbnail(f, st.Size())
		f.Close()
		if err != nil {
			t.Errorf("%s: %v", p, err)
			continue
		}
		t.Logf("%s: %v", p, img.Bounds())
	}
}
