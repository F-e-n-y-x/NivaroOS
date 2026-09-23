package main

import (
	"encoding/xml"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckShareSource_RejectsDeniedRootsTheirAncestorsAndContents(t *testing.T) {
	denied := []string{"/DATA/VM-Shares", "/DATA/VMs"}
	const exempt = "/DATA/VMs/share"
	cases := map[string]bool{
		"/DATA/Media":          true,
		"/DATA/Media/Photos":   true,
		exempt:                 true,
		"/DATA":                false, // the allowed root itself (ancestor of every denied root)
		"/":                    false,
		"/etc":                 false,
		"/DATA/VMs":            false,
		"/DATA/VMs/other":      false,
		"/DATA/VMs/ISOs":       false,
		"/DATA/VM-Shares":      false,
		"/DATA/VM-Shares/vm/x": false,
		"/DATAX":               false,
	}
	for path, ok := range cases {
		err := checkShareSource(path, "/DATA", denied, exempt)
		if (err == nil) != ok {
			t.Errorf("%s: allowed=%v, want %v (err %v)", path, err == nil, ok, err)
		}
	}
	// A denied root nested deeper than the allowed root still makes every
	// ancestor of it unshareable.
	if err := checkShareSource("/DATA/pool", "/DATA", []string{"/DATA/pool/VMs"}, ""); err == nil {
		t.Error("expected an ancestor of a denied root to be rejected")
	}
}

func TestDefaultShareOnlyRule(t *testing.T) {
	if !isDefaultShare(SharedFolderSpec{SourceDir: defaultAutoShareDir + "/", TargetTag: "share", ReadOnly: true}) {
		t.Error("the default share (any read-only flag, uncleaned path) must be allowed")
	}
	for _, sf := range []SharedFolderSpec{
		{SourceDir: defaultAutoShareDir, TargetTag: "other"},
		{SourceDir: defaultAutoShareDir, TargetTag: ""},
		{SourceDir: defaultAutoShareDir + "/sub", TargetTag: "share"},
		{SourceDir: "/DATA/Media", TargetTag: "share"},
	} {
		if isDefaultShare(sf) {
			t.Errorf("%+v must not count as the default share", sf)
		}
	}

	got, err := normalizeRequestedShares(nil, []SharedFolderSpec{{SourceDir: defaultAutoShareDir, TargetTag: "share", ReadOnly: true}})
	if err != nil || len(got) != 1 || !isDefaultShare(got[0]) || !got[0].ReadOnly {
		t.Errorf("nil request should keep the default share and its read-only flag, got %+v, %v", got, err)
	}
	got, err = normalizeRequestedShares([]SharedFolderSpec{}, nil)
	if err != nil || len(got) != 1 || !isDefaultShare(got[0]) {
		t.Errorf("an empty list should still mean the default share, got %+v, %v", got, err)
	}
	_, err = normalizeRequestedShares([]SharedFolderSpec{{SourceDir: defaultAutoShareDir, TargetTag: "share"}, {SourceDir: "/DATA/Media", TargetTag: "media"}}, nil)
	if err == nil || err.Error() != "Only the default shared folder /DATA/VMs/share is allowed" || errorStatus(err, 0) != 400 {
		t.Errorf("expected the default-only 400, got %v", err)
	}
}

func TestSharedFolderAPI_DefaultOnly(t *testing.T) {
	store := newTestStore(t)
	createStoppedVM(t, store, CreateVMRequest{Name: "share-api-vm", VCPUs: 1, MemoryMiB: 256})

	_, err := store.AttachSharedFolder("share-api-vm", SharedFolderSpec{SourceDir: t.TempDir(), TargetTag: "share"})
	if err == nil || err.Error() != "Only the default shared folder /DATA/VMs/share is allowed" {
		t.Errorf("attach of a custom folder: expected the default-only 400, got %v", err)
	}
	err = store.DetachSharedFolder("share-api-vm", "share")
	if err == nil || err.Error() != "The default shared folder can't be removed" || errorStatus(err, 0) != 400 {
		t.Errorf("detach of the default share: expected 400, got %v", err)
	}
	if _, err := store.AttachSharedFolder("nope", SharedFolderSpec{SourceDir: defaultAutoShareDir}); !isNotFound(err) {
		t.Errorf("attach to a missing VM must fail on the lookup first, got %v", err)
	}
	shares, err := store.ListSharedFolders("share-api-vm")
	if err != nil || len(shares) != 1 || !isDefaultShare(shares[0]) || shares[0].ReadOnly {
		t.Errorf("expected just the default share, got %+v, %v", shares, err)
	}
	warning, err := store.AttachSharedFolder("share-api-vm", SharedFolderSpec{SourceDir: defaultAutoShareDir, TargetTag: "share", ReadOnly: true})
	if err != nil || warning != "" {
		t.Fatalf("attach of the default share on a stopped VM: %q, %v", warning, err)
	}
	if shares, _ := store.ListSharedFolders("share-api-vm"); len(shares) != 1 || !shares[0].ReadOnly {
		t.Errorf("expected the default share to be read-only now, got %+v", shares)
	}
}

const legacyDomainXML = `<domain type='kvm'>
  <name>old-vm</name>
  <uuid>7747bba7-596e-448a-be61-2f9fcf182703</uuid>
  <memory unit='KiB'>4194304</memory>
  <vcpu placement='static'>2</vcpu>
  <os><type arch='x86_64' machine='pc-q35-10.0'>hvm</type></os>
  <devices>
    <emulator>/usr/bin/qemu-system-x86_64</emulator>
    <filesystem type='mount' accessmode='passthrough'>
      <driver type='virtiofs'/>
      <source dir='/DATA/VM-Shares/old-vm'/>
      <target dir='nivaroshare'/>
    </filesystem>
    <tpm model='tpm-crb'><backend type='emulator' version='2.0'/></tpm>
  </devices>
</domain>`

func TestSetDefaultShareInXML_AddsDeviceAndSharedMemory(t *testing.T) {
	out, changed, err := setDefaultShareInXML(legacyDomainXML, false, true)
	if err != nil || !changed {
		t.Fatalf("expected a change, got changed=%v err=%v", changed, err)
	}
	var parsed domainXML
	if err := xml.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("result doesn't parse: %v\n%s", err, out)
	}
	shares := sharesFromXML(parsed)
	if len(shares) != 2 || shares[0].TargetTag != "nivaroshare" || shares[1].TargetTag != "share" ||
		shares[1].SourceDir != filepath.Clean(defaultAutoShareDir) || shares[1].ReadOnly {
		t.Fatalf("expected the legacy device kept and the default share added, got %+v", shares)
	}
	for _, want := range []string{"<binary path='" + virtiofsdPath + "'/>", "<access mode='shared'/>", "<source type='memfd'/>", "<tpm model='tpm-crb'>", "<uuid>7747bba7"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in result:\n%s", want, out)
		}
	}

	// Idempotent: a second pass changes nothing.
	again, changed, err := setDefaultShareInXML(out, false, true)
	if err != nil || changed || again != out {
		t.Fatalf("expected no further change, got changed=%v err=%v", changed, err)
	}
	// onlyIfMissing=false flips an existing device's read-only flag in place.
	ro, changed, err := setDefaultShareInXML(out, true, false)
	if err != nil || !changed || strings.Count(ro, "<target dir='share'/>") != 1 || !strings.Contains(ro, "<readonly/>") {
		t.Fatalf("expected one read-only default share, got changed=%v err=%v\n%s", changed, err, ro)
	}
}

func TestEnsureSharedMemoryBacking(t *testing.T) {
	cases := map[string]string{
		"missing":      "<domain><memory>1</memory><vcpu>1</vcpu><devices/></domain>",
		"no access":    "<domain><memoryBacking><hugepages/></memoryBacking><devices/></domain>",
		"self-closing": "<domain><memoryBacking/><devices/></domain>",
	}
	for name, in := range cases {
		out, changed, err := ensureSharedMemoryBacking(in)
		if err != nil || !changed || strings.Count(out, "<access mode='shared'/>") != 1 || strings.Count(out, "<memoryBacking>") != 1 {
			t.Errorf("%s: got changed=%v err=%v out=%s", name, changed, err, out)
		}
	}
	shared := "<domain><memoryBacking><source type='memfd'/><access mode=\"shared\"/></memoryBacking></domain>"
	if out, changed, err := ensureSharedMemoryBacking(shared); err != nil || changed || out != shared {
		t.Errorf("already shared: expected no change, got changed=%v err=%v", changed, err)
	}
	if _, _, err := ensureSharedMemoryBacking("<domain><memoryBacking><access mode='private'/></memoryBacking></domain>"); err == nil {
		t.Error("explicitly private memory: expected an error rather than a silent override")
	}
}

func TestStartVM_HealsMissingDefaultShare(t *testing.T) {
	store := newTestStore(t)
	createStoppedVM(t, store, CreateVMRequest{Name: "heal-vm", VCPUs: 1, MemoryMiB: 256})
	dom, err := store.lookup("heal-vm")
	if err != nil {
		t.Fatal(err)
	}
	// Strip the share device, as on a VM created before it existed.
	x, _ := dom.GetXMLDesc(0)
	stripped := filesystemElemRe.ReplaceAllString(x, "")
	conn, _ := store.getConn()
	d2, err := conn.DomainDefineXML(stripped)
	if err != nil {
		t.Fatal(err)
	}
	d2.Free()
	dom.Free()
	if vm, _ := store.GetVM("heal-vm"); len(vm.SharedFolders) != 0 {
		t.Fatalf("setup: expected no share device, got %+v", vm.SharedFolders)
	}

	if err := store.StartVM("heal-vm"); err != nil {
		t.Fatalf("StartVM: %v", err)
	}
	vm, _ := store.GetVM("heal-vm")
	if len(vm.SharedFolders) != 1 || !isDefaultShare(vm.SharedFolders[0]) {
		t.Fatalf("expected StartVM to add the default share, got %+v", vm.SharedFolders)
	}
}
