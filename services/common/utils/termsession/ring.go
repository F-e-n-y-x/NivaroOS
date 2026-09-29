package termsession

import (
	"bytes"
	"unicode/utf8"
)

// Ring is a fixed-capacity byte ring holding the most recent output of a
// session (its scrollback). It is not safe for concurrent use; Session
// guards it with its own mutex.
type Ring struct {
	buf     []byte
	start   int   // index of the oldest byte
	size    int   // bytes currently held
	written int64 // total bytes ever written
}

// NewRing returns a ring that keeps the last capacity bytes (min 1).
func NewRing(capacity int) *Ring {
	if capacity < 1 {
		capacity = 1
	}
	return &Ring{buf: make([]byte, capacity)}
}

// Cap is the ring capacity in bytes.
func (r *Ring) Cap() int { return len(r.buf) }

// Len is the number of bytes currently held.
func (r *Ring) Len() int { return r.size }

// Written is the total number of bytes ever written.
func (r *Ring) Written() int64 { return r.written }

// Wrapped reports whether older output has been dropped.
func (r *Ring) Wrapped() bool { return r.written > int64(r.size) }

// Write appends p, dropping the oldest bytes once full. It never fails.
func (r *Ring) Write(p []byte) (int, error) {
	n := len(p)
	r.written += int64(n)
	c := len(r.buf)
	if n >= c {
		copy(r.buf, p[n-c:])
		r.start, r.size = 0, c
		return n, nil
	}
	end := (r.start + r.size) % c
	first := copy(r.buf[end:], p)
	if first < n {
		copy(r.buf, p[first:])
	}
	r.size += n
	if r.size > c {
		r.start = (r.start + r.size - c) % c
		r.size = c
	}
	return n, nil
}

// Bytes returns a copy of the held bytes, oldest first.
func (r *Ring) Bytes() []byte {
	out := make([]byte, r.size)
	c := len(r.buf)
	first := copy(out, r.buf[r.start:min(r.start+r.size, c)])
	if first < r.size {
		copy(out[first:], r.buf[:r.size-first])
	}
	return out
}

// replayCleanCut is how far into a wrapped buffer Snapshot looks for a
// line start to begin the replay at.
const replayCleanCut = 8 << 10

// Snapshot returns the bytes to replay to a (re)attaching viewer. When the
// ring has wrapped, the oldest bytes are a fragment of whatever was being
// printed, so the replay starts at the next line (if one begins within the
// first 8 KiB) and never in the middle of a UTF-8 sequence.
func (r *Ring) Snapshot() []byte {
	b := r.Bytes()
	if !r.Wrapped() || len(b) == 0 {
		return b
	}
	if i := bytes.IndexByte(b[:min(len(b), replayCleanCut)], '\n'); i >= 0 {
		return b[i+1:]
	}
	i := 0
	for i < len(b) && i < utf8.UTFMax && !utf8.RuneStart(b[i]) {
		i++
	}
	return b[i:]
}
