package main

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
)

func TestPickWaylandBackend(t *testing.T) {
	cases := []struct {
		procs   []string
		desktop string
		want    string
	}{
		{[]string{"gnome-shell", "Xwayland"}, "ubuntu:GNOME", "mutter"},
		{nil, "GNOME", "mutter"}, // /proc hidden: logind's Desktop= decides
		{[]string{"sway", "foot"}, "", "wayvnc"},
		{[]string{"Hyprland"}, "Hyprland", "wayvnc"},
		{nil, "labwc:wlroots", "wayvnc"},
		{[]string{"kwin_wayland", "plasmashell"}, "KDE", "portal"},
		{nil, "COSMIC", "portal"},
	}
	for _, c := range cases {
		procs := map[string]bool{}
		for _, p := range c.procs {
			procs[p] = true
		}
		if got := pickWaylandBackend(procs, c.desktop); got != c.want {
			t.Errorf("%v %q: got %s, want %s", c.procs, c.desktop, got, c.want)
		}
	}
}

func TestDirtyRectsMergesRuns(t *testing.T) {
	// 3x2 tiles for 150x100: row 0 tiles 0,1 dirty, row 1 tile 2 dirty.
	got := dirtyRects([]bool{true, true, false, false, false, true}, 150, 100)
	want := []rfbRect{{x: 0, y: 0, w: 128, h: 64}, {x: 128, y: 64, w: 22, h: 36}}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		g := got[i]
		if g.x != want[i].x || g.y != want[i].y || g.w != want[i].w || g.h != want[i].h {
			t.Errorf("rect %d: got %+v want %+v", i, g, want[i])
		}
	}
}

func TestConvertRectNoVNCFormat(t *testing.T) {
	fb := []byte{1, 2, 3, 0} // B G R x
	// noVNC: 32bpp little endian, red at 0, green 8, blue 16.
	pf := pixelFormat{BPP: 32, Depth: 24, TrueColor: 1, RMax: 255, GMax: 255, BMax: 255, RShift: 0, GShift: 8, BShift: 16}
	if got := convertRect(fb, 1, 0, 0, 1, 1, pf); !bytes.Equal(got, []byte{3, 2, 1, 0}) {
		t.Fatalf("got %v", got)
	}
	if got := convertRect(fb, 1, 0, 0, 1, 1, serverPF); !bytes.Equal(got, fb) {
		t.Fatalf("server format must pass through, got %v", got)
	}
}

type fakeInput struct{ events chan string }

func (f fakeInput) Motion(x, y float64)          { f.events <- fmt.Sprintf("move %v,%v", x, y) }
func (f fakeInput) Button(b int32, down bool)    { f.events <- fmt.Sprintf("button %#x %v", b, down) }
func (f fakeInput) Scroll(axis uint32, n int32)  { f.events <- fmt.Sprintf("scroll %d %d", axis, n) }
func (f fakeInput) Key(keysym uint32, down bool) { f.events <- fmt.Sprintf("key %#x %v", keysym, down) }

func TestRFBServerSessionRawZlibAndInput(t *testing.T) {
	in := fakeInput{events: make(chan string, 32)}
	srv := newRFBServer(in)
	frame := bytes.Repeat([]byte{10, 20, 30, 0}, 4*2)
	srv.pushFrame(4, 2, frame)

	server, client := net.Pipe()
	go srv.handle(server)
	defer client.Close()
	read := func(n int) []byte {
		b := make([]byte, n)
		if _, err := io.ReadFull(client, b); err != nil {
			t.Fatalf("read %d: %v", n, err)
		}
		return b
	}
	write := func(b ...byte) {
		if _, err := client.Write(b); err != nil {
			t.Fatal(err)
		}
	}

	if v := string(read(12)); v != "RFB 003.008\n" {
		t.Fatalf("version %q", v)
	}
	write([]byte("RFB 003.008\n")...)
	if sec := read(2); sec[0] != 1 || sec[1] != 1 {
		t.Fatalf("security types %v", sec)
	}
	write(1)
	if res := read(4); binary.BigEndian.Uint32(res) != 0 {
		t.Fatal("security result")
	}
	write(1)
	init := read(24)
	if w, h := binary.BigEndian.Uint16(init), binary.BigEndian.Uint16(init[2:]); w != 4 || h != 2 {
		t.Fatalf("size %dx%d", w, h)
	}
	read(int(binary.BigEndian.Uint32(init[20:])))

	// Raw, non-incremental: the whole 4x2 frame.
	write(3, 0, 0, 0, 0, 0, 0, 4, 0, 2)
	hdr := read(4)
	if hdr[0] != 0 || binary.BigEndian.Uint16(hdr[2:]) != 1 {
		t.Fatalf("update header %v", hdr)
	}
	rh := read(12)
	if enc := int32(binary.BigEndian.Uint32(rh[8:])); enc != 0 {
		t.Fatalf("encoding %d", enc)
	}
	if px := read(4 * 2 * 4); !bytes.Equal(px, frame) {
		t.Fatalf("pixels %v", px)
	}

	// Zlib + DesktopSize, then a change: only the new frame arrives, zlib'd.
	write(2, 0, 0, 2, 0, 0, 0, 6, 0xff, 0xff, 0xff, 0x21)
	write(3, 1, 0, 0, 0, 0, 0, 4, 0, 2)
	frame2 := append([]byte(nil), frame...)
	frame2[0] = 99
	srv.pushFrame(4, 2, frame2)
	hdr = read(4)
	if binary.BigEndian.Uint16(hdr[2:]) != 1 {
		t.Fatalf("want one rect, header %v", hdr)
	}
	rh = read(12)
	if enc := int32(binary.BigEndian.Uint32(rh[8:])); enc != 6 {
		t.Fatalf("want zlib, got %d", enc)
	}
	zr, err := zlib.NewReader(bytes.NewReader(read(int(binary.BigEndian.Uint32(read(4))))))
	if err != nil {
		t.Fatal(err)
	}
	px := make([]byte, len(frame2))
	if _, err := io.ReadFull(zr, px); err != nil || !bytes.Equal(px, frame2) {
		t.Fatalf("zlib pixels %v %v", px, err)
	}

	// Left press at 3,1, wheel down, release; then a key.
	write(5, 1, 0, 3, 0, 1)
	write(5, 1|16, 0, 3, 0, 1)
	write(5, 0, 0, 3, 0, 1)
	write(4, 1, 0, 0, 0, 0, 0xff, 0x0d)
	var got []string
	for len(got) < 7 {
		got = append(got, <-in.events)
	}
	want := "move 3,1|button 0x110 true|move 3,1|scroll 0 1|move 3,1|button 0x110 false|key 0xff0d true"
	if strings.Join(got, "|") != want {
		t.Fatalf("input events:\n%s\nwant\n%s", strings.Join(got, "|"), want)
	}
}

func TestGstCapsLine(t *testing.T) {
	line := "/GstPipeline:pipeline0/GstCapsFilter:nvout.GstPad:src: caps = video/x-raw, format=(string)BGRx, width=(int)1280, height=(int)800, framerate=(fraction)0/1"
	m := gstCapsRe.FindStringSubmatch(line)
	if m == nil || m[1] != "1280" || m[2] != "800" {
		t.Fatalf("got %v", m)
	}
}
