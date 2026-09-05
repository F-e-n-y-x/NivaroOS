// snapshots.go provides snapshot management (create, list, revert, delete)
// for virtual machines via libvirt Domain Snapshot APIs.
package main

import (
	"encoding/xml"
	"fmt"
	"sort"
	"time"

	libvirt "libvirt.org/go/libvirt"
)

type VMSnapshot struct {
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	State        string `json:"state"`
	CreationTime int64  `json:"creation_time"`
	Parent       string `json:"parent,omitempty"`
	Current      bool   `json:"current"`
}

type CreateSnapshotRequest struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

type snapshotXML struct {
	XMLName      xml.Name `xml:"domainsnapshot"`
	Name         string   `xml:"name,omitempty"`
	Description  string   `xml:"description,omitempty"`
	State        string   `xml:"state,omitempty"`
	CreationTime int64    `xml:"creationTime,omitempty"`
	Parent       *struct {
		Name string `xml:"name"`
	} `xml:"parent,omitempty"`
}

func (s *LibvirtStore) ListSnapshots(domName string) ([]VMSnapshot, error) {
	conn, err := s.getConn()
	if err != nil {
		return nil, err
	}
	dom, err := conn.LookupDomainByName(domName)
	if err != nil {
		return nil, err
	}
	defer dom.Free()

	snaps, err := dom.ListAllSnapshots(0)
	if err != nil {
		return nil, err
	}

	currentSnapName := ""
	if hasCur, err := dom.HasCurrentSnapshot(0); err == nil && hasCur {
		if cur, err := dom.SnapshotCurrent(0); err == nil && cur != nil {
			if n, err := cur.GetName(); err == nil {
				currentSnapName = n
			}
			cur.Free()
		}
	}

	result := make([]VMSnapshot, 0, len(snaps))
	for _, snap := range snaps {
		sCopy := snap
		name, err := sCopy.GetName()
		if err != nil {
			sCopy.Free()
			continue
		}

		xmlDesc, err := sCopy.GetXMLDesc(0)
		if err != nil {
			sCopy.Free()
			continue
		}

		var sx snapshotXML
		if err := xml.Unmarshal([]byte(xmlDesc), &sx); err != nil {
			sCopy.Free()
			continue
		}

		parentName := ""
		if sx.Parent != nil {
			parentName = sx.Parent.Name
		}

		state := sx.State
		if state == "" {
			state = "shutoff"
		}

		creationTime := sx.CreationTime
		if creationTime == 0 {
			creationTime = time.Now().Unix()
		}

		result = append(result, VMSnapshot{
			Name:         name,
			Description:  sx.Description,
			State:        state,
			CreationTime: creationTime,
			Parent:       parentName,
			Current:      name == currentSnapName,
		})
		sCopy.Free()
	}

	// Sort chronologically (newest first)
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreationTime > result[j].CreationTime
	})

	return result, nil
}

func (s *LibvirtStore) GetSnapshot(domName string, snapName string) (*VMSnapshot, error) {
	conn, err := s.getConn()
	if err != nil {
		return nil, err
	}
	dom, err := conn.LookupDomainByName(domName)
	if err != nil {
		return nil, err
	}
	defer dom.Free()

	snap, err := dom.SnapshotLookupByName(snapName, 0)
	if err != nil {
		return nil, err
	}
	defer snap.Free()

	xmlDesc, err := snap.GetXMLDesc(0)
	if err != nil {
		return nil, err
	}

	var sx snapshotXML
	if err := xml.Unmarshal([]byte(xmlDesc), &sx); err != nil {
		return nil, err
	}

	currentSnapName := ""
	if hasCur, err := dom.HasCurrentSnapshot(0); err == nil && hasCur {
		if cur, err := dom.SnapshotCurrent(0); err == nil && cur != nil {
			if n, err := cur.GetName(); err == nil {
				currentSnapName = n
			}
			cur.Free()
		}
	}

	parentName := ""
	if sx.Parent != nil {
		parentName = sx.Parent.Name
	}

	state := sx.State
	if state == "" {
		state = "shutoff"
	}

	creationTime := sx.CreationTime
	if creationTime == 0 {
		creationTime = time.Now().Unix()
	}

	return &VMSnapshot{
		Name:         snapName,
		Description:  sx.Description,
		State:        state,
		CreationTime: creationTime,
		Parent:       parentName,
		Current:      snapName == currentSnapName,
	}, nil
}

func (s *LibvirtStore) CreateSnapshot(domName string, req CreateSnapshotRequest) (*VMSnapshot, error) {
	conn, err := s.getConn()
	if err != nil {
		return nil, err
	}
	dom, err := conn.LookupDomainByName(domName)
	if err != nil {
		return nil, err
	}
	defer dom.Free()

	if req.Name == "" {
		req.Name = fmt.Sprintf("snap-%s", time.Now().Format("20060102-150405"))
	}

	sx := snapshotXML{
		Name:        req.Name,
		Description: req.Description,
	}
	xmlBytes, err := xml.Marshal(sx)
	if err != nil {
		return nil, err
	}

	// Try atomic snapshot first, falling back to 0 if unsupported
	snap, err := dom.CreateSnapshotXML(string(xmlBytes), libvirt.DOMAIN_SNAPSHOT_CREATE_ATOMIC)
	if err != nil {
		snap, err = dom.CreateSnapshotXML(string(xmlBytes), 0)
		if err != nil {
			return nil, fmt.Errorf("failed to create snapshot: %w", err)
		}
	}
	defer snap.Free()

	return s.GetSnapshot(domName, req.Name)
}

func (s *LibvirtStore) RevertSnapshot(domName string, snapName string) error {
	conn, err := s.getConn()
	if err != nil {
		return err
	}
	dom, err := conn.LookupDomainByName(domName)
	if err != nil {
		return err
	}
	defer dom.Free()

	snap, err := dom.SnapshotLookupByName(snapName, 0)
	if err != nil {
		return err
	}
	defer snap.Free()

	if err := snap.RevertToSnapshot(0); err != nil {
		return fmt.Errorf("failed to revert to snapshot %s: %w", snapName, err)
	}
	return nil
}

func (s *LibvirtStore) DeleteSnapshot(domName string, snapName string, children bool) error {
	conn, err := s.getConn()
	if err != nil {
		return err
	}
	dom, err := conn.LookupDomainByName(domName)
	if err != nil {
		return err
	}
	defer dom.Free()

	snap, err := dom.SnapshotLookupByName(snapName, 0)
	if err != nil {
		return err
	}
	defer snap.Free()

	var flags libvirt.DomainSnapshotDeleteFlags
	if children {
		flags = libvirt.DOMAIN_SNAPSHOT_DELETE_CHILDREN
	}

	if err := snap.Delete(flags); err != nil {
		return fmt.Errorf("failed to delete snapshot %s: %w", snapName, err)
	}
	return nil
}
