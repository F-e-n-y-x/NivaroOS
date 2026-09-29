package main

// NVIDIA GPU fans through NVML (libnvidia-ml.so.1, loaded at run time -
// see nvml_cgo.go). NVML is the documented way to drive GeForce/Quadro
// fans without X: nvmlDeviceSetFanSpeed_v2 takes a fan over (manual
// policy) and nvmlDeviceSetDefaultFanSpeed_v2 hands it back to the
// driver's own curve. Both need driver 520 or newer; with an older driver,
// no NVIDIA driver, or a binary built without cgo, GPU fans are shown
// read-only with the reason.

import (
	"errors"
	"sync"
	"time"
)

type gpuDevice struct {
	Index   int
	UUID    string
	Name    string
	NumFans int
	MinPct  int // lowest speed the driver accepts (NVML min fan speed)
	MaxPct  int
	TempC   float64
	TempOK  bool
	// CanControl is false (with Reason) when the driver lacks the
	// fan-control entry points.
	CanControl bool
	Reason     string
}

// gpuAPI is NVML as this service uses it. Tests provide a fake.
type gpuAPI interface {
	// Devices lists the GPUs (with their temperature). err means NVML
	// itself is unavailable; the reason is shown to the user.
	Devices() ([]gpuDevice, error)
	FanSpeed(index, fan int) (int, error)
	SetFanSpeed(index, fan, pct int) error
	SetDefaultFanSpeed(index, fan int) error
	Close()
}

var errNoNVML = errors.New("NVIDIA driver library (NVML) not found")

// nvmlIdle: NVML is initialised on first use and shut down again after
// this long without any, so a box whose GPU fans are all on Auto doesn't
// keep the GPU device open (a driver update wants to unload the module).
const nvmlIdle = 60 * time.Second

// lazyGPU wraps an NVML opener with the idle shutdown.
type lazyGPU struct {
	mu      sync.Mutex
	open    func() (gpuAPI, error)
	api     gpuAPI
	lastUse time.Time
	lastErr error
	failAt  time.Time
	now     func() time.Time
}

func newLazyGPU(open func() (gpuAPI, error)) *lazyGPU {
	return &lazyGPU{open: open, now: time.Now}
}

func (l *lazyGPU) get() (gpuAPI, error) {
	l.lastUse = l.now()
	if l.api != nil {
		return l.api, nil
	}
	// Don't retry a failed open on every tick.
	if l.lastErr != nil && l.now().Sub(l.failAt) < 30*time.Second {
		return nil, l.lastErr
	}
	api, err := l.open()
	if err != nil {
		l.lastErr, l.failAt = err, l.now()
		return nil, err
	}
	l.api, l.lastErr = api, nil
	return api, nil
}

func (l *lazyGPU) Devices() ([]gpuDevice, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	api, err := l.get()
	if err != nil {
		return nil, err
	}
	return api.Devices()
}

func (l *lazyGPU) FanSpeed(i, f int) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	api, err := l.get()
	if err != nil {
		return 0, err
	}
	return api.FanSpeed(i, f)
}

func (l *lazyGPU) SetFanSpeed(i, f, p int) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	api, err := l.get()
	if err != nil {
		return err
	}
	return api.SetFanSpeed(i, f, p)
}

func (l *lazyGPU) SetDefaultFanSpeed(i, f int) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	api, err := l.get()
	if err != nil {
		return err
	}
	return api.SetDefaultFanSpeed(i, f)
}

// idle shuts NVML down when it hasn't been used for nvmlIdle and keep is
// false (keep = some GPU fan is under our control).
func (l *lazyGPU) idle(keep bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.api != nil && !keep && l.now().Sub(l.lastUse) > nvmlIdle {
		l.api.Close()
		l.api = nil
	}
}

func (l *lazyGPU) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.api != nil {
		l.api.Close()
		l.api = nil
	}
}

// nvidiaIO drives one fan of one GPU, found by UUID each time (NVML
// indexes follow PCI order and can change between boots).
type nvidiaIO struct {
	gpu  gpuAPI
	uuid string
	fan  int
}

func (n *nvidiaIO) index() (int, error) {
	devs, err := n.gpu.Devices()
	if err != nil {
		return 0, err
	}
	for _, d := range devs {
		if d.UUID == n.uuid {
			return d.Index, nil
		}
	}
	return 0, errChannelGone
}

func (n *nvidiaIO) read() (float64, int) {
	i, err := n.index()
	if err != nil {
		return -1, -1
	}
	p, err := n.gpu.FanSpeed(i, n.fan)
	if err != nil {
		return -1, -1
	}
	return float64(p), -1
}

func (n *nvidiaIO) capture(id string) (original, error) {
	return original{ID: id, Kind: kindNVIDIA, GPUUUID: n.uuid, Fan: n.fan}, nil
}

func (n *nvidiaIO) set(pct int) error {
	i, err := n.index()
	if err != nil {
		return err
	}
	return n.gpu.SetFanSpeed(i, n.fan, pct)
}

func restoreNVIDIA(gpu gpuAPI, o original) error {
	io := &nvidiaIO{gpu: gpu, uuid: o.GPUUUID, fan: o.Fan}
	i, err := io.index()
	if err != nil {
		return err
	}
	return gpu.SetDefaultFanSpeed(i, o.Fan)
}
