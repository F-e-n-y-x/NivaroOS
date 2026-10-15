package main

// X11 input straight through XTEST (pure Go, no cgo, no x11vnc in the input path).

import (
	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"
)

type xinput struct {
	c    *xgb.Conn
	root xproto.Window
	keys map[uint32]byte // keysym -> keycode
	mask int
}

func newXInput(display string) (*xinput, error) {
	c, err := xgb.NewConnDisplay(display)
	if err != nil {
		return nil, err
	}
	if err := xtest.Init(c); err != nil {
		c.Close()
		return nil, err
	}
	setup := xproto.Setup(c)
	x := &xinput{c: c, root: setup.DefaultScreen(c).Root, keys: map[uint32]byte{}}
	n := byte(setup.MaxKeycode - setup.MinKeycode + 1)
	km, err := xproto.GetKeyboardMapping(c, setup.MinKeycode, n).Reply()
	if err != nil {
		c.Close()
		return nil, err
	}
	per := int(km.KeysymsPerKeycode)
	for i := 0; i < int(n); i++ {
		for j := 0; j < per && j < 2; j++ { // plain + shifted level
			ks := uint32(km.Keysyms[i*per+j])
			if _, ok := x.keys[ks]; ks != 0 && !ok {
				x.keys[ks] = byte(int(setup.MinKeycode) + i)
			}
		}
	}
	return x, nil
}

func (x *xinput) pointer(px, py, mask int) {
	xtest.FakeInput(x.c, xproto.MotionNotify, 0, 0, x.root, int16(px), int16(py), 0)
	for b := 0; b < 5; b++ {
		bit := 1 << b
		if (mask^x.mask)&bit != 0 {
			t := byte(xproto.ButtonRelease)
			if mask&bit != 0 {
				t = xproto.ButtonPress
			}
			xtest.FakeInput(x.c, t, byte(b+1), 0, x.root, 0, 0, 0)
		}
	}
	x.mask = mask
	x.c.Sync()
}

// ponytail: keysyms missing from the keymap are dropped; production remaps a spare keycode (x11vnc -add_keysyms does this).
func (x *xinput) key(ks uint32, down bool) {
	kc, ok := x.keys[ks]
	if !ok {
		return
	}
	t := byte(xproto.KeyRelease)
	if down {
		t = xproto.KeyPress
	}
	xtest.FakeInput(x.c, t, kc, 0, x.root, 0, 0, 0)
	x.c.Sync()
}
