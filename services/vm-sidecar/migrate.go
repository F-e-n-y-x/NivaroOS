package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"libvirt.org/go/libvirt"
)

// Brings VMs made by older versions onto the current /DATA/VMs layout,
// automatically, on every install that updates - not just the one box a
// migration was once done on by hand:
//
//	/DATA/VMs/ISOs/          ISO images (older installers used "isos")
//	/DATA/VMs/share/         the one shared folder
//	/DATA/VMs/<vm>/          the VM's disks and its UEFI variables
//
// Per VM it moves disks and NVRAM that sit loose in /DATA/VMs into the
// VM's folder, converts raw UEFI variables to qcow2 (internal snapshots
// need that; the raw file is kept next to it as .bak), and repoints CD
// drives from the old isos/ folder. Only stopped VMs without snapshots
// are touched (snapshot metadata records the old paths); others are
// retried on the next start or restart. A failed redefine puts every
// file back.

// layoutOp is one file operation of a VM's migration.
type layoutOp struct {
	kind string // "move" or "convert-nvram"
	from string
	to   string
}

var (
	diskBlockRe  = regexp.MustCompile(`(?s)<disk\b[^>]*>.*?</disk>`)
	sourceFileRe = regexp.MustCompile(`<source file='([^']+)'`)
	nvramRe      = regexp.MustCompile(`<nvram\b([^>]*)>([^<]+)</nvram>`)
	formatAttrRe = regexp.MustCompile(`\sformat='([^']*)'`)
)

// planLayoutMigration works out, from a stopped VM's XML, which files move
// where and the XML that points at the new places. It doesn't touch the
// filesystem except through exists.
func planLayoutMigration(name, xmlDesc, storageDir, isoDir string, exists func(string) bool) (string, []layoutOp) {
	storageDir = filepath.Clean(storageDir)
	legacyISODir := filepath.Join(storageDir, "isos")
	vmDir := filepath.Join(storageDir, name)
	var ops []layoutOp
	moved := map[string]string{}

	out := diskBlockRe.ReplaceAllStringFunc(xmlDesc, func(block string) string {
		m := sourceFileRe.FindStringSubmatch(block)
		if m == nil {
			return block
		}
		src := m[1]
		var dst string
		switch {
		case strings.Contains(block, "device='disk'") && filepath.Dir(src) == storageDir:
			// A loose disk image of this VM.
			dst = filepath.Join(vmDir, filepath.Base(src))
			if exists(dst) || !exists(src) {
				return block
			}
			if _, done := moved[src]; !done {
				ops = append(ops, layoutOp{kind: "move", from: src, to: dst})
				moved[src] = dst
			}
		case strings.Contains(block, "device='cdrom'") && filepath.Dir(src) == legacyISODir:
			dst = filepath.Join(isoDir, filepath.Base(src))
			if !exists(dst) {
				if !exists(src) {
					return block
				}
				if _, done := moved[src]; !done {
					ops = append(ops, layoutOp{kind: "move", from: src, to: dst})
					moved[src] = dst
				}
			}
		default:
			return block
		}
		return strings.Replace(block, "<source file='"+src+"'", "<source file='"+dst+"'", 1)
	})

	out = nvramRe.ReplaceAllStringFunc(out, func(tag string) string {
		m := nvramRe.FindStringSubmatch(tag)
		attrs, path := m[1], strings.TrimSpace(m[2])
		format := ""
		if f := formatAttrRe.FindStringSubmatch(attrs); f != nil {
			format = f[1]
		}
		inRoot := filepath.Dir(path) == storageDir
		if format == "qcow2" {
			if !inRoot || !exists(path) {
				return tag
			}
			dst := filepath.Join(vmDir, filepath.Base(path))
			if exists(dst) {
				return tag
			}
			ops = append(ops, layoutOp{kind: "move", from: path, to: dst})
			return strings.Replace(tag, ">"+m[2]+"<", ">"+dst+"<", 1)
		}
		// raw (or unspecified, which libvirt treats as raw)
		if !exists(path) {
			return tag
		}
		dst := filepath.Join(vmDir, name+"_VARS.qcow2")
		if exists(dst) {
			return tag
		}
		ops = append(ops, layoutOp{kind: "convert-nvram", from: path, to: dst})
		newAttrs := attrs
		if format == "" {
			newAttrs += " format='qcow2'"
		} else {
			newAttrs = formatAttrRe.ReplaceAllStringFunc(attrs, func(a string) string {
				// templateFormat='raw' must stay; only the plain format attr.
				return " format='qcow2'"
			})
		}
		return "<nvram" + newAttrs + ">" + dst + "</nvram>"
	})
	return out, ops
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// runLayoutOps performs ops, returning an undo that puts everything back.
func runLayoutOps(ops []layoutOp) (func(), error) {
	var undo []func()
	rollback := func() {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
	}
	for _, op := range ops {
		if err := os.MkdirAll(filepath.Dir(op.to), 0755); err != nil {
			rollback()
			return nil, err
		}
		switch op.kind {
		case "move":
			// Same filesystem: an instant rename. Anything else (a disk on
			// another drive) is left where it is rather than copied.
			if err := os.Rename(op.from, op.to); err != nil {
				rollback()
				return nil, fmt.Errorf("move %s: %w", op.from, err)
			}
			from, to := op.from, op.to
			undo = append(undo, func() { _ = os.Rename(to, from) })
		case "convert-nvram":
			if out, err := exec.Command("qemu-img", "convert", "-f", "raw", "-O", "qcow2", op.from, op.to).CombinedOutput(); err != nil {
				_ = os.Remove(op.to)
				rollback()
				return nil, fmt.Errorf("convert %s to qcow2: %v: %s", op.from, err, strings.TrimSpace(string(out)))
			}
			bak := filepath.Join(filepath.Dir(op.to), filepath.Base(op.from)+".bak")
			if err := os.Rename(op.from, bak); err != nil {
				_ = os.Remove(op.to)
				rollback()
				return nil, err
			}
			from, to := op.from, op.to
			undo = append(undo, func() {
				_ = os.Rename(bak, from)
				_ = os.Remove(to)
			})
		}
	}
	return rollback, nil
}

// migrateVMLayout migrates one VM if it's stopped and has no snapshots.
// It reports whether anything changed.
func (s *LibvirtStore) migrateVMLayout(conn *libvirt.Connect, dom *libvirt.Domain) (bool, error) {
	name, err := dom.GetName()
	if err != nil {
		return false, err
	}
	if active, err := dom.IsActive(); err != nil || active {
		return false, nil
	}
	if n, err := dom.SnapshotNum(0); err == nil && n > 0 {
		return false, nil
	}
	xmlDesc, err := dom.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err != nil {
		return false, err
	}
	newXML, ops := planLayoutMigration(name, xmlDesc, defaultStorageDir, defaultISODir, fileExists)
	if len(ops) == 0 && newXML == xmlDesc {
		return false, nil
	}
	rollback, err := runLayoutOps(ops)
	if err != nil {
		return false, err
	}
	nd, err := conn.DomainDefineXML(newXML)
	if err != nil {
		rollback()
		return false, fmt.Errorf("redefine %s: %w", name, err)
	}
	nd.Free()
	log.Printf("vm %s: moved to the %s/%s layout (%d file operations)", name, defaultStorageDir, name, len(ops))
	return true, nil
}

// MigrateLegacyLayout runs at startup: every stopped VM, then the old
// shared folders (loose ISOs from isos/ into ISOs/, empty legacy dirs
// removed). Best-effort: problems are logged, never fatal.
func (s *LibvirtStore) MigrateLegacyLayout() {
	conn, err := s.getConn()
	if err != nil {
		return
	}
	doms, err := conn.ListAllDomains(0)
	if err == nil {
		for i := range doms {
			if _, err := s.migrateVMLayout(conn, &doms[i]); err != nil {
				log.Printf("vm layout migration: %v", err)
			}
			doms[i].Free()
		}
	}
	migrateLegacyDirs(defaultStorageDir, defaultISODir, legacyVMSharesBaseDir)
}

// migrateLegacyDirs moves ISOs left in the old lowercase isos/ folder into
// ISOs/ (never overwriting) and removes legacy folders that are empty.
func migrateLegacyDirs(storageDir, isoDir, legacySharesDir string) {
	legacy := filepath.Join(storageDir, "isos")
	if entries, err := os.ReadDir(legacy); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			src := filepath.Join(legacy, e.Name())
			dst := filepath.Join(isoDir, e.Name())
			if fileExists(dst) {
				continue
			}
			if err := os.MkdirAll(isoDir, 0755); err == nil {
				if err := os.Rename(src, dst); err != nil && !errors.Is(err, os.ErrExist) {
					log.Printf("move %s to %s: %v", src, dst, err)
				}
			}
		}
	}
	// os.Remove only removes empty directories - anything a user put there
	// stays.
	for _, d := range []string{legacy, filepath.Join(storageDir, "Disks"), filepath.Join(storageDir, "Images"), legacySharesDir} {
		_ = os.Remove(d)
	}
}
