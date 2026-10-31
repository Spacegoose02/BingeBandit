package trickplay

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"

	"bingebandit/internal/store"
)

var bifMagic = []byte{0x89, 'B', 'I', 'F', '\r', '\n', 0x1a, '\n'}

// BIF writes the file's thumbnails as a Roku BIF archive (one JPEG per
// thumbnail with a time index), cut from the sheets and cached beside them.
// from shifts the index for a conversion that starts from seconds into the
// file, since a Roku counts from the start of what it plays; thumbnails
// before it are left out.
func (g *Generator) BIF(f *store.MediaFile, from int) (string, error) {
	in, ok := g.Info(f)
	if !ok {
		return "", os.ErrNotExist
	}
	skip := min(max(from, 0)/in.Interval, in.Count-1)
	path := filepath.Join(g.Dir(f), fmt.Sprintf("roku-%d.bif", skip))
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	var frames [][]byte
	per := in.Columns * in.Rows
	var sheet image.Image
	for n := skip; n < in.Count; n++ {
		if n%per == 0 || sheet == nil {
			r, err := os.Open(g.Sheet(f, n/per))
			if err != nil {
				return "", err
			}
			sheet, err = jpeg.Decode(r)
			r.Close()
			if err != nil {
				return "", err
			}
		}
		at := n % per
		x, y := (at%in.Columns)*in.Width, (at/in.Columns)*in.Height
		crop := image.Rect(x, y, x+in.Width, y+in.Height).Add(sheet.Bounds().Min)
		sub, ok := sheet.(interface {
			SubImage(image.Rectangle) image.Image
		})
		if !ok {
			return "", fmt.Errorf("cannot crop %T", sheet)
		}
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, sub.SubImage(crop), &jpeg.Options{Quality: 80}); err != nil {
			return "", err
		}
		frames = append(frames, buf.Bytes())
	}
	b := encodeBIF(frames, in.Interval*1000)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return "", err
	}
	return path, os.Rename(tmp, path)
}

// encodeBIF lays out a BIF file: a 64-byte header, an index of (timestamp,
// offset) pairs ending with 0xffffffff and the end offset, then the JPEGs.
// Timestamps count in units of everyMS.
func encodeBIF(frames [][]byte, everyMS int) []byte {
	header := make([]byte, 64)
	copy(header, bifMagic)
	binary.LittleEndian.PutUint32(header[8:], 0) // version
	binary.LittleEndian.PutUint32(header[12:], uint32(len(frames)))
	binary.LittleEndian.PutUint32(header[16:], uint32(everyMS))
	index := make([]byte, 8*(len(frames)+1))
	offset := uint32(len(header) + len(index))
	for i, fr := range frames {
		binary.LittleEndian.PutUint32(index[i*8:], uint32(i))
		binary.LittleEndian.PutUint32(index[i*8+4:], offset)
		offset += uint32(len(fr))
	}
	binary.LittleEndian.PutUint32(index[len(frames)*8:], 0xffffffff)
	binary.LittleEndian.PutUint32(index[len(frames)*8+4:], offset)
	out := make([]byte, 0, offset)
	out = append(out, header...)
	out = append(out, index...)
	for _, fr := range frames {
		out = append(out, fr...)
	}
	return out
}
