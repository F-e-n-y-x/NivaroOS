package main

import "testing"

func TestDecodePPMRejectsHeaderBiggerThanData(t *testing.T) {
	for _, h := range []string{"P6 -2 1 255\n", "P6 100000 100000 255\n"} {
		if _, err := decodePPM([]byte(h + "abcdef")); err == nil {
			t.Errorf("%q: want an error", h)
		}
	}
	img, err := decodePPM([]byte("P6 2 1 255\nabcdef"))
	if err != nil || img.Bounds().Dx() != 2 {
		t.Fatal(err)
	}
}
