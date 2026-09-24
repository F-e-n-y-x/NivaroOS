package engine

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// sendOOB sends one byte of TCP urgent data.
func sendOOB(fd int) error { return unix.Sendto(fd, []byte{1}, unix.MSG_OOB, nil) }

func TestDiffVolumes(t *testing.T) {
	now := time.Now()
	a := mountVolume{vol: Volume{MountID: 10, UUID: "a"}}
	b := mountVolume{vol: Volume{MountID: 11, UUID: "b"}}
	b2 := mountVolume{vol: Volume{MountID: 12, UUID: "b"}} // b remounted
	c := mountVolume{vol: Volume{MountID: 13, UUID: "c"}}
	evs := diffVolumes(&snapshot{vols: []mountVolume{a, b}}, &snapshot{vols: []mountVolume{a, b2, c}}, now)
	got := map[string]int{}
	for _, ev := range evs {
		got[ev.Type+"/"+ev.Volume.UUID]++
		if ev.Initial {
			t.Errorf("%s marked initial", ev.Type)
		}
	}
	want := map[string]int{EventVolumeMounted + "/b": 1, EventVolumeMounted + "/c": 1, EventVolumeUnmounted + "/b": 1}
	if len(got) != len(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	for k, n := range want {
		if got[k] != n {
			t.Fatalf("events = %v, want %v", got, want)
		}
	}
	if evs := diffVolumes(&snapshot{vols: []mountVolume{a}}, &snapshot{vols: []mountVolume{a}}, now); len(evs) != 0 {
		t.Fatalf("no change gave %v", evs)
	}
}

// nextEvent waits for an event of one type, skipping others.
func nextEvent(t *testing.T, ch <-chan Event, typ string) Event {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatalf("event channel closed waiting for %s", typ)
			}
			if ev.Type == typ {
				return ev
			}
		case <-timeout:
			t.Fatalf("no %s event", typ)
		}
	}
}

func TestEventsInitialThenChanges(t *testing.T) {
	s := newTestSys(t)
	first := s.addVolume("first", "cccccccc-aaaa-4bbb-8ccc-00000000000c", "ext4")
	e := s.engine()
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := e.Events(ctx)
	must(t, err)
	ev := nextEvent(t, ch, EventVolumeMounted)
	if !ev.Initial || ev.Volume.UUID != first.UUID || ev.Volume.MountID != first.ID {
		t.Fatalf("initial event = %+v %+v", ev, ev.Volume)
	}
	// A drive plugged in later: mounted, not initial, with its identity.
	usb := s.addVolume("stick", "1234-ABCD", "vfat", func(m *fakeMount) { m.USB = true })
	s.notify()
	ev = nextEvent(t, ch, EventVolumeMounted)
	v := ev.Volume
	if ev.Initial || v.UUID != "1234-ABCD" || v.Serial != usb.Serial || v.Size != usb.Size || v.FSType != "vfat" || v.MountPoint != usb.MountPoint || v.Tran != "usb" || v.Label != "stick" {
		t.Fatalf("mounted = %+v %+v", ev, v)
	}
	s.unmount(usb)
	s.notify()
	ev = nextEvent(t, ch, EventVolumeUnmounted)
	if ev.Volume.MountID != usb.ID {
		t.Fatalf("unmounted = %+v", ev.Volume)
	}
	// Ending the context closes the stream.
	cancel()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("the event channel was not closed")
		}
	}
}

func TestEventsJobDone(t *testing.T) {
	_, e, src, dst := twoVolumes(t)
	writeTree(t, filepath.Join(src.MountPoint, "s"), map[string]string{"a": "1"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := e.Events(ctx)
	must(t, err)
	st := runJob(t, e, withRun(baseReq(OpCopy, ep(src, "s"), ep(dst, "d")), "run_evt"))
	ev := nextEvent(t, ch, EventJobDone)
	if ev.JobID != st.ID || ev.RunID != "run_evt" || ev.State != JobDone {
		t.Fatalf("job.done = %+v", ev)
	}
}

func TestPollWaiterSeesPriorityData(t *testing.T) {
	// The kernel signals mount-table changes on mountinfo with POLLPRI;
	// TCP urgent data raises the same flag on a socket, which stands in
	// for the mountinfo fd here.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	must(t, err)
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err == nil {
			accepted <- c
		}
	}()
	client, err := net.Dial("tcp", ln.Addr().String())
	must(t, err)
	defer client.Close()
	server := <-accepted
	defer server.Close()
	f, err := server.(*net.TCPConn).File()
	must(t, err)
	defer f.Close()
	fd := int(f.Fd())

	changed, err := pollFD(fd, 50*time.Millisecond)
	if err != nil || changed {
		t.Fatalf("idle fd: changed=%v err=%v", changed, err)
	}
	cf, err := client.(*net.TCPConn).File()
	must(t, err)
	defer cf.Close()
	must(t, sendOOB(int(cf.Fd())))
	changed, err = pollFD(fd, 5*time.Second)
	if err != nil || !changed {
		t.Fatalf("after urgent data: changed=%v err=%v", changed, err)
	}
}

func TestPollWaiterOnRealMountinfo(t *testing.T) {
	w, err := openPollWaiter("/proc/self/mountinfo")
	if err != nil {
		t.Skipf("no mountinfo: %v", err)
	}
	defer w.close()
	quit := make(chan struct{})
	start := time.Now()
	if _, err := w.wait(100*time.Millisecond, quit); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("wait ignored its timeout")
	}
	// A closed quit channel ends a long wait within about a second.
	close(quit)
	start = time.Now()
	if _, err := w.wait(time.Minute, quit); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("wait ignored quit")
	}
}

func TestWatcherFallsBackToPolling(t *testing.T) {
	s := newTestSys(t)
	waiterMu.Lock()
	prev := newChangeWaiter
	newChangeWaiter = func(string) (changeWaiter, error) { return nil, os.ErrPermission }
	cfg := s.config()
	cfg.WatchBackstop = 20 * time.Millisecond
	e, err := New(cfg)
	newChangeWaiter = prev
	waiterMu.Unlock()
	must(t, err)
	defer e.Close()
	ch, err := e.Events(context.Background())
	must(t, err)
	s.addVolume("late", "dddddddd-aaaa-4bbb-8ccc-00000000000d", "ext4")
	if ev := nextEvent(t, ch, EventVolumeMounted); ev.Volume.UUID != "dddddddd-aaaa-4bbb-8ccc-00000000000d" {
		t.Fatalf("event = %+v", ev.Volume)
	}
}
