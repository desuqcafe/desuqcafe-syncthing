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
// The History and Conflicts screens show a thumbnail beside every version of
// a texture, and until now showed a grey square beside every version of the
// thing the modellers actually care about. Blender writes a small preview
// into every .blend it saves -- the same picture its own file browser and
// Explorer's thumbnail handler show -- so the picture is already in the file.
// This reads it out.
//
// THE FORMAT, AS FAR AS THIS NEEDS IT
//
// A .blend is a header followed by a flat list of blocks, each a block header
// (a four-byte code and a length, among other things) and that many bytes.
// Blender writes them in a fixed order at the top of the file: one REND block
// per scene, then TEST -- the thumbnail -- if there is one, then GLOB. So the
// reader stops at GLOB: whatever the file's size, it never looks past the
// first few kilobytes of it. TEST is two int32s, width and height, then that
// many RGBA pixels, bottom row first.
//
// There are two header layouts. Up to Blender 4.x it is twelve bytes,
// "BLENDER" + pointer size ('_' four, '-' eight) + endianness ('v' little,
// 'V' big) + three version digits, and block headers are 20 or 24 bytes by
// pointer size. From 5.0 it is "BLENDER17-01v0500": header length, a format
// version, endianness and four version digits, and format 1 widens the block
// header to 32 bytes with a 64-bit length in a different position.
//
// COMPRESSION, AND WHY THERE IS AN ELF FILE IN HERE
//
// Blender compressed .blend files with gzip until 3.0 and with zstd since,
// and 5.x compresses by default: every 5.x file on the development machine
// this was written on began 28 B5 2F FD. So zstd is not an edge case.
//
// The standard library has a zstd decoder, internal/zstd, and does not export
// it. The alternative is a new dependency in the root go.mod, which the fork
// avoids because every upstream dependency bump would then conflict on it
// (see CLAUDE.md). But debug/elf *is* exported, and it opens zstd-compressed
// ELF sections with exactly that decoder. So zstdReader wraps the compressed
// bytes in the smallest ELF file debug/elf will accept -- a header and two
// section headers -- and asks for the section's contents. It is forty lines of
// arithmetic against a documented, stable file format, and it is tested
// against real Blender 5 files. If the root module ever gains a zstd package
// for its own reasons, replace this with that.

package api

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"debug/elf"
	"encoding/binary"
	"errors"
	"image"
	"io"
)

const (
	// blendThumbMaxEdge rejects a thumbnail claiming to be larger than this.
	// Blender writes 128 or 256 pixels on a side; anything past 1024 is not a
	// thumbnail and would be a large allocation taken on the file's word.
	blendThumbMaxEdge = 1024

	// blendThumbMaxBlocks bounds the walk. The thumbnail follows one REND
	// block per scene, so this is a file with a hundred scenes and no
	// thumbnail -- or not a .blend at all.
	blendThumbMaxBlocks = 128

	// blendThumbMaxSkip bounds how much of the decompressed stream the walk
	// will read past before giving up. REND blocks are tens of bytes each.
	blendThumbMaxSkip = 8 << 20
)

var errBlendNoThumb = errors.New("this .blend has no preview picture")

// blendThumbnail reads the preview picture out of a .blend.
func blendThumbnail(r io.ReaderAt, size int64) (image.Image, error) {
	var magic [4]byte
	if _, err := r.ReadAt(magic[:], 0); err != nil {
		return nil, err
	}
	var stream io.Reader
	switch {
	case magic[0] == 0x1f && magic[1] == 0x8b:
		gz, err := gzip.NewReader(io.NewSectionReader(r, 0, size))
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		stream = gz
	case bytes.Equal(magic[:], []byte{0x28, 0xb5, 0x2f, 0xfd}):
		zr, err := zstdReader(r, size)
		if err != nil {
			return nil, err
		}
		stream = zr
	default:
		stream = io.NewSectionReader(r, 0, size)
	}
	return blendThumbnailStream(bufio.NewReader(io.LimitReader(stream, blendThumbMaxSkip)))
}

// blendThumbnailStream walks the uncompressed block list.
func blendThumbnailStream(r io.Reader) (image.Image, error) {
	var head [17]byte
	if _, err := io.ReadFull(r, head[:12]); err != nil {
		return nil, errPreviewUnsupported
	}
	if string(head[:7]) != "BLENDER" {
		return nil, errPreviewUnsupported
	}

	var (
		order  binary.ByteOrder
		layout int // block header size: 20, 24 or 32
	)
	endian := byte(0)
	switch head[7] {
	case '_', '-':
		// Legacy: pointer size, endianness, three version digits.
		layout = 20
		if head[7] == '-' {
			layout = 24
		}
		endian = head[8]
	default:
		// 5.0 and later: "BLENDER" + two digits of header length, then
		// "-01v0500". Only header length 17 and format 1 exist.
		if string(head[7:9]) != "17" {
			return nil, errPreviewUnsupported
		}
		if _, err := io.ReadFull(r, head[12:17]); err != nil {
			return nil, errPreviewUnsupported
		}
		if head[9] != '-' || string(head[10:12]) != "01" {
			return nil, errPreviewUnsupported
		}
		layout = 32
		endian = head[12]
	}
	switch endian {
	case 'v':
		order = binary.LittleEndian
	case 'V':
		order = binary.BigEndian
	default:
		return nil, errPreviewUnsupported
	}

	bh := make([]byte, layout)
	for i := 0; i < blendThumbMaxBlocks; i++ {
		if _, err := io.ReadFull(r, bh); err != nil {
			return nil, errBlendNoThumb
		}
		code := string(bh[:4])
		var length int64
		switch layout {
		case 20, 24:
			// code, len, old pointer, SDNA index, count.
			length = int64(int32(order.Uint32(bh[4:8])))
		case 32:
			// code, SDNA index, old pointer, len (64-bit), count.
			length = int64(order.Uint64(bh[16:24]))
		}
		if length < 0 {
			return nil, errPreviewUnsupported
		}

		switch code {
		case "TEST":
			return blendReadThumb(r, order, length)
		case "GLOB", "DNA1", "ENDB":
			// The thumbnail comes before GLOB or not at all.
			return nil, errBlendNoThumb
		}
		if _, err := io.CopyN(io.Discard, r, length); err != nil {
			return nil, errBlendNoThumb
		}
	}
	return nil, errBlendNoThumb
}

// blendReadThumb decodes a TEST block's body.
func blendReadThumb(r io.Reader, order binary.ByteOrder, length int64) (image.Image, error) {
	var dims [8]byte
	if length < 8 {
		return nil, errPreviewUnsupported
	}
	if _, err := io.ReadFull(r, dims[:]); err != nil {
		return nil, errPreviewUnsupported
	}
	w := int(int32(order.Uint32(dims[0:4])))
	h := int(int32(order.Uint32(dims[4:8])))
	if w <= 0 || h <= 0 || w > blendThumbMaxEdge || h > blendThumbMaxEdge {
		return nil, errPreviewUnsupported
	}
	// Blender's own extractor insists on an exact match, and so does this:
	// a block whose size disagrees with its dimensions is not one to trust
	// the pixels of.
	if length-8 != int64(w)*int64(h)*4 {
		return nil, errPreviewUnsupported
	}

	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	stride := w * 4
	// Stored bottom row first, the way Blender's image buffers are.
	for y := h - 1; y >= 0; y-- {
		if _, err := io.ReadFull(r, img.Pix[y*img.Stride:y*img.Stride+stride]); err != nil {
			return nil, errPreviewUnsupported
		}
	}
	return img, nil
}

// zstdReader decompresses a zstd stream using the decoder debug/elf carries.
// See the file comment for why it is done this way.
//
// The ELF file is built in memory around the compressed bytes, which are
// read through r and never copied: a 64-byte ELF header, the compressed
// section (a 24-byte compression header then the stream), and two 64-byte
// section headers -- the mandatory null one and ours. No string table, which
// debug/elf allows (shstrndx 0).
func zstdReader(r io.ReaderAt, size int64) (io.Reader, error) {
	const (
		ehSize   = 64
		chSize   = 24
		shSize   = 64
		dataOff  = ehSize
		streamAt = dataOff + chSize
	)
	secSize := chSize + size
	shOff := dataOff + secSize

	var eh [ehSize]byte
	copy(eh[:], []byte{0x7f, 'E', 'L', 'F', byte(elf.ELFCLASS64), byte(elf.ELFDATA2LSB), byte(elf.EV_CURRENT)})
	le := binary.LittleEndian
	le.PutUint16(eh[16:], uint16(elf.ET_REL))
	le.PutUint16(eh[18:], uint16(elf.EM_X86_64))
	le.PutUint32(eh[20:], uint32(elf.EV_CURRENT))
	le.PutUint64(eh[40:], uint64(shOff)) // e_shoff
	le.PutUint16(eh[52:], ehSize)        // e_ehsize
	le.PutUint16(eh[58:], shSize)        // e_shentsize
	le.PutUint16(eh[60:], 2)             // e_shnum
	le.PutUint16(eh[62:], 0)             // e_shstrndx: no names

	var ch [chSize]byte
	le.PutUint32(ch[0:], uint32(elf.COMPRESS_ZSTD))
	// ch_size is the uncompressed size, which only bounds Seek. Reads are
	// streamed and stop at the stream's own end, or at the caller's limit.
	le.PutUint64(ch[8:], 1<<62)
	le.PutUint64(ch[16:], 1)

	var sh [2 * shSize]byte
	s := sh[shSize:]
	le.PutUint32(s[4:], uint32(elf.SHT_PROGBITS))
	le.PutUint64(s[8:], uint64(elf.SHF_COMPRESSED))
	le.PutUint64(s[24:], dataOff)         // sh_offset
	le.PutUint64(s[32:], uint64(secSize)) // sh_size
	le.PutUint64(s[48:], 1)               // sh_addralign

	file := &concatReaderAt{parts: []readerAtPart{
		{bytes.NewReader(eh[:]), 0, ehSize},
		{bytes.NewReader(ch[:]), dataOff, chSize},
		{r, streamAt, size},
		{bytes.NewReader(sh[:]), shOff, 2 * shSize},
	}}
	ef, err := elf.NewFile(file)
	if err != nil {
		return nil, err
	}
	if len(ef.Sections) != 2 {
		return nil, errPreviewUnsupported
	}
	return ef.Sections[1].Open(), nil
}

// concatReaderAt presents contiguous parts as one io.ReaderAt.
type concatReaderAt struct {
	parts []readerAtPart
}

type readerAtPart struct {
	r    io.ReaderAt
	off  int64
	size int64
}

func (c *concatReaderAt) ReadAt(p []byte, off int64) (int, error) {
	n := 0
	for _, part := range c.parts {
		if len(p) == 0 {
			break
		}
		if off+int64(n) >= part.off+part.size || off+int64(n) < part.off {
			continue
		}
		rel := off + int64(n) - part.off
		want := p
		if int64(len(want)) > part.size-rel {
			want = want[:part.size-rel]
		}
		m, err := part.r.ReadAt(want, rel)
		n += m
		p = p[m:]
		if err != nil && !(errors.Is(err, io.EOF) && m == len(want)) {
			return n, err
		}
	}
	if len(p) > 0 {
		return n, io.EOF
	}
	return n, nil
}
