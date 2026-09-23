// sharedfolder.go implements the VM shared folder: one host directory,
// /DATA/VMs/share, exported straight into every VM via VirtIO-FS under
// the guest mount tag "share".
//
// This used to be a per-VM export directory (/DATA/VM-Shares/<vm>) with
// arbitrary host folders bind-mounted into it. That's gone: shared folders
// are restricted to the one default share (a guest has live read-write
// access to whatever it's given, and handing VMs arbitrary host folders
// has already cost real data once), and with only one folder there is
// nothing to bind-mount - virtiofsd serves /DATA/VMs/share directly. No
// mount/umount ever runs on a VM's behalf any more, and nothing needs
// re-creating after a host reboot.
package main

import (
	"encoding/xml"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	libvirt "libvirt.org/go/libvirt"
)

// defaultAutoShareDir, shareAllowedRoot and legacyVMSharesBaseDir are
// vars, not consts, so tests can point them at temp dirs instead of the
// real /DATA.
var (
	defaultAutoShareDir = "/DATA/VMs/share"
	// shareAllowedRoot is this project's convention for user-managed
	// storage (see FstabPanel/cloud storage, which scope themselves the
	// same way) - nothing outside it can be shared with a VM.
	shareAllowedRoot = "/DATA"
	// legacyVMSharesBaseDir is the old per-VM bind-mount export area. VMs
	// defined back then may still have a virtiofs device pointing into it;
	// those are left alone (never unmounted or deleted automatically) -
	// only DeleteVM tidies up the deleted VM's own leftover directory.
	legacyVMSharesBaseDir = "/DATA/VM-Shares"
	// virtiofsdPath is Debian's virtiofsd location (package virtiofsd).
	virtiofsdPath = "/usr/libexec/virtiofsd"
)

// runUmount is the only place this file shells out to umount(8) - a var
// so tests can stub it.
var runUmount = func(args ...string) error { return exec.Command("umount", args...).Run() }

// defaultShareTag is the guest-side virtiofs mount tag of the default share.
const defaultShareTag = "share"

func EnsureAutoShareDir() string {
	_ = os.MkdirAll(defaultAutoShareDir, 0777)
	_ = os.Chmod(defaultAutoShareDir, 0777)
	return defaultAutoShareDir
}

type SharedFolderSpec struct {
	SourceDir string `json:"source_dir"`
	TargetTag string `json:"target_tag"` // guest mount tag - always "share" now
	ReadOnly  bool   `json:"read_only,omitempty"`
}

// defaultShares is the single share every VM gets.
func defaultShares() []SharedFolderSpec {
	return []SharedFolderSpec{{SourceDir: EnsureAutoShareDir(), TargetTag: defaultShareTag}}
}

// errOnlyDefaultShare is returned for any shared folder other than the
// default one.
var errOnlyDefaultShare = badRequestf("Only the default shared folder %s is allowed", "/DATA/VMs/share")

// isDefaultShare reports whether sf is the default share (source
// defaultAutoShareDir, tag "share"; read-only either way) - the only
// shared folder allowed (see the file comment).
func isDefaultShare(sf SharedFolderSpec) bool {
	return filepath.Clean(sf.SourceDir) == filepath.Clean(defaultAutoShareDir) && sf.TargetTag == defaultShareTag
}

// normalizeRequestedShares turns a create/update request's shared_folders
// into the list actually applied: always exactly the default share.
// Omitted (nil) or empty keeps the read-only flag from current (the VM's
// existing shares, if any); any entry that isn't the default share is
// rejected with errOnlyDefaultShare.
func normalizeRequestedShares(requested, current []SharedFolderSpec) ([]SharedFolderSpec, error) {
	shares := defaultShares()
	if len(requested) == 0 {
		for _, sf := range current {
			if isDefaultShare(sf) {
				shares[0].ReadOnly = sf.ReadOnly
			}
		}
		return shares, nil
	}
	for _, sf := range requested {
		if !isDefaultShare(sf) {
			return nil, errOnlyDefaultShare
		}
		shares[0].ReadOnly = sf.ReadOnly
	}
	return shares, nil
}

// deniedShareRoots are paths under the otherwise-allowed /DATA tree that
// must never be handed to a VM even though they're legitimate host
// directories - sharing defaultStorageDir would give a VM's guest OS live,
// direct read/write access to every VM's virtual disk images (including
// its own, while running). A func, not a var, so it always reflects the
// current (test-overridable) directory settings.
func deniedShareRoots() []string {
	return []string{legacyVMSharesBaseDir, defaultStorageDir}
}

// isStrictlyWithin reports whether path is a descendant of root (not root
// itself). Both must already be clean, absolute paths.
func isStrictlyWithin(path, root string) bool {
	if root == string(filepath.Separator) {
		return path != root && strings.HasPrefix(path, root)
	}
	return strings.HasPrefix(path, root+string(filepath.Separator))
}

// validateShareSource resolves sourceDir to its real, symlink-free absolute
// path, confirms it's an existing directory, and applies checkShareSource
// - defense in depth on top of the default-only rule, e.g. against
// /DATA/VMs/share itself having been replaced by a symlink to /.
func validateShareSource(sourceDir string) (string, error) {
	abs, err := filepath.Abs(sourceDir)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("source folder %q does not exist: %w", sourceDir, err)
	}
	fi, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("source folder %q does not exist: %w", sourceDir, err)
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("%q is not a directory", sourceDir)
	}
	if err := checkShareSource(resolved, shareAllowedRoot, deniedShareRoots(), defaultAutoShareDir); err != nil {
		return "", fmt.Errorf("%q can't be shared: %w", sourceDir, err)
	}
	return resolved, nil
}

// checkShareSource is validateShareSource's pure policy half: resolved
// must be strictly inside allowedRoot, and must be neither a denied root,
// inside one, nor an ANCESTOR of one - sharing /DATA (or /) would expose
// every denied root beneath it just the same. exempt (the default share,
// which lives inside defaultStorageDir) is always allowed.
func checkShareSource(resolved, allowedRoot string, denied []string, exempt string) error {
	if !isStrictlyWithin(resolved, allowedRoot) {
		return fmt.Errorf("only folders inside %s (not %s itself) can be shared with a VM", allowedRoot, allowedRoot)
	}
	if resolved == exempt {
		return nil
	}
	for _, d := range denied {
		if resolved == d || isStrictlyWithin(resolved, d) || isStrictlyWithin(d, resolved) {
			return errors.New("it contains or is part of storage used internally by the VM system")
		}
	}
	return nil
}

// defaultShareSource creates the default share directory if needed and
// confirms it's a real directory at exactly that path (not a symlink
// pointing anywhere else) before any VM is given it.
func defaultShareSource() (string, error) {
	dir := filepath.Clean(EnsureAutoShareDir())
	resolved, err := validateShareSource(dir)
	if err != nil {
		return "", err
	}
	if resolved != dir {
		return "", fmt.Errorf("default shared folder %s resolves to %s - refusing to share it", dir, resolved)
	}
	return dir, nil
}

// shareDeviceXMLTemplate is the default share's virtiofs device - the
// "share" sub-template of the domain template, and used alone to patch an
// existing domain's config. <binary> pins Debian's virtiofsd location
// rather than relying on libvirt's vhost-user descriptor lookup.
const shareDeviceXMLTemplate = `<filesystem type='mount' accessmode='passthrough'>
      <driver type='virtiofs'/>
      <binary path='{{x .Binary}}'/>
      <source dir='{{x .Dir}}'/>
      <target dir='{{x .Tag}}'/>{{if .ReadOnly}}
      <readonly/>{{end}}
    </filesystem>`

var shareDeviceTemplate = template.Must(template.New("share").Funcs(xmlFuncs).Parse(shareDeviceXMLTemplate))

type shareDeviceData struct {
	Binary, Dir, Tag string
	ReadOnly         bool
}

func newShareDeviceData(readOnly bool) shareDeviceData {
	return shareDeviceData{Binary: virtiofsdPath, Dir: filepath.Clean(defaultAutoShareDir), Tag: defaultShareTag, ReadOnly: readOnly}
}

func renderShareDeviceXML(readOnly bool) (string, error) {
	var buf strings.Builder
	if err := shareDeviceTemplate.Execute(&buf, newShareDeviceData(readOnly)); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// filesystemXML mirrors one <filesystem> element (the same shape as
// domainXML's Filesystems entries), for inspecting a single raw element.
type filesystemXML struct {
	Driver struct {
		Type string `xml:"type,attr"`
	} `xml:"driver"`
	Source struct {
		Dir string `xml:"dir,attr"`
	} `xml:"source"`
	Target struct {
		Dir string `xml:"dir,attr"`
	} `xml:"target"`
	ReadOnly *struct{} `xml:"readonly"`
}

var filesystemElemRe = regexp.MustCompile(`(?s)<filesystem\b.*?</filesystem>`)

// sharesFromXML lists a domain's virtiofs shares as SharedFolderSpecs.
func sharesFromXML(parsed domainXML) []SharedFolderSpec {
	var shares []SharedFolderSpec
	for _, fs := range parsed.Devices.Filesystems {
		if fs.Driver.Type == "virtiofs" && fs.Source.Dir != "" && fs.Target.Dir != "" {
			shares = append(shares, SharedFolderSpec{SourceDir: fs.Source.Dir, TargetTag: fs.Target.Dir, ReadOnly: fs.ReadOnly != nil})
		}
	}
	return shares
}

// setDefaultShareInXML returns domainXMLDesc with the default share device
// present exactly once, read-only per readOnly (onlyIfMissing: leave an
// existing one as it is), plus the shared memfd memory backing virtiofs
// needs. Other <filesystem> devices (a legacy per-VM export, say) are left
// untouched. Pure string surgery on libvirt's own XML rather than an
// unmarshal/marshal round trip, which would drop every element domainXML
// doesn't model.
func setDefaultShareInXML(domainXMLDesc string, readOnly, onlyIfMissing bool) (string, bool, error) {
	out := domainXMLDesc
	changed := false
	found := false
	var matchErr error
	out = filesystemElemRe.ReplaceAllStringFunc(out, func(elem string) string {
		var fs filesystemXML
		if err := xml.Unmarshal([]byte(elem), &fs); err != nil {
			matchErr = err
			return elem
		}
		if fs.Driver.Type != "virtiofs" || fs.Target.Dir != defaultShareTag {
			return elem
		}
		if found {
			changed = true
			return "" // a duplicate default share device
		}
		found = true
		if onlyIfMissing || ((fs.ReadOnly != nil) == readOnly && filepath.Clean(fs.Source.Dir) == filepath.Clean(defaultAutoShareDir)) {
			return elem
		}
		changed = true
		replacement, err := renderShareDeviceXML(readOnly)
		if err != nil {
			matchErr = err
			return elem
		}
		return replacement
	})
	if matchErr != nil {
		return "", false, matchErr
	}
	if !found {
		device, err := renderShareDeviceXML(readOnly)
		if err != nil {
			return "", false, err
		}
		idx := strings.LastIndex(out, "</devices>")
		if idx < 0 {
			return "", false, errors.New("domain XML has no <devices> element")
		}
		out = out[:idx] + "  " + device + "\n  " + out[idx:]
		changed = true
	}

	withMemory, memChanged, err := ensureSharedMemoryBacking(out)
	if err != nil {
		return "", false, err
	}
	return withMemory, changed || memChanged, nil
}

var memoryBackingRe = regexp.MustCompile(`(?s)<memoryBacking\s*/>|<memoryBacking\s*>(.*?)</memoryBacking>`)

// ensureSharedMemoryBacking adds <access mode='shared'/> (and a memfd
// source, if there's no source at all) to a domain's memory backing -
// vhost-user devices like virtiofs refuse to start without shared guest
// memory.
func ensureSharedMemoryBacking(domainXMLDesc string) (string, bool, error) {
	const shared = "<source type='memfd'/>\n    <access mode='shared'/>"
	loc := memoryBackingRe.FindStringSubmatchIndex(domainXMLDesc)
	if loc == nil {
		for _, anchor := range []string{"</vcpu>", "</currentMemory>", "</memory>"} {
			if idx := strings.Index(domainXMLDesc, anchor); idx >= 0 {
				idx += len(anchor)
				return domainXMLDesc[:idx] + "\n  <memoryBacking>\n    " + shared + "\n  </memoryBacking>" + domainXMLDesc[idx:], true, nil
			}
		}
		return "", false, errors.New("domain XML has no <memory>/<vcpu> element to anchor <memoryBacking> to")
	}
	inner := ""
	if loc[2] >= 0 {
		inner = domainXMLDesc[loc[2]:loc[3]]
	}
	if regexp.MustCompile(`<access\s+mode=['"]shared['"]`).MatchString(inner) {
		return domainXMLDesc, false, nil
	}
	if regexp.MustCompile(`<access\b`).MatchString(inner) {
		return "", false, errors.New("the VM's memory backing is explicitly private - shared folders need shared memory; change it or remove the <access> element")
	}
	add := "<access mode='shared'/>"
	if !strings.Contains(inner, "<source") {
		add = shared
	}
	replacement := "<memoryBacking>\n    " + add + inner + "</memoryBacking>"
	return domainXMLDesc[:loc[0]] + replacement + domainXMLDesc[loc[1]:], true, nil
}

// applyDefaultShare writes setDefaultShareInXML's result into dom's
// persistent config (never the live VM - a change reaches the guest at its
// next start), reporting whether anything changed.
func applyDefaultShare(conn *libvirt.Connect, dom *libvirt.Domain, readOnly, onlyIfMissing bool) (bool, error) {
	if _, err := defaultShareSource(); err != nil {
		return false, err
	}
	inactiveXML, err := dom.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err != nil {
		return false, err
	}
	newXML, changed, err := setDefaultShareInXML(inactiveXML, readOnly, onlyIfMissing)
	if err != nil || !changed {
		return false, err
	}
	newDom, err := conn.DomainDefineXML(newXML)
	if err != nil {
		return false, fmt.Errorf("add shared folder to VM config: %w", err)
	}
	newDom.Free()
	return true, nil
}

// ensureDefaultShareDevice gives a VM whose persistent config lacks the
// default share (every VM created before it was added automatically) the
// device, config-only - self-healing on the VM's next start.
func ensureDefaultShareDevice(conn *libvirt.Connect, dom *libvirt.Domain) error {
	_, err := applyDefaultShare(conn, dom, false, true)
	return err
}

// ensureLegacyExportDirs makes sure any legacy per-VM export directory a
// domain's virtiofs devices still point at exists - virtiofsd refuses to
// start (and so does the whole VM) if its source directory is missing. It
// only ever creates an empty directory; nothing is mounted or removed.
func ensureLegacyExportDirs(domainXMLDesc string) {
	var parsed domainXML
	if err := xml.Unmarshal([]byte(domainXMLDesc), &parsed); err != nil {
		return
	}
	for _, sf := range sharesFromXML(parsed) {
		if isStrictlyWithin(filepath.Clean(sf.SourceDir), filepath.Clean(legacyVMSharesBaseDir)) {
			if err := os.MkdirAll(sf.SourceDir, 0755); err != nil {
				log.Printf("shared folder: create legacy export dir %s: %v", sf.SourceDir, err)
			}
		}
	}
}

func isMounted(path string) bool {
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return false
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == path {
			return true
		}
	}
	return false
}

// removeLegacyVMShareDir tidies up a DELETED VM's leftover legacy export
// directory: unmounts the bind mounts inside it (unmounting never touches
// the shared folders' own contents), then removes the mountpoints, its
// .shares.json and the directory - os.Remove only, which refuses anything
// non-empty. NEVER os.RemoveAll: through a live bind mount that would
// delete the shared folder's real data.
func removeLegacyVMShareDir(name string) {
	if !vmNameRe.MatchString(name) {
		return
	}
	vmDir := filepath.Join(legacyVMSharesBaseDir, name)
	if filepath.Dir(vmDir) != filepath.Clean(legacyVMSharesBaseDir) {
		return
	}
	entries, err := os.ReadDir(vmDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		mountpoint := filepath.Join(vmDir, entry.Name())
		if isMounted(mountpoint) {
			if err := runUmount("-l", mountpoint); err != nil {
				log.Printf("shared folder: umount %s: %v", mountpoint, err)
				continue
			}
		}
		if !isMounted(mountpoint) {
			_ = os.Remove(mountpoint)
		}
	}
	_ = os.Remove(filepath.Join(vmDir, ".shares.json"))
	if err := os.Remove(vmDir); err != nil {
		log.Printf("shared folder: remove %s: %v", vmDir, err)
	}
}

// ListSharedFolders returns name's shared folders as its config has them
// - or the default share it will get at its next start, if it has none
// yet.
func (s *LibvirtStore) ListSharedFolders(name string) ([]SharedFolderSpec, error) {
	vm, err := s.GetVM(name)
	if err != nil {
		return nil, err
	}
	if len(vm.SharedFolders) > 0 {
		return vm.SharedFolders, nil
	}
	return defaultShares(), nil
}

// AttachSharedFolder (re)applies the default share with spec's read-only
// flag - the only folder allowed. Config-only: a running VM isn't
// hot-plugged, and the returned warning says the change applies at its
// next start.
func (s *LibvirtStore) AttachSharedFolder(name string, spec SharedFolderSpec) (string, error) {
	conn, err := s.getConn()
	if err != nil {
		return "", err
	}
	dom, err := s.lookup(name)
	if err != nil {
		return "", err
	}
	defer dom.Free()

	if spec.SourceDir == "" {
		return "", badRequestf("source_dir is required")
	}
	if spec.TargetTag == "" {
		spec.TargetTag = defaultShareTag
	}
	if !isDefaultShare(spec) {
		return "", errOnlyDefaultShare
	}
	changed, err := applyDefaultShare(conn, dom, spec.ReadOnly, false)
	if err != nil {
		return "", err
	}
	if active, _ := dom.IsActive(); active && changed {
		return "The shared folder change applies the next time the VM starts.", nil
	}
	return "", nil
}

// DetachSharedFolder removes a (legacy) non-default virtiofs device, by
// its guest tag, from name's persistent config. The default share can't
// be removed.
func (s *LibvirtStore) DetachSharedFolder(name string, targetTag string) error {
	conn, err := s.getConn()
	if err != nil {
		return err
	}
	dom, err := s.lookup(name)
	if err != nil {
		return err
	}
	defer dom.Free()

	if targetTag == defaultShareTag {
		return badRequestf("The default shared folder can't be removed")
	}
	inactiveXML, err := dom.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err != nil {
		return err
	}
	removed := false
	newXML := filesystemElemRe.ReplaceAllStringFunc(inactiveXML, func(elem string) string {
		var fs filesystemXML
		if xml.Unmarshal([]byte(elem), &fs) == nil && fs.Driver.Type == "virtiofs" && fs.Target.Dir == targetTag {
			removed = true
			return ""
		}
		return elem
	})
	if !removed {
		return badRequestf("no shared folder %q on %q", targetTag, name)
	}
	newDom, err := conn.DomainDefineXML(newXML)
	if err != nil {
		return fmt.Errorf("remove shared folder from VM config: %w", err)
	}
	newDom.Free()
	return nil
}
