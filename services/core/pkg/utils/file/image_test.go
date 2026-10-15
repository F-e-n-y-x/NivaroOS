package file

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// tinyTIFF is a little-endian 8-bit grayscale TIFF header claiming w x h
// pixels in one strip, with only a few bytes of pixel data behind it.
func tinyTIFF(w, h uint32) []byte {
	var b bytes.Buffer
	le := binary.LittleEndian
	b.WriteString("II*\x00")
	binary.Write(&b, le, uint32(8))
	type ent struct {
		tag, typ uint16
		n, v     uint32
	}
	ents := []ent{
		{256, 4, 1, w},   // ImageWidth
		{257, 4, 1, h},   // ImageLength
		{258, 3, 1, 8},   // BitsPerSample
		{259, 3, 1, 1},   // Compression: none
		{262, 3, 1, 1},   // Photometric: BlackIsZero
		{273, 4, 1, 128}, // StripOffsets
		{277, 3, 1, 1},   // SamplesPerPixel
		{278, 4, 1, h},   // RowsPerStrip
		{279, 4, 1, w * h},
	}
	binary.Write(&b, le, uint16(len(ents)))
	for _, e := range ents {
		binary.Write(&b, le, e)
	}
	binary.Write(&b, le, uint32(0))
	for b.Len() < 128+16 {
		b.WriteByte(0x80)
	}
	return b.Bytes()
}

func writeTemp(t *testing.T, name string, data []byte) string {
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// A few hundred bytes claiming a 200000x200000 canvas made the decoder
// allocate 40 GB before noticing the data is missing.
func TestThumbnailRejectsHugeCanvas(t *testing.T) {
	p := writeTemp(t, "bomb.tiff", tinyTIFF(200000, 200000))
	if _, err := GetImage(p, 100, 0); err == nil {
		t.Fatal("want an error for a 40-gigapixel image")
	}
}

func TestThumbnailOfAPNG(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 400, 200))
	for i := range src.Pix {
		src.Pix[i] = 0xff
	}
	src.Set(0, 0, color.NRGBA{A: 0})
	var buf bytes.Buffer
	png.Encode(&buf, src)
	out, err := GetImage(writeTemp(t, "a.png", buf.Bytes()), 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	img, format, err := image.Decode(bytes.NewReader(out))
	if err != nil || format != "png" || img.Bounds().Dx() != 100 || img.Bounds().Dy() != 50 {
		t.Fatalf("got %v %s %v", err, format, img)
	}
}

func TestThumbnailOfAGrayTIFF(t *testing.T) {
	out, err := GetImage(writeTemp(t, "g.tif", tinyTIFF(4, 4)), 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := image.Decode(bytes.NewReader(out)); err != nil {
		t.Fatal(err)
	}
}
