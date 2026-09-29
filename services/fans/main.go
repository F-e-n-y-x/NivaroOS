// nivaroos-fans is NivaroOS fan control: it finds every fan it can drive
// (motherboard super-I/O, amdgpu, laptop drivers, the Pi's pwm-fan through
// hwmon; NVIDIA through NVML), runs per-fan Auto / Fixed / Curve modes
// with hard safety rules (see controller.go), and always hands the fans
// back to the BIOS/driver when it stops.
//
//	nivaroos-fans [flags]                     serve on 127.0.0.1:28644 (gateway: /v1/fans)
//	nivaroos-fans [flags] restore             hand every fan back to Auto (ExecStopPost)
//	nivaroos-fans [flags] detect-modules      find + persist the motherboard fan driver (installer)
//	nivaroos-fans [flags] status              print what is detected, as JSON
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

const (
	listenAddr   = "127.0.0.1:28644"
	tickInterval = 2 * time.Second
)

type options struct {
	addr, sysRoot, dataDir, runDir, runtimePath, modulesConf string
}

func (o options) store() *store {
	return &store{
		settingsPath:  filepath.Join(o.dataDir, "settings.json"),
		originalsPath: filepath.Join(o.runDir, "fans-originals.json"),
	}
}

func (o options) detector() detector {
	return detector{
		sysRoot:    o.sysRoot,
		ops:        realModules{sysRoot: o.sysRoot},
		confPath:   o.modulesConf,
		statePath:  filepath.Join(o.dataDir, "detect.json"),
		markerPath: filepath.Join(o.runDir, "fans-detect.done"),
		isRoot:     os.Geteuid() == 0,
	}
}

func main() {
	log.SetFlags(0)
	fs := flag.NewFlagSet("nivaroos-fans", flag.ContinueOnError)
	var o options
	fs.StringVar(&o.addr, "addr", listenAddr, "address to listen on (loopback only; the gateway routes "+apiBase+" here)")
	fs.StringVar(&o.sysRoot, "sys", "/sys", "sysfs root")
	fs.StringVar(&o.dataDir, "data-dir", "/var/lib/nivaroos/fans", "settings directory")
	fs.StringVar(&o.runDir, "run-dir", "/run/nivaroos", "per-boot state (the fans' original settings)")
	fs.StringVar(&o.runtimePath, "runtime-path", "/var/run/nivaroos", "NivaroOS runtime directory (user-service's JWKS)")
	fs.StringVar(&o.modulesConf, "modules-conf", modulesConfPath, "where the fan driver is persisted for boot")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: nivaroos-fans [flags] [restore|detect-modules|status]\n\nflags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			return
		}
		os.Exit(2)
	}
	switch fs.Arg(0) {
	case "":
		os.Exit(serve(o))
	case "restore":
		os.Exit(restoreCmd(o))
	case "detect-modules":
		r := o.detector().run(true)
		b, _ := json.MarshalIndent(r, "", "  ")
		fmt.Println(string(b))
	case "status":
		c := newController(o.sysRoot, o.store(), newLazyGPU(openNVML), os.Geteuid() == 0)
		c.mu.Lock()
		c.settings = c.store.loadSettings()
		c.detect = o.detector().load()
		c.rescanLocked()
		c.observeLocked()
		c.mu.Unlock()
		b, _ := json.MarshalIndent(c.Status(), "", "  ")
		fmt.Println(string(b))
		c.gpu.Close()
	default:
		fs.Usage()
		os.Exit(2)
	}
}

// restoreCmd is ExecStopPost: whatever happened to the service (clean
// stop, crash, watchdog kill), every fan it had taken over goes back.
func restoreCmd(o options) int {
	gpu := newLazyGPU(openNVML)
	defer gpu.Close()
	n, err := restoreLeftovers(o.sysRoot, o.store(), gpu)
	if n > 0 {
		log.Printf("fans: handed %d fan(s) back to Auto", n)
	}
	if err != nil {
		log.Printf("fans: restore: %v", err)
		return 1
	}
	return 0
}

func serve(o options) int {
	isRoot := os.Geteuid() == 0
	if err := os.MkdirAll(o.runDir, 0o755); err != nil {
		log.Printf("fans: %v", err)
	}
	gpu := newLazyGPU(openNVML)
	c := newController(o.sysRoot, o.store(), gpu, isRoot)
	c.notify = func() { sdNotify("WATCHDOG=1") }
	det := o.detector()
	c.detect = det.run(false) // loads the fan driver if this boot hasn't yet
	c.Start()

	ln, err := net.Listen("tcp", o.addr)
	if err != nil {
		log.Printf("fans: listen %s: %v", o.addr, err)
		c.Shutdown()
		return 1
	}
	srv := &http.Server{Handler: newAPI(c, det, o.runtimePath).handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("fans: http: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	t := time.NewTicker(tickInterval)
	defer t.Stop()

	// A panic in the loop must not leave fans driven: hand them back, then
	// let it crash (systemd restarts the service; ExecStopPost runs too).
	defer func() {
		if p := recover(); p != nil {
			c.Shutdown()
			panic(p)
		}
	}()
	c.Tick()
	sdNotify("READY=1")
	log.Printf("fans: serving on %s", o.addr)
	for {
		select {
		case <-t.C:
			c.Tick()
		case s := <-stop:
			log.Printf("fans: %v - handing every fan back to Auto", s)
			sdNotify("STOPPING=1")
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = srv.Shutdown(ctx)
			cancel()
			c.Shutdown()
			return 0
		}
	}
}

// sdNotify speaks the systemd notify protocol (READY, WATCHDOG).
func sdNotify(state string) {
	sock := os.Getenv("NOTIFY_SOCKET")
	if sock == "" {
		return
	}
	if sock[0] == '@' {
		sock = "\x00" + sock[1:]
	}
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: sock, Net: "unixgram"})
	if err != nil {
		return
	}
	defer conn.Close()
	_, _ = conn.Write([]byte(state))
}
