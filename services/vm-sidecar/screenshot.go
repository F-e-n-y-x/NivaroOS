// screenshot.go grabs a still frame of a running VM's display via
// libvirt's screenshot API, for the VM Manager's live preview thumbnails -
// no VNC/noVNC connection needed just to show what a VM's screen looks
// like right now.
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"strings"
)

// Screenshot grabs the current framebuffer of screen 0 and returns it as
// PNG bytes. Which raw format libvirt hands back over the stream is
// hypervisor/version-dependent - documented as PPM for QEMU historically,
// but observed returning PNG directly on newer builds - so the MIME type
// Domain.Screenshot() itself reports is checked rather than assumed.
func (s *LibvirtStore) Screenshot(name string) ([]byte, error) {
	dom, err := s.lookup(name)
	if err != nil {
		return nil, err
	}
	defer dom.Free()

	conn, err := s.getConn()
	if err != nil {
		return nil, err
	}
	stream, err := conn.NewStream(0)
	if err != nil {
		return nil, err
	}
	defer stream.Free()

	mime, err := dom.Screenshot(stream, 0, 0)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	chunk := make([]byte, 64*1024)
	for {
		n, recvErr := stream.Recv(chunk)
		if n > 0 {
			buf.Write(chunk[:n])
		}
		if recvErr == io.EOF {
			break
		}
		if recvErr != nil {
			return nil, recvErr
		}
	}
	stream.Finish()

	if strings.Contains(mime, "png") {
		return buf.Bytes(), nil
	}

	img, err := decodePPM(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("decode %s screenshot: %w", mime, err)
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, fmt.Errorf("encode screenshot: %w", err)
	}
	return out.Bytes(), nil
}

// decodePPM parses a binary PPM (P6) image - width/height/maxval as
// whitespace-delimited ASCII tokens (comments allowed), then exactly one
// delimiter byte, then raw RGB triples.
func decodePPM(data []byte) (image.Image, error) {
	r := bufio.NewReader(bytes.NewReader(data))
	magic, err := readPPMToken(r)
	if err != nil {
		return nil, err
	}
	if magic != "P6" {
		return nil, fmt.Errorf("unsupported format %q (expected P6 PPM)", magic)
	}
	width, err := readPPMIntToken(r)
	if err != nil {
		return nil, err
	}
	height, err := readPPMIntToken(r)
	if err != nil {
		return nil, err
	}
	maxVal, err := readPPMIntToken(r)
	if err != nil {
		return nil, err
	}
	if maxVal != 255 {
		return nil, fmt.Errorf("unsupported maxval %d (expected 255)", maxVal)
	}

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	row := make([]byte, width*3)
	for y := 0; y < height; y++ {
		if _, err := io.ReadFull(r, row); err != nil {
			return nil, err
		}
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{row[x*3], row[x*3+1], row[x*3+2], 255})
		}
	}
	return img, nil
}

func readPPMToken(r *bufio.Reader) (string, error) {
	var tok []byte
	for {
		b, err := r.ReadByte()
		if err != nil {
			return "", err
		}
		if b == '#' {
			for {
				b, err := r.ReadByte()
				if err != nil {
					return "", err
				}
				if b == '\n' {
					break
				}
			}
			continue
		}
		if b == ' ' || b == '\t' || b == '\n' || b == '\r' {
			if len(tok) > 0 {
				return string(tok), nil
			}
			continue
		}
		tok = append(tok, b)
	}
}

func readPPMIntToken(r *bufio.Reader) (int, error) {
	tok, err := readPPMToken(r)
	if err != nil {
		return 0, err
	}
	var n int
	if _, err := fmt.Sscanf(tok, "%d", &n); err != nil {
		return 0, fmt.Errorf("invalid PPM header token %q: %w", tok, err)
	}
	return n, nil
}

func RegisterScreenshotRoutes(mux *http.ServeMux, store *LibvirtStore) {
	mux.HandleFunc("GET /vms/{name}/screenshot", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		png, err := store.Screenshot(name)
		if err != nil {
			if isNotFound(err) {
				writeError(w, http.StatusNotFound, err)
				return
			}
			// Most common failure: VM isn't running, so there's no
			// framebuffer to grab yet - a client-side "expected" case,
			// not a server error.
			writeError(w, http.StatusBadRequest, err)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(png)
	})
}
