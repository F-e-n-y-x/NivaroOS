//go:build webrtc_integration

// Measures every preset on a throwaway Xvfb (never the host's display):
//
//	WEBRTC_BENCH=1 go test -tags webrtc_integration -run BenchPresets -v .
//
// WEBRTC_BENCH_ENCODERS (default "x264enc nvh264enc"), WEBRTC_BENCH_SIZE
// (default 3398x1220). Each nvh264enc run holds one NVENC session.
// Scenes: "full" = full-screen motion (a video), "window" = a 960x540
// moving window on a still desktop. CPU is the encoder child's, in % of
// one core.
package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// cpuTicks: utime+stime of pid (all threads), in clock ticks.
func cpuTicks(pid int) int64 {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0
	}
	f := strings.Fields(string(b[strings.LastIndexByte(string(b), ')')+2:]))
	u, _ := strconv.ParseInt(f[11], 10, 64)
	s, _ := strconv.ParseInt(f[12], 10, 64)
	return u + s
}

func TestBenchPresets(t *testing.T) {
	if os.Getenv("WEBRTC_BENCH") == "" {
		t.Skip("WEBRTC_BENCH not set")
	}
	for _, b := range []string{"Xvfb", "ffplay", gstLaunch} {
		if _, err := exec.LookPath(b); err != nil {
			t.Skipf("%s not found", b)
		}
	}
	size := os.Getenv("WEBRTC_BENCH_SIZE")
	if size == "" {
		size = "3398x1220"
	}
	var W, H int
	fmt.Sscanf(size, "%dx%d", &W, &H)
	encs := strings.Fields(os.Getenv("WEBRTC_BENCH_ENCODERS"))
	if len(encs) == 0 {
		encs = []string{"x264enc", "nvh264enc"}
	}
	disp := freeDisplay(t)
	startBG(t, "Xvfb", disp, "-screen", "0", size+"x24", "-nolisten", "tcp")
	waitFor(t, "Xvfb", func() bool { return displaySocketExists(disp) })

	scenes := map[string][]string{
		"full":   {"-f", "lavfi", "-i", fmt.Sprintf("testsrc2=s=%dx%d:r=30", W, H), "-fs"},
		"window": {"-f", "lavfi", "-i", "testsrc2=s=960x540:r=30", "-left", "200", "-top", "200"},
	}
	t.Logf("%-8s %-10s %-9s %9s %6s %8s %7s", "scene", "encoder", "preset", "size", "fps", "Mbit/s", "CPU%")
	for _, scene := range []string{"full", "window"} {
		ff := exec.Command("ffplay", append(scenes[scene], "-an", "-noborder", "-loglevel", "quiet")...)
		ff.Env = append(os.Environ(), "DISPLAY="+disp, "XAUTHORITY=/dev/null", "SDL_VIDEODRIVER=x11", "SDL_AUDIODRIVER=dummy")
		if err := ff.Start(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Second)
		for _, enc := range encs {
			for _, preset := range presetOrder {
				sp := resolvePreset(enc, preset, 0, 0)
				udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
				if err != nil {
					t.Fatal(err)
				}
				p := encParams{Encoder: enc, Preset: preset, FPS: sp.FPS, Kbps: sp.Kbps, MaxW: sp.MaxW, W: W, H: H, Seq: -1, TS: -1, CudaScale: os.Getenv("WEBRTC_BENCH_NOCUDASCALE") == "",
					Port: udp.LocalAddr().(*net.UDPAddr).Port}
				cmd := exec.Command(gstLaunch, x11Argv(disp, p)...)
				cmd.Env = append(os.Environ(), "DISPLAY="+disp, "XAUTHORITY=/dev/null")
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				var bytes, frames atomic.Int64
				var counting atomic.Bool
				go func() {
					b := make([]byte, 1500)
					for {
						n, err := udp.Read(b)
						if err != nil {
							return
						}
						if counting.Load() && n >= 12 {
							bytes.Add(int64(n))
							if b[1]&0x80 != 0 { // marker: last packet of a frame
								frames.Add(1)
							}
						}
					}
				}()
				time.Sleep(2 * time.Second) // warm up
				counting.Store(true)
				c0, t0 := cpuTicks(cmd.Process.Pid), time.Now()
				time.Sleep(10 * time.Second)
				c1, el := cpuTicks(cmd.Process.Pid), time.Since(t0).Seconds()
				counting.Store(false)
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				udp.Close()
				ew, eh := encodeSize(sp.MaxW, W, H)
				t.Logf("%-8s %-10s %-9s %9s %6.1f %8.2f %7.0f", scene, enc, preset, fmt.Sprintf("%dx%d", ew, eh),
					float64(frames.Load())/el, float64(bytes.Load())*8/el/1e6, float64(c1-c0)/100/el*100)
			}
		}
		_ = ff.Process.Kill()
		_ = ff.Wait()
	}
}
