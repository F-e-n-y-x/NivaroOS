// Package ntfsperm makes a drive mounted with the kernel ntfs3 driver
// present ownership the way ntfs-3g did.
//
// ntfs-3g showed every file as the mount's uid/gid with mode 0777 and
// ignored chown/chmod. ntfs3 (much faster) instead stores the creating
// process's owner and mode on each file ($LXUID/$LXGID/$LXMOD) and those
// win over the uid=/gid=/fmask= mount options. On a NAS nearly everything
// writes as root (the Files app, samba with force user = root, containers),
// so files became read-only for the desktop user and for apps that don't
// run as root.
//
// A Watcher listens to fanotify for the whole filesystem (create, rename
// in, attribute change) and puts each touched entry back to the policy
// the mount options ask for. Repair does the same for a whole tree once.
package ntfsperm

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

// Policy is the owner and permissions every entry should have.
type Policy struct {
	UID, GID          int
	FileMode, DirMode os.FileMode
}

// fix brings one entry in line. It reports whether it changed anything.
func (p Policy) fix(path string) (bool, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return false, nil
	}
	changed := false
	if int(st.Uid) != p.UID || int(st.Gid) != p.GID {
		if err := os.Lchown(path, p.UID, p.GID); err != nil {
			return false, err
		}
		changed = true
	}
	if fi.Mode()&fs.ModeSymlink != 0 {
		return changed, nil // symlinks have no mode of their own
	}
	want := p.FileMode
	if fi.IsDir() {
		want = p.DirMode
	}
	if fi.Mode().Perm() != want {
		if err := os.Chmod(path, want); err != nil {
			return changed, err
		}
		changed = true
	}
	return changed, nil
}

// Repair walks root and fixes every entry. It returns how many it changed.
func Repair(ctx context.Context, root string, p Policy) (int, error) {
	fixed := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return nil // unreadable or vanished: nothing to fix
		}
		if d.IsDir() && (d.Name() == "System Volume Information" || d.Name() == "$RECYCLE.BIN") {
			return filepath.SkipDir
		}
		if ok, _ := p.fix(path); ok {
			fixed++
		}
		return nil
	})
	return fixed, err
}

// policyFor derives the policy from a mount's filesystem type and super
// options. Only ntfs3 mounts that name an owner (uid=) get one.
func policyFor(fstype, opts string) (Policy, bool) {
	if fstype != "ntfs3" {
		return Policy{}, false
	}
	p := Policy{UID: -1, GID: 0, FileMode: 0o777, DirMode: 0o777}
	for _, o := range strings.Split(opts, ",") {
		k, v, _ := strings.Cut(o, "=")
		switch k {
		case "uid":
			p.UID, _ = strconv.Atoi(v)
		case "gid":
			p.GID, _ = strconv.Atoi(v)
		case "fmask", "dmask", "umask":
			m, err := strconv.ParseUint(v, 8, 32)
			if err != nil {
				continue
			}
			if k != "dmask" {
				p.FileMode = 0o777 &^ os.FileMode(m)
			}
			if k != "fmask" {
				p.DirMode = 0o777 &^ os.FileMode(m)
			}
		}
	}
	if p.UID < 0 {
		return Policy{}, false
	}
	return p, true
}

// Mount is an ntfs3 mount point that should be watched.
type Mount struct {
	Point  string
	Policy Policy
}

// Mounts lists the current ntfs3 mounts that ask for an owner.
func Mounts() []Mount {
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []Mount
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		for i, x := range fields {
			if x != "-" || i+3 >= len(fields) || len(fields) < 5 {
				continue
			}
			if p, ok := policyFor(fields[i+1], fields[i+3]); ok {
				point := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\134`, `\`).Replace(fields[4])
				out = append(out, Mount{Point: point, Policy: p})
			}
			break
		}
	}
	return out
}

// Watcher keeps one mounted filesystem in line with its policy.
type Watcher struct {
	fan, mountFD int
	policy       Policy
	done         chan struct{}
	closeOnce    sync.Once
	wg           sync.WaitGroup
}

const watchMask = unix.FAN_CREATE | unix.FAN_MOVED_TO | unix.FAN_ATTRIB | unix.FAN_ONDIR

// Start begins watching the filesystem mounted at mountPoint. Events are
// already being collected when it returns.
func Start(mountPoint string, p Policy) (*Watcher, error) {
	fan, err := unix.FanotifyInit(unix.FAN_CLASS_NOTIF|unix.FAN_REPORT_DFID_NAME|unix.FAN_UNLIMITED_QUEUE|unix.FAN_CLOEXEC|unix.FAN_NONBLOCK, unix.O_RDONLY|unix.O_LARGEFILE)
	if err != nil {
		return nil, fmt.Errorf("fanotify_init: %w", err)
	}
	if err := unix.FanotifyMark(fan, unix.FAN_MARK_ADD|unix.FAN_MARK_FILESYSTEM, watchMask, unix.AT_FDCWD, mountPoint); err != nil {
		unix.Close(fan)
		return nil, fmt.Errorf("fanotify_mark %s: %w", mountPoint, err)
	}
	mfd, err := unix.Open(mountPoint, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		unix.Close(fan)
		return nil, err
	}
	w := &Watcher{fan: fan, mountFD: mfd, policy: p, done: make(chan struct{})}
	w.wg.Add(1)
	go w.loop()
	return w, nil
}

func (w *Watcher) Close() {
	w.closeOnce.Do(func() {
		close(w.done)
		w.wg.Wait()
		unix.Close(w.fan)
		unix.Close(w.mountFD)
	})
}

func (w *Watcher) loop() {
	defer w.wg.Done()
	buf := make([]byte, 64<<10)
	fds := []unix.PollFd{{Fd: int32(w.fan), Events: unix.POLLIN}}
	for {
		select {
		case <-w.done:
			return
		default:
		}
		// Poll with a timeout so Close is noticed promptly.
		n, err := unix.Poll(fds, 200)
		if err != nil && !errors.Is(err, unix.EINTR) {
			return
		}
		if n <= 0 {
			continue
		}
		for {
			n, err := unix.Read(w.fan, buf)
			if err != nil || n <= 0 {
				break
			}
			w.handle(buf[:n])
		}
	}
}

const metaSize = 24 // sizeof(struct fanotify_event_metadata)

func (w *Watcher) handle(b []byte) {
	for len(b) >= metaSize {
		evLen := int(binary.LittleEndian.Uint32(b[0:4]))
		metaLen := int(binary.LittleEndian.Uint16(b[6:8]))
		mask := binary.LittleEndian.Uint64(b[8:16])
		if evLen < metaSize || evLen > len(b) {
			return
		}
		if mask&unix.FAN_Q_OVERFLOW == 0 {
			if p, ok := w.pathOf(b[metaLen:evLen]); ok {
				_, _ = w.policy.fix(p)
			}
		}
		b = b[evLen:]
	}
}

// pathOf decodes a DFID_NAME info record into "<dir>/<name>".
func (w *Watcher) pathOf(info []byte) (string, bool) {
	for len(info) >= 4 {
		typ := info[0]
		recLen := int(binary.LittleEndian.Uint16(info[2:4]))
		if recLen < 4 || recLen > len(info) {
			return "", false
		}
		rec := info[:recLen]
		info = info[recLen:]
		if typ != unix.FAN_EVENT_INFO_TYPE_DFID_NAME || len(rec) < 4+8+8 {
			continue
		}
		fh := rec[4+8:] // header, fsid
		hBytes := int(binary.LittleEndian.Uint32(fh[0:4]))
		hType := int32(binary.LittleEndian.Uint32(fh[4:8]))
		if 8+hBytes > len(fh) {
			return "", false
		}
		handle := unix.NewFileHandle(hType, fh[8:8+hBytes])
		name := fh[8+hBytes:]
		if i := indexZero(name); i >= 0 {
			name = name[:i]
		}
		dfd, err := unix.OpenByHandleAt(w.mountFD, handle, unix.O_PATH|unix.O_CLOEXEC)
		if err != nil {
			return "", false // gone already
		}
		dir, err := os.Readlink("/proc/self/fd/" + strconv.Itoa(dfd))
		unix.Close(dfd)
		if err != nil {
			return "", false
		}
		if len(name) == 0 || string(name) == "." {
			return dir, true
		}
		return filepath.Join(dir, string(name)), true
	}
	return "", false
}

func indexZero(b []byte) int {
	for i, c := range b {
		if c == 0 {
			return i
		}
	}
	return -1
}
