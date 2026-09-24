package main

import (
	"encoding/xml"
	"strings"
	"testing"
)

// A VM as versions before the clipboard channel defined it (as libvirt
// hands its XML back, addresses and all).
const noClipboardVMXML = `<domain type='kvm'>
  <name>mint</name>
  <devices>
    <channel type='unix'>
      <source mode='bind' path='/run/libvirt/qemu/channel/1-mint/org.qemu.guest_agent.0'/>
      <target type='virtio' name='org.qemu.guest_agent.0'/>
      <address type='virtio-serial' controller='0' bus='0' port='1'/>
    </channel>
    <input type='tablet' bus='usb'/>
  </devices>
</domain>`

func TestAVMWithoutAClipboardChannelGetsOne(t *testing.T) {
	out, changed := planClipboardChannel(noClipboardVMXML)
	if !changed {
		t.Fatal("expected the channel to be added")
	}
	for _, want := range []string{
		"<channel type='qemu-vdagent'>",
		"<clipboard copypaste='yes'/>",
		"<mouse mode='server'/>",
		"<target type='virtio' name='com.redhat.spice.0'/>",
		// The guest agent channel is untouched.
		"name='org.qemu.guest_agent.0'",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("planned XML lacks %q:\n%s", want, out)
		}
	}
	if !strings.HasSuffix(strings.TrimSpace(out), "</channel>\n  </devices>\n</domain>") {
		t.Fatalf("channel should go last in <devices>:\n%s", out)
	}
	var v struct{}
	if err := xml.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("planned XML isn't well-formed: %v", err)
	}
	if !hasClipboardChannel(out) {
		t.Fatal("hasClipboardChannel should see the added channel")
	}
}

func TestAVMThatHasAClipboardChannelIsLeftAlone(t *testing.T) {
	first, _ := planClipboardChannel(noClipboardVMXML)
	second, changed := planClipboardChannel(first)
	if changed || second != first {
		t.Fatal("second run should do nothing")
	}
	if strings.Count(second, "qemu-vdagent") != 1 {
		t.Fatalf("expected exactly one channel:\n%s", second)
	}
}

// A hand-added SPICE agent channel owns com.redhat.spice.0 - adding ours
// too would make the define fail on the duplicate port name.
func TestAHandAddedSpiceChannelCountsAsHavingOne(t *testing.T) {
	x := strings.Replace(noClipboardVMXML, "<input", `<channel type="spicevmc"><target type="virtio" name="com.redhat.spice.0"/></channel>
    <input`, 1)
	if _, changed := planClipboardChannel(x); changed {
		t.Fatal("a VM with a spicevmc agent channel shouldn't get another")
	}
}

func TestPlanningWithoutDevicesChangesNothing(t *testing.T) {
	x := "<domain type='kvm'><name>odd</name></domain>"
	if out, changed := planClipboardChannel(x); changed || out != x {
		t.Fatal("XML without a <devices> element should be left alone")
	}
	if hasClipboardChannel(noClipboardVMXML) {
		t.Fatal("the guest agent channel isn't a clipboard channel")
	}
}

func TestNewVMsGetTheChannelOnlyWhenSupported(t *testing.T) {
	render := func(clipboard bool) string {
		var b strings.Builder
		data := domainXMLData{Name: "vm", VCPUs: 1, MemoryMiB: 512, ISO: &renderedISO{}, UseOSBoot: true, ClipboardChannel: clipboard}
		if err := domainTemplate.Execute(&b, data); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	with, without := render(true), render(false)
	if !hasClipboardChannel(with) || !strings.Contains(with, "<mouse mode='server'/>") {
		t.Fatalf("supported host: no clipboard channel:\n%s", with)
	}
	if _, changed := planClipboardChannel(with); changed {
		t.Fatal("a freshly created VM shouldn't be migrated again")
	}
	if hasClipboardChannel(without) {
		t.Fatal("unsupported host: the channel must be left out")
	}
}
