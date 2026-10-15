package samba

import (
	"encoding/binary"
	"io"
	"net"
	"testing"
)

var le = binary.LittleEndian

// fakeSMB serves connections, answering each request frame with reply(req).
// A nil reply closes the connection.
func fakeSMB(t *testing.T, reply func(req []byte) []byte) (string, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go serve(c, reply)
		}
	}()
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	return host, port
}

func serve(c net.Conn, reply func(req []byte) []byte) {
	defer c.Close()
	for {
		var h [4]byte
		if _, err := io.ReadFull(c, h[:]); err != nil {
			return
		}
		req := make([]byte, binary.BigEndian.Uint32(h[:]))
		if _, err := io.ReadFull(c, req); err != nil {
			return
		}
		res := reply(req)
		if res == nil {
			return
		}
		binary.BigEndian.PutUint32(h[:], uint32(len(res)))
		c.Write(append(h[:], res...))
	}
}

// smbReply builds a response header for req followed by body.
func smbReply(req []byte, cmd uint16, status uint32, body []byte) []byte {
	h := make([]byte, 64)
	copy(h, "\xfeSMB")
	le.PutUint16(h[4:], 64)
	le.PutUint32(h[8:], status)
	le.PutUint16(h[12:], cmd)
	le.PutUint16(h[14:], 64) // credits
	le.PutUint32(h[16:], 1)  // server to client
	copy(h[24:32], req[24:32])
	return append(h, body...)
}

func negotiateOK(req []byte) []byte {
	body := make([]byte, 72)
	le.PutUint16(body[0:], 65)
	le.PutUint16(body[4:], 0x0210) // SMB 2.1
	le.PutUint32(body[28:], 1<<16)
	le.PutUint32(body[32:], 1<<16)
	le.PutUint32(body[36:], 1<<16)
	return smbReply(req, 0, 0, body)
}

// A frame shorter than an SMB2 header used to panic go-smb2's receiver
// goroutine, which no recover can catch: it killed the whole core process.
func TestShortFrameFromServerIsAnError(t *testing.T) {
	host, port := fakeSMB(t, func([]byte) []byte { return []byte{0} })
	if _, err := GetSambaSharesList(host, port, "u", "p"); err == nil {
		t.Fatal("want an error")
	}
	if err := ConnectSambaService(host, port, "u", "p", "x"); err == nil {
		t.Fatal("want an error")
	}
}

// A chained command offset past the frame used to panic the receiver too.
func TestBadNextCommandFromServerIsAnError(t *testing.T) {
	host, port := fakeSMB(t, func(req []byte) []byte {
		res := negotiateOK(req)
		le.PutUint32(res[20:], 0xffff)
		return res
	})
	if _, err := GetSambaSharesList(host, port, "u", "p"); err == nil {
		t.Fatal("want an error")
	}
}

// A session-setup reply that names no known auth mech made go-smb2 call a
// nil Initiator (panic in our goroutine). It must come back as an error.
func TestMalformedSessionSetupIsAnError(t *testing.T) {
	// SPNEGO NegTokenResp [1] { SEQUENCE { negState [0] ENUMERATED 1 } }, no supportedMech.
	tok := []byte{0xa1, 0x07, 0x30, 0x05, 0xa0, 0x03, 0x0a, 0x01, 0x01}
	host, port := fakeSMB(t, func(req []byte) []byte {
		switch le.Uint16(req[12:]) {
		case 0:
			return negotiateOK(req)
		case 1:
			body := make([]byte, 8)
			le.PutUint16(body[0:], 9)
			le.PutUint16(body[4:], 72)
			le.PutUint16(body[6:], uint16(len(tok)))
			return smbReply(req, 1, 0xc0000016, append(body, tok...))
		}
		return nil
	})
	_, err := GetSambaSharesList(host, port, "u", "p")
	if err == nil {
		t.Fatal("want an error")
	}
	t.Log(err)
}
