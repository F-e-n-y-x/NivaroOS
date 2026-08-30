// setup.go detects whether the virtualization stack (QEMU/KVM, libvirt)
// is installed and provides an explicit, user-triggered install path.
// This sidecar never installs or changes system packages/services on its
// own - only in response to a direct POST /setup/install call.
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"

	libvirt "libvirt.org/go/libvirt"
)

// defaultStorageDir is a var, not a const, so tests can point it at a
// temp dir instead of the real /DATA/VMs path.
var defaultStorageDir = "/DATA/VMs"

const defaultISODir = "/DATA/VMs/isos"

// requiredPackages are the exact Debian 13 (trixie) package names for
// the virtualization stack - "qemu-kvm" does not exist as a package on
// trixie, the equivalent is qemu-system-x86 (+ qemu-utils for qemu-img).
var requiredPackages = []string{
	"qemu-system-x86",
	"qemu-utils",
	"libvirt-daemon-system",
	"libvirt-clients",
	"ovmf",
}

const defaultNetworkXML = `<network>
  <name>default</name>
  <bridge name='virbr0'/>
  <forward mode='nat'/>
  <ip address='192.168.122.1' netmask='255.255.255.0'>
    <dhcp>
      <range start='192.168.122.2' end='192.168.122.254'/>
    </dhcp>
  </ip>
</network>`

type SetupStatus struct {
	MissingPackages  []string `json:"missing_packages"`
	LibvirtReachable bool     `json:"libvirt_reachable"`
	Ready            bool     `json:"ready"`
}

func packageInstalled(pkg string) bool {
	out, err := exec.Command("dpkg-query", "-W", "-f=${Status}", pkg).Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "install ok installed"
}

func CheckSetupStatus(store *LibvirtStore) SetupStatus {
	status := SetupStatus{}
	for _, pkg := range requiredPackages {
		if !packageInstalled(pkg) {
			status.MissingPackages = append(status.MissingPackages, pkg)
		}
	}
	if _, err := store.getConn(); err == nil {
		status.LibvirtReachable = true
	}
	status.Ready = len(status.MissingPackages) == 0 && status.LibvirtReachable
	return status
}

// InstallResult reports exactly which step failed - never a generic
// "install failed" - so the UI can show the real apt/systemctl output.
type InstallResult struct {
	Step    string `json:"step"`
	Output  string `json:"output,omitempty"`
	Success bool   `json:"success"`
}

// RunSetupInstall installs whatever packages are missing (one at a time,
// so a failure names the exact package), enables+starts libvirtd, then
// ensures the default storage pool and NAT network exist and are set to
// autostart. It stops at the first failing step.
func RunSetupInstall(store *LibvirtStore, storageDir, isoDir string) InstallResult {
	for _, pkg := range requiredPackages {
		if packageInstalled(pkg) {
			continue
		}
		step := fmt.Sprintf("apt-get install -y %s", pkg)
		out, err := exec.Command("apt-get", "install", "-y", pkg).CombinedOutput()
		if err != nil {
			return InstallResult{Step: step, Output: strings.TrimSpace(string(out)), Success: false}
		}
	}

	step := "systemctl enable --now libvirtd"
	if out, err := exec.Command("systemctl", "enable", "--now", "libvirtd").CombinedOutput(); err != nil {
		return InstallResult{Step: step, Output: strings.TrimSpace(string(out)), Success: false}
	}

	conn, err := store.getConn()
	if err != nil {
		return InstallResult{Step: "connect to libvirtd", Output: err.Error(), Success: false}
	}

	if err := os.MkdirAll(isoDir, 0755); err != nil {
		return InstallResult{Step: fmt.Sprintf("create ISO directory %s", isoDir), Output: err.Error(), Success: false}
	}

	poolXML := fmt.Sprintf(`<pool type='dir'><name>default</name><target><path>%s</path></target></pool>`, storageDir)
	if err := ensurePoolActive(conn, "default", poolXML); err != nil {
		return InstallResult{Step: "create default storage pool", Output: err.Error(), Success: false}
	}

	if err := ensureNetworkActive(conn, "default", defaultNetworkXML); err != nil {
		return InstallResult{Step: "create default NAT network", Output: err.Error(), Success: false}
	}

	// Best-effort: a bridged network is genuinely useful (VMs get a real
	// LAN address instead of being stuck behind NAT) but only safe to set
	// up automatically when there's exactly one unambiguous, spare NIC to
	// use - never on the interface carrying the default route (that's
	// what actually matters: it's how this API request itself arrived),
	// and never a guess between multiple candidates. Any other outcome
	// (no spare NIC, more than one, one already bridged) just means
	// staying NAT-only, which is not a setup failure.
	if err := ensureDefaultBridge(defaultBridgeRegistryPath, interfacesDotDDir); err != nil {
		log.Printf("setup: skipping automatic bridge creation: %v", err)
	}

	return InstallResult{Step: "done", Success: true}
}

// ensureDefaultBridge auto-creates a bridge named "br0" over the host's
// one spare physical NIC (if there's exactly one), so bridged networking
// works out of the box without a manual trip to Networks -> Create
// bridged network first. Idempotent: does nothing if br0 (or any bridge
// over the same NIC) is already registered.
func ensureDefaultBridge(registryPath, interfacesDir string) error {
	bridges, err := loadBridgeRegistry(registryPath)
	if err != nil {
		return fmt.Errorf("load bridge registry: %w", err)
	}
	if len(bridges) > 0 {
		return nil // something's already been set up (auto or manual) - leave it alone
	}

	all, err := listPhysicalInterfaces()
	if err != nil {
		return fmt.Errorf("list physical interfaces: %w", err)
	}
	dflt, err := defaultRouteInterface()
	if err != nil {
		return fmt.Errorf("determine default route interface: %w", err)
	}
	spare := make([]string, 0, len(all))
	for _, nic := range all {
		if nic != dflt {
			spare = append(spare, nic)
		}
	}
	if len(spare) != 1 {
		return fmt.Errorf("need exactly one spare NIC (not carrying the default route) to auto-bridge, found %d: %v", len(spare), spare)
	}

	_, err = CreateBridgeNetwork(registryPath, interfacesDir, CreateBridgeRequest{Name: "br0", HostNIC: spare[0]})
	return err
}

// ensurePoolActive looks up an existing pool by name, defining it from
// poolXML if it doesn't exist yet, then makes sure it's active and set
// to autostart. Shared by setup's default pool and (in a later task)
// any additional pools the user configures.
func ensurePoolActive(conn *libvirt.Connect, name, poolXML string) error {
	pool, err := conn.LookupStoragePoolByName(name)
	if err != nil {
		pool, err = conn.StoragePoolDefineXML(poolXML, 0)
		if err != nil {
			return fmt.Errorf("define pool: %w", err)
		}
		if err := pool.Build(libvirt.STORAGE_POOL_BUILD_NEW); err != nil {
			log.Printf("storage pool %q build: %v (continuing - target directory may already exist)", name, err)
		}
	}
	defer pool.Free()

	active, err := pool.IsActive()
	if err != nil {
		return fmt.Errorf("check pool active: %w", err)
	}
	if !active {
		if err := pool.Create(0); err != nil {
			return fmt.Errorf("start pool: %w", err)
		}
	}
	return pool.SetAutostart(true)
}

// ensureNetworkActive mirrors ensurePoolActive for libvirt networks.
func ensureNetworkActive(conn *libvirt.Connect, name, networkXML string) error {
	net, err := conn.LookupNetworkByName(name)
	if err != nil {
		net, err = conn.NetworkDefineXML(networkXML)
		if err != nil {
			return fmt.Errorf("define network: %w", err)
		}
	}
	defer net.Free()

	active, err := net.IsActive()
	if err != nil {
		return fmt.Errorf("check network active: %w", err)
	}
	if !active {
		if err := net.Create(); err != nil {
			return fmt.Errorf("start network: %w", err)
		}
	}
	return net.SetAutostart(true)
}

func RegisterSetupRoutes(mux *http.ServeMux, store *LibvirtStore, storageDir, isoDir string) {
	mux.HandleFunc("GET /setup/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, CheckSetupStatus(store))
	})

	mux.HandleFunc("POST /setup/install", func(w http.ResponseWriter, r *http.Request) {
		result := RunSetupInstall(store, storageDir, isoDir)
		status := http.StatusOK
		if !result.Success {
			status = http.StatusInternalServerError
		}
		writeJSON(w, status, result)
	})
}
