package v1

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// tiffOrientation is a minimal little-endian TIFF block with one IFD0
// entry: Orientation = o.
func tiffOrientation(o uint16) []byte {
	b := []byte("II*\x00\x08\x00\x00\x00\x01\x00")
	e := make([]byte, 12)
	binary.LittleEndian.PutUint16(e, 0x0112)
	binary.LittleEndian.PutUint16(e[2:], 3)
	binary.LittleEndian.PutUint32(e[4:], 1)
	binary.LittleEndian.PutUint16(e[8:], o)
	return append(append(b, e...), 0, 0, 0, 0)
}

// A phone photo stored sideways (top-left quadrant red, EXIF "rotate 90
// CW") comes out upright: the red quadrant ends up top-right.
func TestAvatarAppliesEXIFOrientation(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			c := color.RGBA{0, 0, 255, 255}
			if x < 32 && y < 32 {
				c = color.RGBA{255, 0, 0, 255}
			}
			img.Set(x, y, c)
		}
	}
	var j bytes.Buffer
	if err := jpeg.Encode(&j, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	app1 := append([]byte("Exif\x00\x00"), tiffOrientation(6)...)
	seg := append([]byte{0xFF, 0xE1, byte((len(app1) + 2) >> 8), byte(len(app1) + 2)}, app1...)
	raw := append(append([]byte{0xFF, 0xD8}, seg...), j.Bytes()[2:]...)
	if o := exifOrientation(raw); o != 6 {
		t.Fatalf("orientation %d, want 6", o)
	}
	out, msg := processAvatar(raw)
	if msg != "" {
		t.Fatal(msg)
	}
	got, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	red := func(x, y int) bool { r, _, b, _ := got.At(x, y).RGBA(); return r > b }
	if !red(384, 128) || red(128, 128) || red(384, 384) {
		t.Fatal("not rotated upright")
	}
	if exifOrientation(j.Bytes()) != 1 {
		t.Fatal("no EXIF must mean upright")
	}
}

func TestEXIFOrientationInWebPAndJunk(t *testing.T) {
	exif := append([]byte("Exif\x00\x00"), tiffOrientation(8)...)
	chunk := append([]byte("EXIF\x00\x00\x00\x00"), exif...)
	binary.LittleEndian.PutUint32(chunk[4:], uint32(len(exif)))
	webp := append([]byte("RIFF\x00\x00\x00\x00WEBPVP8X\x0a\x00\x00\x00"), make([]byte, 10)...)
	webp = append(webp, chunk...)
	if o := exifOrientation(webp); o != 8 {
		t.Fatalf("webp orientation %d, want 8", o)
	}
	for _, junk := range [][]byte{nil, {0xFF, 0xD8, 0xFF, 0xE1, 0xFF, 0xFF}, []byte("RIFF\x00\x00\x00\x00WEBPEXIF\xff\xff\xff\xff"), append([]byte{0xFF, 0xD8, 0xFF, 0xE1, 0, 16}, "Exif\x00\x00II*\x00\xff\xff\xff\xff"...)} {
		if o := exifOrientation(junk); o != 1 {
			t.Fatalf("junk %q -> %d", junk, o)
		}
	}
	// Each orientation's transform, on a 2x2 with one marked pixel at (0,0):
	// where the stored top-left corner ends up once upright.
	want := map[int]image.Point{1: {0, 0}, 2: {1, 0}, 3: {1, 1}, 4: {0, 1}, 5: {0, 0}, 6: {1, 0}, 7: {1, 1}, 8: {0, 1}}
	for o, p := range want {
		src := image.NewRGBA(image.Rect(0, 0, 2, 2))
		src.SetRGBA(0, 0, color.RGBA{255, 0, 0, 255})
		if got := orient(src, o); got.RGBAAt(p.X, p.Y).R != 255 {
			t.Errorf("orientation %d: marked pixel not at %v", o, p)
		}
	}
}
