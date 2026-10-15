/*
 * @Author: LinkLeong link@icewhale.org
 * @Date: 2022-07-27 10:35:29
 * @LastEditors: LinkLeong
 * @LastEditTime: 2022-08-01 13:56:44
 * @FilePath: /CasaOS/pkg/samba/smaba.go
 * @Description:
 * Copyright (c) 2022 by icewhale, All Rights Reserved.
 */
package samba

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/hirochachacha/go-smb2"
)

func ConnectSambaService(host, port, username, password, directory string) error {
	names, err := GetSambaSharesList(host, port, username, password)
	if err != nil {
		return err
	}
	for _, name := range names {
		if name == directory {
			return nil
		}
	}
	return errors.New("directory not found")
}

// get share name list
func GetSambaSharesList(host, port, username, password string) (names []string, err error) {
	// go-smb2 is unmaintained and panics on some malformed replies from a
	// hostile server (GO-2026-5051). Panics in this goroutine become errors;
	// checkedConn keeps the ones that would hit its receiver goroutine out.
	defer func() {
		if r := recover(); r != nil {
			names, err = nil, fmt.Errorf("smb: malformed server response: %v", r)
		}
	}()
	// Bounded: an offline host used to hang the request past the UI's limit.
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 10*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	d := &smb2.Dialer{
		Initiator: &smb2.NTLMInitiator{
			User:     username,
			Password: password,
		},
	}

	s, err := d.Dial(&checkedConn{Conn: conn})
	if err != nil {
		return nil, err
	}
	defer s.Logoff()
	return s.ListSharenames()
}

// checkedConn hands go-smb2 only frames its receiver goroutine can parse
// without panicking: a panic there cannot be recovered and kills core.
type checkedConn struct {
	net.Conn
	buf []byte
}

func (c *checkedConn) Read(p []byte) (int, error) {
	if len(c.buf) == 0 {
		var h [4]byte
		if _, err := io.ReadFull(c.Conn, h[:]); err != nil {
			return 0, err
		}
		frame := make([]byte, 4+binary.BigEndian.Uint32(h[:])&0xffffff)
		copy(frame, h[:])
		if _, err := io.ReadFull(c.Conn, frame[4:]); err != nil {
			return 0, err
		}
		if err := checkFrame(frame[4:]); err != nil {
			return 0, err
		}
		c.buf = frame
	}
	n := copy(p, c.buf)
	c.buf = c.buf[n:]
	return n, nil
}

// checkFrame validates the SMB2 header chain. Encrypted (transform) frames
// only get a length check: their plaintext is not visible here.
// ponytail: a server holding the session key could still send a bad
// encrypted payload; fork go-smb2 if core ever talks to untrusted servers more.
func checkFrame(pkt []byte) error {
	bad := errors.New("smb: malformed frame from server")
	if len(pkt) >= 4 && string(pkt[:4]) == "\xfdSMB" {
		if len(pkt) < 52+64 {
			return bad
		}
		return nil
	}
	for {
		if len(pkt) < 64 || string(pkt[:4]) != "\xfeSMB" {
			return bad
		}
		off := binary.LittleEndian.Uint32(pkt[20:24])
		if off == 0 {
			return nil
		}
		if off < 64 || off > uint32(len(pkt)) {
			return bad
		}
		pkt = pkt[off:]
	}
}
