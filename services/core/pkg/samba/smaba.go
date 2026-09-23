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
	"errors"
	"net"
	"time"

	"github.com/hirochachacha/go-smb2"
)

func ConnectSambaService(host, port, username, password, directory string) error {
	// Bounded: an offline host used to hang the request past the UI's limit.
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 10*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	d := &smb2.Dialer{
		Initiator: &smb2.NTLMInitiator{
			User:     username,
			Password: password,
		},
	}

	s, err := d.Dial(conn)
	if err != nil {
		return err
	}
	defer s.Logoff()
	names, err := s.ListSharenames()
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
func GetSambaSharesList(host, port, username, password string) ([]string, error) {
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

	s, err := d.Dial(conn)
	if err != nil {
		return nil, err
	}
	defer s.Logoff()
	names, err := s.ListSharenames()
	if err != nil {
		return nil, err
	}
	return names, err
}
