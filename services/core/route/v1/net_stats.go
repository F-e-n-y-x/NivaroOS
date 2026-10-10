package v1

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/core/model"
	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/config"
	"github.com/F-e-n-y-x/NivaroOS/services/core/service"
)

var sysClassNet = "/sys/class/net"

// NetStats is the reading for the NICs in nets (the physical ones): the
// kernel's counters plus link state, negotiated speed and today's totals.
// Every utilization reading goes through it, so it also keeps the totals.
func NetStats(nets []string, state func(string) string, now time.Time) []model.IOCountersStat {
	out := []model.IOCountersStat{}
	for _, n := range service.MyService.System().GetNetInfo() {
		for _, name := range nets {
			if n.Name != name {
				continue
			}
			item := model.IOCountersStat{
				Name: n.Name, BytesSent: n.BytesSent, BytesRecv: n.BytesRecv, PacketsSent: n.PacketsSent, PacketsRecv: n.PacketsRecv,
				Errin: n.Errin, Errout: n.Errout, Dropin: n.Dropin, Dropout: n.Dropout, Fifoin: n.Fifoin, Fifoout: n.Fifoout,
				State: state(n.Name), Time: now.Unix(),
			}
			item.Speed, item.Wireless = linkSpeed(n.Name)
			item.TodayRecv, item.TodaySent = netTodayTotals.add(n.Name, n.BytesRecv, n.BytesSent, now)
			out = append(out, item)
			break
		}
	}
	return out
}

// linkSpeed is the negotiated speed in Mbit/s, 0 when unknown: the link is
// down (the kernel says -1 or refuses to read), or it's Wi-Fi, whose
// driver doesn't report one there.
func linkSpeed(name string) (int, bool) {
	_, err := os.Stat(filepath.Join(sysClassNet, name, "wireless"))
	wireless := err == nil
	raw, err := os.ReadFile(filepath.Join(sysClassNet, name, "speed"))
	if err != nil {
		return 0, wireless
	}
	v, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || v <= 0 {
		return 0, wireless
	}
	return v, wireless
}

// netToday counts each NIC's traffic since local midnight. The kernel's
// counters start at 0 on boot, so it adds up deltas between readings and
// keeps them (with the last counters and the boot they came from) in a
// small file, which carries the totals across service restarts and reboots.
// ponytail: traffic between the last save (<= 1 min) and a reboot or a
// crash is lost, and a NIC first seen today counts from then.
type netToday struct {
	mu      sync.Mutex
	loaded  bool
	path    string // "" = not persisted
	bootID  func() string
	savedAt time.Time

	Day    string                 `json:"day"`
	Boot   string                 `json:"boot"`
	Ifaces map[string]*ifaceToday `json:"ifaces"`
}

type ifaceToday struct {
	LastRx uint64 `json:"last_rx"`
	LastTx uint64 `json:"last_tx"`
	Rx     uint64 `json:"rx"`
	Tx     uint64 `json:"tx"`
}

const netTodaySaveEvery = time.Minute

var netTodayTotals = &netToday{bootID: func() string {
	raw, _ := os.ReadFile("/proc/sys/kernel/random/boot_id")
	return strings.TrimSpace(string(raw))
}}

// add records a reading of name's counters and returns today's totals.
func (t *netToday) add(name string, rx, tx uint64, now time.Time) (uint64, uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.loaded {
		t.loaded = true
		if t.path == "" && config.AppInfo != nil && config.AppInfo.DBPath != "" {
			t.path = filepath.Join(config.AppInfo.DBPath, "net-today.json")
		}
		if t.path != "" {
			if raw, err := os.ReadFile(t.path); err == nil {
				_ = json.Unmarshal(raw, t)
			}
		}
		// Rebooted since: the counters restarted at 0.
		if boot := t.bootID(); boot != t.Boot {
			for _, i := range t.Ifaces {
				i.LastRx, i.LastTx = 0, 0
			}
			t.Boot = boot
		}
	}
	day := now.Format("2006-01-02")
	dirty := false
	if day != t.Day || t.Ifaces == nil {
		if t.Ifaces == nil {
			t.Ifaces = map[string]*ifaceToday{}
		}
		for _, i := range t.Ifaces {
			i.Rx, i.Tx = 0, 0
		}
		t.Day, dirty = day, true
	}
	i := t.Ifaces[name]
	if i == nil {
		i = &ifaceToday{LastRx: rx, LastTx: tx}
		t.Ifaces[name], dirty = i, true
	}
	// A counter that went backwards was reset (the NIC was re-created):
	// everything it shows now is new.
	delta := func(cur, last uint64) uint64 {
		if cur < last {
			return cur
		}
		return cur - last
	}
	i.Rx += delta(rx, i.LastRx)
	i.Tx += delta(tx, i.LastTx)
	i.LastRx, i.LastTx = rx, tx
	if t.path != "" && (dirty || now.Sub(t.savedAt) >= netTodaySaveEvery) {
		t.savedAt = now
		if raw, err := json.Marshal(t); err == nil {
			tmp := t.path + ".tmp"
			if os.WriteFile(tmp, raw, 0o644) != nil || os.Rename(tmp, t.path) != nil {
				os.Remove(tmp)
			}
		}
	}
	return i.Rx, i.Tx
}
