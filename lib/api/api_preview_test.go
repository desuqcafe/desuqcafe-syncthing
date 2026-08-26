// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// desuqcafe fork. See api_preview.go.

package api

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestPreviewSupported(t *testing.T) {
	cases := map[string]bool{
		"a.png":             true,
		"a.PNG":             true,
		"refs/chair.jpeg":   true,
		`refs\chair.JPG`:    true,
		"loop.gif":          true,
		"scene.blend":       false,
		"texture.tga":       false,
		"noextension":       false,
		"":                  false,
		"archive.png.blend": false,
	}
	for in, want := range cases {
		if got := previewSupported(in); got != want {
			t.Errorf("previewSupported(%q) = %v, want %v", in, got, want)
		}
	}
}

// A thumbnail keeps the aspect ratio and never grows a small image -- a 32px
// icon rendered at 128 would be a blurry lie about its resolution.
func TestPreviewThumbnailBounds(t *testing.T) {
	cases := []struct{ w, h, wantW, wantH int }{
		{400, 200, 128, 64},
		{200, 400, 64, 128},
		{300, 300, 128, 128},
		{100, 40, 100, 40}, // already small enough: untouched
		{1, 1, 1, 1},
	}
	for _, tc := range cases {
		src := image.NewRGBA(image.Rect(0, 0, tc.w, tc.h))
		got := previewThumbnail(src, previewMaxEdge).Bounds()
		if got.Dx() != tc.wantW || got.Dy() != tc.wantH {
			t.Errorf("%dx%d -> %dx%d, want %dx%d", tc.w, tc.h, got.Dx(), got.Dy(), tc.wantW, tc.wantH)
		}
	}
}

// Averaging happens in the premultiplied space, so a transparent region stays
// transparent instead of being dragged towards whatever colour is behind it.
// This is the case that matters for textures cut out against nothing.
func TestPreviewThumbnailKeepsTransparency(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 256, 256))
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			if x < 128 {
				src.SetRGBA(x, y, color.RGBA{R: 200, G: 0, B: 0, A: 255})
			}
			// The right half is left at the zero value: fully transparent.
		}
	}

	thumb := previewThumbnail(src, 64)
	if _, _, _, a := thumb.At(4, 32).RGBA(); a != 0xffff {
		t.Errorf("left half alpha = %d, want opaque", a)
	}
	if _, _, _, a := thumb.At(60, 32).RGBA(); a != 0 {
		t.Errorf("right half alpha = %d, want transparent", a)
	}
}

// The output format follows the input, because only one of the two can carry
// an alpha channel.
func TestPreviewEncodeFormats(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 300, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 300; x++ {
			src.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 40, A: 255})
		}
	}

	var asPNG, asJPEG bytes.Buffer
	if err := png.Encode(&asPNG, src); err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(&asJPEG, src, nil); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		in   []byte
		want string
	}{
		{"png in, png out", asPNG.Bytes(), "image/png"},
		{"jpeg in, jpeg out", asJPEG.Bytes(), "image/jpeg"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, ct, err := previewEncode(bytes.NewReader(tc.in))
			if err != nil {
				t.Fatal(err)
			}
			if ct != tc.want {
				t.Errorf("content type = %q, want %q", ct, tc.want)
			}
			if len(body) >= len(tc.in) {
				t.Errorf("thumbnail is %d bytes, source was %d -- it should be smaller", len(body), len(tc.in))
			}
			out, _, err := image.Decode(bytes.NewReader(body))
			if err != nil {
				t.Fatalf("thumbnail does not decode: %v", err)
			}
			if out.Bounds().Dx() != previewMaxEdge {
				t.Errorf("thumbnail is %d wide, want %d", out.Bounds().Dx(), previewMaxEdge)
			}
		})
	}
}

// Anything that is not an image is refused rather than served: a .png that is
// really something else renamed reaches this path, and so does a copy that is
// still being written.
func TestPreviewEncodeRefusesNonImage(t *testing.T) {
	if _, _, err := previewEncode(bytes.NewReader([]byte("this is not an image"))); err == nil {
		t.Error("expected an error for a non-image")
	}
	if _, _, err := previewEncode(bytes.NewReader(nil)); err == nil {
		t.Error("expected an error for an empty file")
	}
}
