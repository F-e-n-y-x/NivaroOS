package main

import (
	"fmt"
	"log"
	"regexp"
	"strings"

	"libvirt.org/go/libvirt"
)

// Clipboard sharing between the browser console and a VM. noVNC sends the
// browser's clipboard as VNC ClientCutText and listens for ServerCutText;
// QEMU's built-in VNC server only forwards either when the domain has a
// qemu-vdagent chardev, which speaks the SPICE agent protocol over the
// com.redhat.spice.0 virtio-serial port to spice-vdagent inside the guest
// (installed by the guest tools). Without the channel, pastes are dropped
// and guest copies never reach the browser.
//
// <mouse mode='server'/> (QEMU mouse=off) is deliberate: left out, QEMU
// defaults the vdagent's mouse to on, and once the guest's agent connects
// it takes pointer input over from the USB tablet - a pointer behaviour
// change for every VM (and one that only kicks in after guest tools are
// installed), for nothing the tablet doesn't already do.
const clipboardChannelXML = `<channel type='qemu-vdagent'>
      <source>
        <clipboard copypaste='yes'/>
        <mouse mode='server'/>
      </source>
      <target type='virtio' name='com.redhat.spice.0'/>
    </channel>`

// qemu-vdagent chardevs arrived in libvirt 8.4.0 and QEMU 6.1.0; both
// versions are libvirt's major*1000000 + minor*1000 + release encoding.
const (
	minLibvirtForClipboard = 8004000
	minQEMUForClipboard    = 6001000
)

// hostSupportsClipboardChannel reports whether the connected libvirt and
// QEMU both know qemu-vdagent - on older distros the channel is left out
// entirely rather than making every define fail. Any error (or a non-QEMU
// driver, e.g. the test driver) counts as unsupported.
func hostSupportsClipboardChannel(conn *libvirt.Connect) bool {
	if t, err := conn.GetType(); err != nil || t != "QEMU" {
		return false
	}
	lib, err := conn.GetLibVersion()
	if err != nil || lib < minLibvirtForClipboard {
		return false
	}
	hv, err := conn.GetVersion()
	return err == nil && hv >= minQEMUForClipboard
}

var (
	channelBlockRe  = regexp.MustCompile(`(?s)<channel\b[^>]*>.*?</channel>`)
	spiceChannelRe  = regexp.MustCompile(`<target\b[^>]*\bname=['"]com\.redhat\.spice\.0['"]`)
	devicesCloseRe  = regexp.MustCompile(`</devices>`)
	vdagentTypeAttr = regexp.MustCompile(`^<channel\b[^>]*\btype=['"]qemu-vdagent['"]`)
)

// hasClipboardChannel reports whether a domain XML already has a SPICE
// agent channel - ours, or any other on com.redhat.spice.0 (a spicevmc one
// someone added by hand), which a second channel on that name would clash
// with.
func hasClipboardChannel(xmlDesc string) bool {
	for _, block := range channelBlockRe.FindAllString(xmlDesc, -1) {
		if vdagentTypeAttr.MatchString(block) || spiceChannelRe.MatchString(block) {
			return true
		}
	}
	return false
}

// planClipboardChannel returns xmlDesc with the clipboard channel added
// just before </devices>, and whether that changed anything - nothing when
// it already has one (so it's safe to run on every start).
func planClipboardChannel(xmlDesc string) (string, bool) {
	if hasClipboardChannel(xmlDesc) {
		return xmlDesc, false
	}
	locs := devicesCloseRe.FindAllStringIndex(xmlDesc, -1)
	if len(locs) != 1 {
		return xmlDesc, false
	}
	at := locs[0][0]
	return xmlDesc[:at] + "  " + clipboardChannelXML + "\n  " + xmlDesc[at:], true
}

// isClipboardChannelError reports whether a define failed over the
// qemu-vdagent channel itself (a QEMU built without it despite a new
// enough version), so the caller can retry without it.
func isClipboardChannelError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "vdagent")
}

// ensureClipboardChannel gives a stopped VM that lacks it the clipboard
// channel, in its persistent config. Running VMs are left alone - they get
// it through StartVM on their next start. A failed define leaves the old
// definition exactly as it was, so there's nothing to roll back. It reports
// whether anything changed.
func ensureClipboardChannel(conn *libvirt.Connect, dom *libvirt.Domain, supported bool) (bool, error) {
	if !supported {
		return false, nil
	}
	if active, err := dom.IsActive(); err != nil || active {
		return false, nil
	}
	xmlDesc, err := dom.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err != nil {
		return false, err
	}
	newXML, changed := planClipboardChannel(xmlDesc)
	if !changed {
		return false, nil
	}
	name, _ := dom.GetName()
	nd, err := conn.DomainDefineXML(newXML)
	if err != nil {
		return false, fmt.Errorf("add clipboard channel to %s: %w", name, err)
	}
	nd.Free()
	log.Printf("vm %s: added the clipboard channel (takes effect on its next start)", name)
	return true, nil
}

func (s *LibvirtStore) clipboardSupported() bool {
	conn, err := s.getConn()
	return err == nil && hostSupportsClipboardChannel(conn)
}

// defineDomainXML defines xmlDesc (rendered from data), and if that fails
// over the clipboard channel alone, defines it again without one - a VM
// without clipboard sharing beats no VM at all.
func defineDomainXML(conn *libvirt.Connect, data domainXMLData, xmlDesc string) (*libvirt.Domain, error) {
	dom, err := conn.DomainDefineXML(xmlDesc)
	if err == nil || !data.ClipboardChannel || !isClipboardChannelError(err) {
		return dom, err
	}
	log.Printf("vm %s: libvirt refused the clipboard channel, defining it without one: %v", data.Name, err)
	data.ClipboardChannel = false
	var b strings.Builder
	if rerr := domainTemplate.Execute(&b, data); rerr != nil {
		return nil, err
	}
	return conn.DomainDefineXML(b.String())
}
