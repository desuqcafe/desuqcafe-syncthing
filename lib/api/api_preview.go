// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

// This file is an addition made by the desuqcafe fork. See
// custom/CUSTOMIZATIONS.md.
//
// WHY THIS EXISTS
//
// Restoring from the History screen is a blind act. Staggered versioning at
// thirty days keeps roughly fifty copies of a texture that gets saved twice an
// afternoon, and the screen offers them as fifty timestamps. Nobody knows what
// they did at 14:22 on the 3rd. The only way to find out which copy is the one
// wanted is to restore it and look -- which overwrites the file you were
// trying to protect, and then you do it again.
//
// A picture answers it immediately, and this is a folder full of textures.
//
// WHY IT RE-ENCODES RATHER THAN SERVING THE FILE
//
// A 4K texture is twenty megabytes and the screen wants a ninety-six pixel
// square. Serving the original would mean the browser downloading and decoding
// twenty rows of that to draw a strip of thumbnails, over a link that for the
// modellers this build is aimed at is not always the local network. So the
// server decodes, box-samples down to a thumbnail and re-encodes -- a few
// kilobytes per row.
//
// The sampler is deliberately capped at a fixed number of samples per output
// pixel rather than averaging every source pixel: a 4096x4096 source has
// sixteen million of them and image.At is an interface call each. Bounded
// sampling makes the cost a function of the thumbnail size instead of the
// source, which is what keeps a screen of twenty of these from being a stall.
//
// WHY IT IS NOT UNDER /rest/
//
// Because it is an <img> source, and an <img> cannot send a header. Everything
// under /rest/ is behind the CSRF middleware, which admits a request only with
// a CSRF token header or an API key header -- a plain <img src> has neither and
// is answered 403. Found the direct way: the first cut of this endpoint was
// /rest/folder/preview, and every thumbnail in a real browser came back
// forbidden while every jsdom test passed, because the tests stub $http and
// never exercise the middleware.
//
// So it is mounted beside upstream's /qr/, which exists for exactly the same
// reason and has the same shape: a side-effect-free GET, outside the CSRF
// prefix, still behind the authentication middleware -- a session cookie is
// sent by an <img> where a header is not. Cross-origin, an <img> can load this
// and learn nothing from it: the pixels are unreadable to the embedding page.
//
// WHAT IT WILL NOT DO
//
// Only the image formats the standard library decodes -- PNG, JPEG and GIF.
// Not because a .blend preview would not be lovely, but because the only
// honest way to read one is a Blender header parser, and because refusing
// everything else keeps this from being a general "read any file in any
// folder" route. The GUI checks the same extension list before it asks, so an
// unsupported file costs no request at all.

package api

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"strings"
	"time"

	// GIF is decoded but never encoded, so it is the one format whose
	// decoder has to be registered with image.Decode by hand. The PNG and
	// JPEG packages above register theirs on the same import.
	_ "image/gif"

	"github.com/syncthing/syncthing/lib/fs"
	"github.com/syncthing/syncthing/lib/versioner"
)

const (
	// previewMaxEdge is the longest edge of the thumbnail produced. The
	// History screen draws them at 48 CSS pixels, so this covers a 2x display
	// and a hover-to-enlarge with room to spare.
	previewMaxEdge = 128

	// previewSamples bounds how many source pixels are read per output pixel
	// per axis. 4 means at most sixteen reads per thumbnail pixel whatever the
	// source resolution.
	previewSamples = 4

	// previewMaxBytes refuses a source file larger than this outright. A 4K
	// PNG with alpha is around forty megabytes; beyond sixty-four something
	// unusual is going on and decoding it would be a memory spike on a
	// machine that is also syncing.
	previewMaxBytes = 64 << 20

	// previewMaxPixels is the decompression-bomb rail: dimensions are read
	// from the header before any pixels are decoded, and a file claiming more
	// than this is refused without allocating for it.
	previewMaxPixels = 64 << 20
)

var (
	errPreviewUnsupported = errors.New("no preview for this kind of file")
	errPreviewTooLarge    = errors.New("that file is too large to preview")
)

// getFolderPreview serves a thumbnail of one file: the live copy, or an
// archived one with ?version=.
func (s *service) getFolderPreview(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	folder := qs.Get("folder")
	name := qs.Get("file")

	cfg, ok := s.cfg.Folders()[folder]
	if !ok {
		forkHTTPError(w, errNoSuchFolder)
		return
	}
	if name == "" || !previewSupported(name) {
		http.Error(w, errPreviewUnsupported.Error(), http.StatusUnsupportedMediaType)
		return
	}

	var (
		fd   fs.File
		info fs.FileInfo
		err  error
	)
	if v := qs.Get("version"); v != "" {
		var when time.Time
		when, err = time.Parse(time.RFC3339, v)
		if err != nil {
			http.Error(w, "version must be an RFC3339 time", http.StatusBadRequest)
			return
		}
		fd, info, err = versioner.OpenVersion(cfg, name, when)
		if errors.Is(err, versioner.ErrVersionNotFound) {
			http.Error(w, "no archived copy of that file at that time", http.StatusNotFound)
			return
		}
	} else {
		ffs := cfg.Filesystem()
		info, err = ffs.Lstat(name)
		if err == nil && !info.IsRegular() {
			err = errPreviewUnsupported
		}
		if err == nil {
			fd, err = ffs.Open(name)
		}
	}
	if err != nil {
		if fs.IsNotExist(err) {
			http.Error(w, "that file is not on this computer", http.StatusNotFound)
			return
		}
		forkHTTPError(w, err)
		return
	}
	defer fd.Close()

	if info.Size() > previewMaxBytes {
		http.Error(w, errPreviewTooLarge.Error(), http.StatusRequestEntityTooLarge)
		return
	}

	body, contentType, err := previewEncode(fd)
	if err != nil {
		// A file that does not decode is not a server fault -- a .png that is
		// really a .tga renamed reaches here, and so does a copy that is still
		// being written.
		http.Error(w, errPreviewUnsupported.Error(), http.StatusUnsupportedMediaType)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if qs.Get("version") != "" {
		// An archived copy never changes, so this one is worth keeping. The
		// live file is not: it is the thing being edited.
		w.Header().Set("Cache-Control", "private, max-age=86400")
	} else {
		w.Header().Set("Cache-Control", "private, no-cache")
	}
	w.Write(body)
}

// previewSupported is the extension gate, matching the decoders imported
// above.
func previewSupported(name string) bool {
	i := strings.LastIndexByte(name, '.')
	if i < 0 {
		return false
	}
	switch lowerASCII(name[i:]) {
	case ".png", ".jpg", ".jpeg", ".gif":
		return true
	}
	return false
}

// previewEncode reads an image and returns the thumbnail bytes.
//
// The output format follows the input rather than always being JPEG: a PNG or
// a GIF may carry transparency, and flattening that onto black is exactly the
// case -- a texture cut out against nothing -- where the thumbnail would be
// most misleading.
func previewEncode(r io.Reader) ([]byte, string, error) {
	raw, err := io.ReadAll(io.LimitReader(r, previewMaxBytes))
	if err != nil {
		return nil, "", err
	}

	// DecodeConfig reads the header only, so the pixel-count rail is applied
	// before anything is allocated for pixels.
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, "", err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > previewMaxPixels {
		return nil, "", errPreviewTooLarge
	}

	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, "", err
	}

	thumb := previewThumbnail(src, previewMaxEdge)

	var out bytes.Buffer
	switch format {
	case "png", "gif":
		if err := png.Encode(&out, thumb); err != nil {
			return nil, "", err
		}
		return out.Bytes(), "image/png", nil
	default:
		if err := jpeg.Encode(&out, thumb, &jpeg.Options{Quality: 80}); err != nil {
			return nil, "", err
		}
		return out.Bytes(), "image/jpeg", nil
	}
}

// previewThumbnail box-samples src down so its longest edge is at most max.
//
// Values are averaged in the premultiplied space image.Color.RGBA returns and
// written straight into an image.RGBA, which is premultiplied too -- so a
// transparent pixel contributes nothing to the colour of its neighbours,
// rather than dragging them towards whatever colour was left behind it.
func previewThumbnail(src image.Image, max int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return src
	}

	nw, nh := w, h
	if w > max || h > max {
		if w >= h {
			nw = max
			nh = h * max / w
		} else {
			nh = max
			nw = w * max / h
		}
	}
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		y0 := b.Min.Y + y*h/nh
		y1 := b.Min.Y + (y+1)*h/nh
		if y1 <= y0 {
			y1 = y0 + 1
		}
		// Hoisted: the row's sample coordinates do not depend on x, and this
		// loop body runs once per output pixel.
		ys := previewSteps(y0, y1)

		for x := 0; x < nw; x++ {
			x0 := b.Min.X + x*w/nw
			x1 := b.Min.X + (x+1)*w/nw
			if x1 <= x0 {
				x1 = x0 + 1
			}

			var rs, gs, bs, as, n uint64
			for _, sy := range ys {
				for _, sx := range previewSteps(x0, x1) {
					cr, cg, cb, ca := src.At(sx, sy).RGBA()
					rs += uint64(cr)
					gs += uint64(cg)
					bs += uint64(cb)
					as += uint64(ca)
					n++
				}
			}
			if n == 0 {
				continue
			}
			dst.SetRGBA(x, y, color.RGBA{
				R: uint8(rs / n >> 8),
				G: uint8(gs / n >> 8),
				B: uint8(bs / n >> 8),
				A: uint8(as / n >> 8),
			})
		}
	}
	return dst
}

// previewSteps picks at most previewSamples evenly spread coordinates from
// [lo, hi). This is what bounds the work by the thumbnail size rather than the
// source size.
func previewSteps(lo, hi int) []int {
	span := hi - lo
	if span <= previewSamples {
		out := make([]int, 0, span)
		for i := lo; i < hi; i++ {
			out = append(out, i)
		}
		return out
	}
	out := make([]int, 0, previewSamples)
	for i := 0; i < previewSamples; i++ {
		out = append(out, lo+(i*2+1)*span/(previewSamples*2))
	}
	return out
}
