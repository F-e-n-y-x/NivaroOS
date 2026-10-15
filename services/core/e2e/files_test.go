//go:build e2e

package e2e

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"math/rand"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// ---- data helpers ----

// writeRandom writes size pseudo-random bytes (seeded) and returns the sha256.
func writeRandom(t *testing.T, p string, size int64, seed int64) string {
	t.Helper()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(rand.New(rand.NewSource(seed)), size)); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// writeSparse makes a size-byte sparse file with random 1 MiB islands, so
// the checksum still catches misplaced or dropped blocks.
func writeSparse(t *testing.T, p string, size int64) {
	t.Helper()
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	r := rand.New(rand.NewSource(size))
	buf := make([]byte, 1<<20)
	for off := int64(0); off < size; off += 512 << 20 {
		r.Read(buf)
		n := int64(len(buf))
		if off+n > size {
			n = size - off
		}
		if _, err := f.WriteAt(buf[:n], off); err != nil {
			t.Fatal(err)
		}
	}
	r.Read(buf)
	if _, err := f.WriteAt(buf[:4096], size-4096); err != nil {
		t.Fatal(err)
	}
}

func sum(t *testing.T, p string) string {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// tree maps every entry under dir to its checksum ("dir", "link:<target>").
func tree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		switch {
		case p == dir:
		case d.Type()&fs.ModeSymlink != 0:
			target, _ := os.Readlink(p)
			out[rel] = "link:" + target
		case d.IsDir():
			out[rel] = "dir"
		default:
			out[rel] = sum(t, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sameTree(t *testing.T, want, got map[string]string, what string) {
	t.Helper()
	bad := 0
	for k, v := range want {
		if got[k] != v {
			if bad++; bad <= 10 {
				t.Errorf("%s: %q: want %.20s got %.20s", what, k, v, got[k])
			}
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			if bad++; bad <= 10 {
				t.Errorf("%s: unexpected %q", what, k)
			}
		}
	}
	if bad > 10 {
		t.Errorf("%s: %d differences in total", what, bad)
	}
}

// noLeftovers: no temp/partial files anywhere under dir.
func noLeftovers(t *testing.T, dir string) {
	t.Helper()
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && (strings.Contains(d.Name(), ".nvtmp-") || strings.Contains(d.Name(), ".nvupload-")) {
			t.Errorf("leftover temp file %s", p)
		}
		return nil
	})
}

// makeTree builds a mixed folder: unicode/space names, nesting, empty
// file, empty dir, a symlink.
func makeTree(t *testing.T, root string, files int) map[string]string {
	t.Helper()
	names := []string{"plain.txt", "with space.bin", "ünïcödé ✓.dat", "日本語のファイル.txt", "emoji 🎉🎉.jpg", "#hash & amp%.txt", "a'quote\".txt"}
	for i := 0; i < files; i++ {
		dir := filepath.Join(root, fmt.Sprintf("d%d", i%7), fmt.Sprintf("sub %d", i%3))
		writeRandom(t, filepath.Join(dir, fmt.Sprintf("%04d %s", i, names[i%len(names)])), int64(i*37%5000), int64(i))
	}
	writeRandom(t, filepath.Join(root, "empty"), 0, 0)
	_ = os.MkdirAll(filepath.Join(root, "empty dir"), 0o755)
	_ = os.Symlink("plain target.txt", filepath.Join(root, "link to ✓"))
	writeRandom(t, filepath.Join(root, "plain target.txt"), 100, 99)
	return tree(t, root)
}

// ---- upload client (what simple-uploader.js sends) ----

type uploadOpts struct {
	chunk      int64
	parallel   int
	identifier string
	skip       map[int64]bool // chunks not to send (interrupted upload)
	only       map[int64]bool // send only these
}

func chunkCount(size, chunk int64) int64 {
	n := (size + chunk - 1) / chunk
	if n < 1 {
		n = 1
	}
	return n
}

func uploadFields(destDir, rel string, size int64, o uploadOpts, n int64) url.Values {
	total := chunkCount(size, o.chunk)
	cur := o.chunk
	if n == total {
		cur = size - (total-1)*o.chunk
	}
	return url.Values{
		"chunkNumber": {strconv.FormatInt(n, 10)}, "chunkSize": {strconv.FormatInt(o.chunk, 10)},
		"currentChunkSize": {strconv.FormatInt(cur, 10)}, "totalSize": {strconv.FormatInt(size, 10)},
		"identifier": {o.identifier}, "filename": {filepath.Base(rel)}, "relativePath": {rel},
		"totalChunks": {strconv.FormatInt(total, 10)}, "path": {destDir},
	}
}

// hasChunk is the uploader's testChunks GET: true = server already has it.
func hasChunk(t *testing.T, destDir, rel string, size int64, o uploadOpts, n int64) bool {
	t.Helper()
	resp, err := http.Get(core.base + "/v2/casaos/file/upload?" + uploadFields(destDir, rel, size, o, n).Encode())
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

// postChunk sends one chunk; returns HTTP status and the "complete" flag.
func postChunk(t *testing.T, destDir, rel string, src io.ReaderAt, size int64, o uploadOpts, n int64) (int, bool, string) {
	t.Helper()
	f := uploadFields(destDir, rel, size, o, n)
	cur, _ := strconv.ParseInt(f.Get("currentChunkSize"), 10, 64)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range f {
		_ = mw.WriteField(k, v[0])
	}
	fw, _ := mw.CreateFormFile("file", filepath.Base(rel))
	if _, err := io.Copy(fw, io.NewSectionReader(src, (n-1)*o.chunk, cur)); err != nil {
		t.Fatal(err)
	}
	mw.Close()
	resp, err := http.Post(core.base+"/v2/casaos/file/upload", mw.FormDataContentType(), &body)
	if err != nil {
		return 0, false, err.Error()
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var r struct {
		Complete bool   `json:"complete"`
		Message  string `json:"message"`
	}
	_ = json.Unmarshal(raw, &r)
	return resp.StatusCode, r.Complete, r.Message
}

// upload sends a file the way the web uploader does (test chunk, then
// send, `parallel` at a time, chunk order shuffled). Returns the failure
// the uploader would show, or "" once every chunk got a 2xx - which is
// all the browser checks before it says "Done".
func upload(t *testing.T, destDir, rel string, src io.ReaderAt, size int64, o uploadOpts) string {
	t.Helper()
	if o.chunk == 0 {
		o.chunk = 8 << 20
	}
	if o.parallel == 0 {
		o.parallel = 3
	}
	if o.identifier == "" {
		o.identifier = fmt.Sprintf("%d-%s", size, strings.ReplaceAll(rel, "/", ""))
	}
	total := chunkCount(size, o.chunk)
	order := rand.Perm(int(total))
	jobs := make(chan int64)
	var mu sync.Mutex
	var failure string
	var wg sync.WaitGroup
	for w := 0; w < o.parallel; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := range jobs {
				mu.Lock()
				stop := failure != "" // the browser aborts the file's other chunks
				mu.Unlock()
				if stop {
					continue
				}
				if hasChunk(t, destDir, rel, size, o, n) {
					continue
				}
				var status int
				var msg string
				for try := 0; try < 5; try++ { // maxChunkRetries: 4
					status, _, msg = postChunk(t, destDir, rel, src, size, o, n)
					if status/100 == 2 || status == 400 || status == 409 {
						break
					}
				}
				if status/100 != 2 {
					mu.Lock()
					failure = fmt.Sprintf("chunk %d: %d %s", n, status, msg)
					mu.Unlock()
				}
			}
		}()
	}
	for _, i := range order {
		n := int64(i + 1)
		if o.skip[n] || (o.only != nil && !o.only[n]) {
			continue
		}
		jobs <- n
	}
	close(jobs)
	wg.Wait()
	return failure
}

func uploadFile(t *testing.T, destDir, rel, local string, o uploadOpts) string {
	t.Helper()
	f, err := os.Open(local)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	st, _ := f.Stat()
	return upload(t, destDir, rel, f, st.Size(), o)
}

func mkdirs(t *testing.T, ps ...string) {
	for _, p := range ps {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func largeSize() int64 {
	if v, err := strconv.ParseInt(os.Getenv("NV_E2E_LARGE_MB"), 10, 64); err == nil && v > 0 {
		return v << 20
	}
	return 3 << 30
}

// =================== uploads ===================

func TestUploadSmallAndOddNames(t *testing.T) {
	dest := filepath.Join(diskVol, "up-small")
	mkdirs(t, dest)
	for i, name := range []string{"a.txt", "with space.txt", "ünïcödé ✓.txt", "日本語.txt", "emoji 🎉.txt", "#hash&amp%20.txt", "empty.txt"} {
		src := filepath.Join(work, "src-small", name)
		size := int64(i * 1000)
		if name == "empty.txt" {
			size = 0
		}
		want := writeRandom(t, src, size, int64(i))
		if msg := uploadFile(t, dest, name, src, uploadOpts{}); msg != "" {
			t.Fatalf("%s: %s", name, msg)
		}
		if got := sum(t, filepath.Join(dest, name)); got != want {
			t.Errorf("%s: checksum mismatch", name)
		}
	}
	noLeftovers(t, dest)
}

func TestUploadLargeSparse(t *testing.T) {
	size := largeSize()
	src := filepath.Join(diskVol, "src-large.img")
	writeSparse(t, src, size)
	want := sum(t, src)
	dest := filepath.Join(diskVol, "up-large")
	mkdirs(t, dest)
	start := time.Now()
	if msg := uploadFile(t, dest, "large.img", src, uploadOpts{}); msg != "" {
		t.Fatal(msg)
	}
	t.Logf("uploaded %d MiB in %s", size>>20, time.Since(start).Round(time.Millisecond))
	up := filepath.Join(dest, "large.img")
	if got := sum(t, up); got != want {
		t.Fatal("large upload checksum mismatch")
	}
	noLeftovers(t, dest)
	_ = os.Remove(src)

	// Download it back (streamed, hashed on the fly).
	resp, err := http.Get(core.base + "/v1/file?path=" + url.QueryEscape(up))
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	n, _ := io.Copy(h, resp.Body)
	resp.Body.Close()
	if n != size || hex.EncodeToString(h.Sum(nil)) != want {
		t.Fatalf("large download: %d of %d bytes, checksum ok=%v", n, size, hex.EncodeToString(h.Sum(nil)) == want)
	}

	// Copy it through the engine; progress must account for every byte.
	cp := filepath.Join(diskVol, "large-copy")
	mkdirs(t, cp)
	j := waitJob(t, submit(t, "copy", []string{up}, cp, ""), 10*time.Minute)
	if j.State != "done" || j.BytesDone != size {
		t.Fatalf("large copy: %s %d/%d", j.State, j.BytesDone, size)
	}
	if sum(t, filepath.Join(cp, "large.img")) != want {
		t.Fatal("large copy checksum mismatch")
	}
	_ = os.RemoveAll(cp)
	_ = os.Remove(up)
}

func TestUploadFolderManySmallFiles(t *testing.T) {
	src := filepath.Join(work, "src-many")
	want := map[string]string{}
	for i := 0; i < 600; i++ {
		rel := filepath.Join("my folder", fmt.Sprintf("sub %d", i%9), fmt.Sprintf("f %d ✓.txt", i))
		want[rel] = writeRandom(t, filepath.Join(src, rel), int64(i%3000), int64(i))
	}
	dest := filepath.Join(ramVol, "up-many")
	mkdirs(t, dest)
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for rel := range want {
		wg.Add(1)
		sem <- struct{}{}
		go func(rel string) {
			defer func() { <-sem; wg.Done() }()
			if msg := uploadFile(t, dest, rel, filepath.Join(src, rel), uploadOpts{}); msg != "" {
				t.Errorf("%s: %s", rel, msg)
			}
		}(rel)
	}
	wg.Wait()
	got := tree(t, dest)
	for rel, s := range want {
		if got[rel] != s {
			t.Errorf("%s: missing or wrong", rel)
		}
	}
	noLeftovers(t, dest)
}

// Two different files with the same name uploaded into two folders at the
// same time (same browser identifier) must not mix.
func TestUploadSameNameDifferentFolders(t *testing.T) {
	a, b := filepath.Join(diskVol, "same-a"), filepath.Join(diskVol, "same-b")
	mkdirs(t, a, b)
	sa := writeRandom(t, filepath.Join(work, "same1"), 20<<20, 1)
	sb := writeRandom(t, filepath.Join(work, "same2"), 20<<20, 2)
	var wg sync.WaitGroup
	for _, x := range [][2]string{{a, "same1"}, {b, "same2"}} {
		wg.Add(1)
		go func(dir, src string) {
			defer wg.Done()
			if msg := uploadFile(t, dir, "photo.jpg", filepath.Join(work, src), uploadOpts{identifier: "20971520-photojpg"}); msg != "" {
				t.Error(msg)
			}
		}(x[0], x[1])
	}
	wg.Wait()
	if sum(t, filepath.Join(a, "photo.jpg")) != sa || sum(t, filepath.Join(b, "photo.jpg")) != sb {
		t.Fatal("same-name uploads mixed up")
	}
}

// The browser loses its connection half way, the server restarts (deploy,
// crash), then the uploader resumes: it asks for each missing chunk and
// sends only those. The file must be complete - and never reported done
// while it isn't there.
func TestUploadResumeAcrossRestart(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGKILL, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			src := filepath.Join(work, "resume-src")
			want := writeRandom(t, src, 50<<20+123, int64(sig))
			dest := filepath.Join(diskVol, "up-resume-"+strconv.Itoa(int(sig)))
			mkdirs(t, dest)
			o := uploadOpts{chunk: 4 << 20}
			total := chunkCount(50<<20+123, o.chunk)
			first := map[int64]bool{}
			for n := int64(1); n <= total/2; n++ {
				first[n] = true
			}
			o.only = first
			if msg := uploadFile(t, dest, "resume.bin", src, o); msg != "" {
				t.Fatal(msg)
			}
			restartCore(t, sig)
			// The browser still knows chunks 1..n/2 were acknowledged and
			// only sends the rest.
			o.only, o.skip = nil, first
			if msg := uploadFile(t, dest, "resume.bin", src, o); msg != "" {
				t.Fatal(msg)
			}
			got := filepath.Join(dest, "resume.bin")
			if _, err := os.Stat(got); err != nil {
				t.Fatalf("uploader reported success but the file isn't there: %v", err)
			}
			if sum(t, got) != want {
				t.Fatal("resumed upload checksum mismatch")
			}
			noLeftovers(t, dest)
		})
	}
}

// Cancel in the upload tray: the half-sent file's temp goes right away,
// not after the 6-hour idle cleanup.
func TestUploadCancelRemovesTemp(t *testing.T) {
	src := filepath.Join(work, "cancel-src")
	size := int64(10 << 20)
	writeRandom(t, src, size, 5)
	dest := filepath.Join(diskVol, "up-cancel")
	mkdirs(t, dest)
	o := uploadOpts{chunk: 4 << 20, identifier: "cancel", only: map[int64]bool{1: true}}
	if msg := uploadFile(t, dest, "c.bin", src, o); msg != "" {
		t.Fatal(msg)
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range uploadFields(dest, "c.bin", size, o, 1) {
		_ = mw.WriteField(k, v[0])
	}
	_ = mw.WriteField("cancel", "1")
	mw.Close()
	resp, err := http.Post(core.base+"/v2/casaos/file/upload", mw.FormDataContentType(), &body)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("cancel: %d", resp.StatusCode)
	}
	noLeftovers(t, dest)
	if _, err := os.Stat(filepath.Join(dest, "c.bin")); err == nil {
		t.Fatal("cancelled upload appeared")
	}
}

// A chunk whose request dies half way (closed tab, dropped Wi-Fi) is
// retried; the result must be byte-identical.
func TestUploadTruncatedChunkRetried(t *testing.T) {
	src := filepath.Join(work, "trunc-src")
	size := int64(10<<20 + 7)
	want := writeRandom(t, src, size, 7)
	dest := filepath.Join(diskVol, "up-trunc")
	mkdirs(t, dest)
	o := uploadOpts{chunk: 4 << 20, identifier: "trunc"}
	// Chunk 2 arrives short (the server must refuse it, not count it).
	f := uploadFields(dest, "t.bin", size, o, 2)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range f {
		_ = mw.WriteField(k, v[0])
	}
	fw, _ := mw.CreateFormFile("file", "t.bin")
	fw.Write(make([]byte, 1000))
	mw.Close()
	resp, err := http.Post(core.base+"/v2/casaos/file/upload", mw.FormDataContentType(), &body)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode/100 == 2 {
		t.Fatalf("short chunk accepted: %d", resp.StatusCode)
	}
	if hasChunk(t, dest, "t.bin", size, o, 2) {
		t.Fatal("short chunk counted as received")
	}
	if msg := uploadFile(t, dest, "t.bin", src, o); msg != "" {
		t.Fatal(msg)
	}
	if sum(t, filepath.Join(dest, "t.bin")) != want {
		t.Fatal("checksum mismatch after retry")
	}
}

// Upload onto a full disk: the uploader must get an error (not "Done"),
// no partial file under the real name, and a retry after freeing space
// must work.
func TestUploadDiskFull(t *testing.T) {
	src := filepath.Join(work, "full-src")
	size := int64(6 << 20) // the volume is 4 MiB
	want := writeRandom(t, src, size, 11)
	dest := filepath.Join(fullVol, "up")
	mkdirs(t, dest)
	msg := uploadFile(t, dest, "big.bin", src, uploadOpts{chunk: 1 << 20, parallel: 1})
	if msg == "" {
		t.Fatal("upload onto a full disk reported success")
	}
	if _, err := os.Stat(filepath.Join(dest, "big.bin")); err == nil {
		t.Fatal("partial upload visible under the real name")
	}
	t.Logf("full-disk upload error shown to the user: %s", msg)
	// Retrying the same file into a folder with room works.
	room := filepath.Join(ramVol, "up-after-full")
	mkdirs(t, room)
	if m := uploadFile(t, room, "big.bin", src, uploadOpts{chunk: 1 << 20}); m != "" {
		t.Fatal(m)
	}
	if sum(t, filepath.Join(room, "big.bin")) != want {
		t.Fatal("checksum mismatch")
	}
	// The failed upload's temp file must not keep the small volume full.
	var st syscall.Statfs_t
	_ = syscall.Statfs(fullVol, &st)
	if free := int64(st.Bavail) * st.Bsize; free < 3<<20 {
		t.Errorf("failed upload left the disk full (%d bytes free)", free)
	}
}

func TestUploadPermissionDenied(t *testing.T) {
	dest := filepath.Join(diskVol, "readonly-up")
	mkdirs(t, dest)
	src := filepath.Join(work, "perm-src")
	writeRandom(t, src, 1000, 5)
	ro(t, dest)
	if msg := uploadFile(t, dest, "x.bin", src, uploadOpts{}); msg == "" {
		t.Fatal("upload into a read-only folder reported success")
	}
}

func chattr(dir string, on bool) error {
	flag := "-i"
	if on {
		flag = "+i"
	}
	if out, err := exec.Command("chattr", flag, dir).CombinedOutput(); err != nil {
		return fmt.Errorf("%v: %s", err, out)
	}
	return nil
}

// ro makes dir read-only for the core. The core runs as root, so file
// modes don't stop it - an immutable attribute does (ext4).
func ro(t *testing.T, dir string) {
	t.Helper()
	if err := chattr(dir, true); err != nil {
		t.Skipf("can't make a read-only folder here: %v", err)
	}
	t.Cleanup(func() { _ = chattr(dir, false) })
}

// =================== downloads ===================

func TestDownloadFullAndResumed(t *testing.T) {
	p := filepath.Join(diskVol, "dl", "ünï code ✓ file.bin")
	size := int64(30<<20 + 5)
	want := writeRandom(t, p, size, 3)
	get := func(rng, enc string) *http.Response {
		req, _ := http.NewRequest("GET", core.base+"/v1/file?path="+url.QueryEscape(p), nil)
		if rng != "" {
			req.Header.Set("Range", rng)
		}
		// What browsers send; Go would otherwise add gzip and decode it.
		req.Header.Set("Accept-Encoding", enc)
		resp, err := http.DefaultTransport.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	// Full download, as a browser asks for it: needs a Content-Length (the
	// download shows its size/progress, and can be resumed) and the bytes.
	resp := get("", "gzip, deflate, br")
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if resp.Header.Get("Content-Encoding") != "" || resp.Header.Get("Content-Length") != strconv.FormatInt(size, 10) {
		t.Errorf("download is %q-encoded with Content-Length %q: the browser can't show progress or resume it", resp.Header.Get("Content-Encoding"), resp.Header.Get("Content-Length"))
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, url.PathEscape(filepath.Base(p))) {
		t.Errorf("Content-Disposition %q lost the name", cd)
	}
	if int64(len(body)) == size {
		h := sha256.Sum256(body)
		if hex.EncodeToString(h[:]) != want {
			t.Error("download checksum mismatch")
		}
	}
	// Interrupted at 11 MB, resumed with Range.
	cut := int64(11<<20 + 3)
	resp = get(fmt.Sprintf("bytes=%d-", cut), "identity")
	rest, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 206 {
		t.Fatalf("range: status %d", resp.StatusCode)
	}
	resp = get(fmt.Sprintf("bytes=0-%d", cut-1), "identity")
	head, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	h := sha256.Sum256(append(head, rest...))
	if hex.EncodeToString(h[:]) != want {
		t.Fatal("resumed download checksum mismatch")
	}
	// A resume from the browser keeps Accept-Encoding: the range must still
	// be the raw bytes at that offset.
	resp = get(fmt.Sprintf("bytes=%d-", cut), "gzip, deflate, br")
	if resp.StatusCode != 206 || resp.Header.Get("Content-Encoding") != "" {
		t.Errorf("browser resume: status %d encoding %q", resp.StatusCode, resp.Header.Get("Content-Encoding"))
	}
	resp.Body.Close()
}

func TestDownloadFolderZip(t *testing.T) {
	root := filepath.Join(diskVol, "dlzip", "Zip me ✓")
	want := makeTree(t, root, 40)
	r := call(t, "POST", "/v1/batch/download", map[string]interface{}{"files": []string{root}, "format": "zip"})
	var ticket string
	_ = json.Unmarshal(r.Data, &ticket)
	resp, err := http.Get(core.base + "/v1/batch?ticket=" + ticket)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("not a zip (%d bytes, status %d): %v", len(raw), resp.StatusCode, err)
	}
	got := map[string]string{}
	for _, f := range zr.File {
		name := strings.TrimSuffix(strings.TrimPrefix(f.Name, "Zip me ✓/"), "/")
		if name == "" || name == "Zip me ✓" {
			continue
		}
		if f.FileInfo().IsDir() {
			got[name] = "dir"
			continue
		}
		rc, _ := f.Open()
		h := sha256.New()
		io.Copy(h, rc)
		rc.Close()
		got[name] = hex.EncodeToString(h.Sum(nil))
	}
	// Symlinks are left out on purpose (they could point anywhere) - and
	// listed in DOWNLOAD-ERRORS.txt, never silently.
	for k, v := range want {
		if strings.HasPrefix(v, "link:") {
			delete(want, k)
		}
	}
	if _, ok := got["DOWNLOAD-ERRORS.txt"]; !ok {
		t.Error("skipped symlink not reported inside the zip")
	}
	delete(got, "DOWNLOAD-ERRORS.txt")
	sameTree(t, want, got, "zip")
}

// =================== copy / move / rename / delete ===================

func TestCopyTreeSameAndCrossVolume(t *testing.T) {
	src := filepath.Join(diskVol, "copy-src", "Tree ✓")
	want := makeTree(t, src, 300)
	for _, dest := range []string{filepath.Join(diskVol, "copy-dst"), filepath.Join(ramVol, "copy-dst")} {
		mkdirs(t, dest)
		j := waitJob(t, submit(t, "copy", []string{src}, dest, ""), time.Minute)
		if j.State != "done" {
			t.Fatalf("copy to %s: %s %+v", dest, j.State, j.Failures)
		}
		sameTree(t, want, tree(t, filepath.Join(dest, "Tree ✓")), "copy to "+dest)
		noLeftovers(t, dest)
	}
	sameTree(t, want, tree(t, src), "source after copy")
}

func TestMoveCrossVolumeAndBack(t *testing.T) {
	src := filepath.Join(diskVol, "move-src", "Mover ✓")
	want := makeTree(t, src, 200)
	there := filepath.Join(ramVol, "move-dst")
	mkdirs(t, there)
	j := waitJob(t, submit(t, "move", []string{src}, there, ""), time.Minute)
	if j.State != "done" {
		t.Fatalf("move: %s %+v", j.State, j.Failures)
	}
	if _, err := os.Lstat(src); !os.IsNotExist(err) {
		t.Fatalf("source still there after move: %v", err)
	}
	sameTree(t, want, tree(t, filepath.Join(there, "Mover ✓")), "moved")
	// And back, same volume rename path on the way.
	back := filepath.Join(diskVol, "move-back")
	mkdirs(t, back)
	j = waitJob(t, submit(t, "move", []string{filepath.Join(there, "Mover ✓")}, back, ""), time.Minute)
	if j.State != "done" {
		t.Fatalf("move back: %s %+v", j.State, j.Failures)
	}
	sameTree(t, want, tree(t, filepath.Join(back, "Mover ✓")), "moved back")
	j = waitJob(t, submit(t, "move", []string{filepath.Join(back, "Mover ✓")}, diskVol, ""), time.Minute)
	if j.State != "done" {
		t.Fatalf("same-volume move: %s", j.State)
	}
	sameTree(t, want, tree(t, filepath.Join(diskVol, "Mover ✓")), "same-volume move")
}

// Cross-volume move where one file can't be read: it must stay at the
// source, everything else moves, and the job says what failed.
func TestMoveCrossVolumeWithUnreadableFile(t *testing.T) {
	src := filepath.Join(diskVol, "move-partial", "P")
	makeTree(t, src, 20)
	bad := filepath.Join(src, "locked.bin")
	writeRandom(t, bad, 5000, 1)
	// Root reads everything, so the item that can't be copied is a FIFO
	// (special file): it must be reported, never silently dropped.
	if err := syscall.Mkfifo(filepath.Join(src, "pipe"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(ramVol, "move-partial-dst")
	mkdirs(t, dest)
	j := waitJob(t, submit(t, "move", []string{src}, dest, ""), time.Minute)
	if j.State != "done_with_errors" || j.FilesFailed != 1 {
		t.Fatalf("want done_with_errors with 1 failure, got %s %d %+v", j.State, j.FilesFailed, j.Failures)
	}
	if _, err := os.Lstat(filepath.Join(src, "pipe")); err != nil {
		t.Fatal("the failed item was removed from the source")
	}
	if _, err := os.Stat(filepath.Join(dest, "P", "locked.bin")); err != nil {
		t.Fatal("good files didn't move")
	}
	if _, err := os.Stat(bad); !os.IsNotExist(err) {
		t.Fatal("a moved file is still at the source")
	}
}

func TestConflictPolicies(t *testing.T) {
	dir := filepath.Join(diskVol, "conflict")
	src := filepath.Join(dir, "src", "doc.txt")
	writeRandom(t, src, 100, 1)
	dest := filepath.Join(dir, "dst")
	old := writeRandom(t, filepath.Join(dest, "doc.txt"), 50, 2)
	waitJob(t, submit(t, "copy", []string{src}, dest, "skip"), time.Minute)
	if sum(t, filepath.Join(dest, "doc.txt")) != old {
		t.Error("skip overwrote")
	}
	waitJob(t, submit(t, "copy", []string{src}, dest, "rename"), time.Minute)
	if sum(t, filepath.Join(dest, "doc (2).txt")) != sum(t, src) || sum(t, filepath.Join(dest, "doc.txt")) != old {
		t.Error("keep both")
	}
	waitJob(t, submit(t, "copy", []string{src}, dest, "overwrite"), time.Minute)
	if sum(t, filepath.Join(dest, "doc.txt")) != sum(t, src) {
		t.Error("overwrite")
	}
	// Paste into the same folder = duplicate ("doc (2)" exists -> "doc (3)").
	waitJob(t, submit(t, "copy", []string{filepath.Join(dest, "doc.txt")}, dest, ""), time.Minute)
	if sum(t, filepath.Join(dest, "doc (3).txt")) != sum(t, src) {
		t.Error("duplicate in place")
	}
}

// Two transfers into the same folder at once.
func TestConcurrentTransfersSameTarget(t *testing.T) {
	dir := filepath.Join(diskVol, "concurrent")
	a := filepath.Join(dir, "a", "same.bin")
	b := filepath.Join(dir, "b", "same.bin")
	sa := writeRandom(t, a, 40<<20, 1)
	sb := writeRandom(t, b, 40<<20, 2)
	dest := filepath.Join(dir, "dst")
	writeRandom(t, filepath.Join(dest, "same.bin"), 10, 3)

	// Keep both, twice at once: both copies must survive under distinct names.
	j1 := submit(t, "copy", []string{a}, dest, "rename")
	j2 := submit(t, "copy", []string{b}, dest, "rename")
	waitJob(t, j1, time.Minute)
	waitJob(t, j2, time.Minute)
	got := map[string]bool{}
	for _, n := range []string{"same (2).bin", "same (3).bin"} {
		if _, err := os.Stat(filepath.Join(dest, n)); err == nil {
			got[sum(t, filepath.Join(dest, n))] = true
		}
	}
	if !got[sa] || !got[sb] {
		t.Errorf("two concurrent keep-both copies: one was lost (have %d of 2)", len(got))
	}

	// Overwrite, twice at once: the result is one of the two, whole.
	d2 := filepath.Join(dir, "dst2")
	mkdirs(t, d2)
	j1 = submit(t, "copy", []string{a}, d2, "overwrite")
	j2 = submit(t, "copy", []string{b}, d2, "overwrite")
	waitJob(t, j1, time.Minute)
	waitJob(t, j2, time.Minute)
	if s := sum(t, filepath.Join(d2, "same.bin")); s != sa && s != sb {
		t.Error("concurrent overwrites produced a mix")
	}
	noLeftovers(t, dir)

	// Same folder copied into the same place twice at once (double paste).
	tr := filepath.Join(dir, "T")
	want := makeTree(t, tr, 50)
	d3 := filepath.Join(dir, "dst3")
	mkdirs(t, d3)
	j1 = submit(t, "copy", []string{tr}, d3, "rename")
	j2 = submit(t, "copy", []string{tr}, d3, "rename")
	waitJob(t, j1, time.Minute)
	waitJob(t, j2, time.Minute)
	sameTree(t, want, tree(t, filepath.Join(d3, "T")), "first paste")
	if _, err := os.Stat(filepath.Join(d3, "T (2)")); err != nil {
		t.Errorf("second paste of a folder merged into the first instead of keeping both")
	} else {
		sameTree(t, want, tree(t, filepath.Join(d3, "T (2)")), "second paste")
	}
}

// Cancelling mid-copy: the job ends as cancelled (never stuck), nothing
// half-written under a real name, the source untouched.
func TestCancelMidTransfer(t *testing.T) {
	src := filepath.Join(diskVol, "cancel", "big.bin")
	want := writeRandom(t, src, 600<<20, 1)
	for _, kind := range []string{"copy", "move"} {
		dest := filepath.Join(ramVol, "cancel-"+kind)
		mkdirs(t, dest)
		id := submit(t, kind, []string{src}, dest, "")
		deadline := time.Now().Add(30 * time.Second)
		for {
			j, _ := getJob(t, id)
			if j.BytesDone > 0 || terminal[j.State] || time.Now().After(deadline) {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if r := call(t, "DELETE", "/v1/batch/"+id+"/task", nil); r.Success != 200 {
			t.Fatalf("cancel: %+v", r)
		}
		j := waitJob(t, id, 30*time.Second)
		if j.State != "cancelled" {
			t.Fatalf("%s: after cancel state %s", kind, j.State)
		}
		if _, err := os.Stat(filepath.Join(dest, "big.bin")); err == nil {
			t.Errorf("%s: cancelled copy left a file under the real name", kind)
		}
		noLeftovers(t, dest)
		if sum(t, src) != want {
			t.Fatalf("%s: source damaged by cancel", kind)
		}
	}
}

// A job running when the service dies shows as interrupted after restart,
// and Retry finishes it.
func TestInterruptedJobRetry(t *testing.T) {
	src := filepath.Join(diskVol, "interrupt", "T")
	want := makeTree(t, src, 50)
	writeRandom(t, filepath.Join(src, "zz big.bin"), 800<<20, 9)
	want = tree(t, src)
	dest := filepath.Join(ramVol, "interrupt-dst")
	mkdirs(t, dest)
	id := submit(t, "move", []string{src}, dest, "")
	deadline := time.Now().Add(30 * time.Second)
	for {
		j, _ := getJob(t, id)
		if j.BytesDone > 0 && j.State == "running" || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	restartCore(t, syscall.SIGKILL)
	j, ok := getJob(t, id)
	if !ok || j.State != "interrupted" {
		t.Fatalf("after a crash the job shows %q (want interrupted)", j.State)
	}
	r := call(t, "POST", "/v1/batch/"+id+"/retry", nil)
	var nj jobEvent
	_ = json.Unmarshal(r.Data, &nj)
	j = waitJob(t, nj.ID, 2*time.Minute)
	if j.State != "done" {
		t.Fatalf("retry: %s %+v", j.State, j.Failures)
	}
	sameTree(t, want, tree(t, filepath.Join(dest, "T")), "after retry")
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		left := tree(t, src)
		t.Errorf("move source not emptied after retry: %d entries left", len(left))
	}
	noLeftovers(t, dest)
}

func TestCopyOntoFullDisk(t *testing.T) {
	src := filepath.Join(diskVol, "tofull")
	writeRandom(t, filepath.Join(src, "small.txt"), 1000, 1)
	writeRandom(t, filepath.Join(src, "huge.bin"), 8<<20, 2)
	dest := filepath.Join(fullVol, "copy")
	mkdirs(t, dest)
	j := waitJob(t, submit(t, "copy", []string{src}, dest, ""), time.Minute)
	if j.State == "done" {
		t.Fatal("copy onto a full disk reported done")
	}
	if len(j.Failures) == 0 || !strings.Contains(j.Failures[0].Error, "space") {
		t.Errorf("failure doesn't say the disk is full: %+v", j.Failures)
	}
	if _, err := os.Stat(filepath.Join(dest, "tofull", "huge.bin")); err == nil {
		t.Error("partial file under the real name")
	}
	noLeftovers(t, dest)
	// Move onto a full disk: nothing may be lost.
	j = waitJob(t, submit(t, "move", []string{src}, dest, "overwrite"), time.Minute)
	if _, err := os.Stat(filepath.Join(src, "huge.bin")); err != nil {
		t.Fatal("move onto full disk deleted the source of a file that didn't arrive")
	}
	_ = os.RemoveAll(dest)
}

func TestCopyIntoReadOnlyFolder(t *testing.T) {
	src := filepath.Join(diskVol, "ro-src.txt")
	writeRandom(t, src, 100, 1)
	dest := filepath.Join(diskVol, "ro-dst")
	mkdirs(t, dest)
	ro(t, dest)
	j := waitJob(t, submit(t, "copy", []string{src}, dest, ""), time.Minute)
	if j.State == "done" {
		t.Fatal("copy into a read-only folder reported done")
	}
	j = waitJob(t, submit(t, "move", []string{src}, dest, ""), time.Minute)
	if _, err := os.Stat(src); err != nil {
		t.Fatal("failed move lost the source")
	}
}

// Names up to the filesystem's 255-byte limit must copy, move and upload.
// (Backup's encrypted folders use long base32 names like these; so do
// long unicode titles: 85 CJK characters are 255 bytes.)
func TestLongNames(t *testing.T) {
	long := strings.Repeat("日", 85)                                                      // 255 bytes
	crypt := strings.Repeat("v", 200) + "0nhrp4bqdbgqoe5mhgj1fb2kn0po52hk7qlk1aanrlhl3i" // rclone crypt style, 246 bytes
	src := filepath.Join(diskVol, "long-src")
	want := map[string]string{}
	for _, n := range []string{long, crypt} {
		want[n] = writeRandom(t, filepath.Join(src, n), 3000, 1)
	}
	want["sub"] = "dir"
	want[filepath.Join("sub", long)] = writeRandom(t, filepath.Join(src, "sub", long), 10, 2)
	for _, dest := range []string{filepath.Join(diskVol, "long-dst"), filepath.Join(ramVol, "long-dst")} {
		mkdirs(t, dest)
		j := waitJob(t, submit(t, "copy", []string{src}, dest, ""), time.Minute)
		if j.State != "done" {
			t.Errorf("copy of long names to %s: %s %+v", dest, j.State, j.Failures)
			continue
		}
		sameTree(t, want, tree(t, filepath.Join(dest, "long-src")), "long names")
	}
	up := filepath.Join(diskVol, "long-up")
	mkdirs(t, up)
	if msg := uploadFile(t, up, long, filepath.Join(src, long), uploadOpts{}); msg != "" {
		t.Errorf("upload of a 255-byte name: %s", msg)
	}
	// Into/out of a folder full of encrypted (crypt) names, cross volume.
	j := waitJob(t, submit(t, "move", []string{filepath.Join(src, crypt)}, ramVol, ""), time.Minute)
	if j.State != "done" || sum(t, filepath.Join(ramVol, crypt)) != want[crypt] {
		t.Errorf("move of a crypt-named file: %s %+v", j.State, j.Failures)
	}
}

func TestSymlinks(t *testing.T) {
	dir := filepath.Join(diskVol, "links")
	writeRandom(t, filepath.Join(dir, "target.txt"), 100, 1)
	_ = os.Symlink("target.txt", filepath.Join(dir, "rel-link"))
	_ = os.Symlink("/nonexistent/dangling", filepath.Join(dir, "dangling"))
	_ = os.Symlink(".", filepath.Join(dir, "loop"))
	want := tree(t, dir)
	for _, dest := range []string{filepath.Join(diskVol, "links-dst"), filepath.Join(ramVol, "links-dst")} {
		mkdirs(t, dest)
		j := waitJob(t, submit(t, "copy", []string{dir}, dest, ""), time.Minute)
		if j.State != "done" {
			t.Fatalf("copy with symlinks to %s: %s %+v", dest, j.State, j.Failures)
		}
		sameTree(t, want, tree(t, filepath.Join(dest, "links")), "symlinks")
	}
	// Copying a symlink to a folder copies the link, not the folder.
	j := waitJob(t, submit(t, "copy", []string{filepath.Join(dir, "loop")}, filepath.Join(ramVol, "links-dst"), ""), time.Minute)
	if j.State == "failed" {
		t.Fatalf("copy of a link to a folder: %+v", j)
	}
}

func TestRename(t *testing.T) {
	dir := filepath.Join(diskVol, "rename")
	a := filepath.Join(dir, "a.txt")
	s := writeRandom(t, a, 100, 1)
	writeRandom(t, filepath.Join(dir, "taken.txt"), 10, 2)
	ok := func(old, nw string) result {
		return call(t, "PUT", "/v1/file/name", map[string]string{"old_path": old, "new_path": nw})
	}
	if r := ok(a, filepath.Join(dir, "ünï ✓ b.txt")); r.Success != 200 {
		t.Fatalf("rename: %+v", r)
	}
	if sum(t, filepath.Join(dir, "ünï ✓ b.txt")) != s {
		t.Fatal("rename lost content")
	}
	if r := ok(filepath.Join(dir, "ünï ✓ b.txt"), filepath.Join(dir, "taken.txt")); r.Success == 200 {
		t.Fatal("rename onto an existing name succeeded (overwrote it)")
	}
	_ = os.Symlink("/nonexistent", filepath.Join(dir, "dangling"))
	if r := ok(filepath.Join(dir, "ünï ✓ b.txt"), filepath.Join(dir, "dangling")); r.Success == 200 {
		t.Error("rename onto a dangling symlink's name replaced the link")
	}
	if r := ok(filepath.Join(dir, "missing"), filepath.Join(dir, "x")); r.Success == 200 {
		t.Error("rename of a missing file reported success")
	} else if r.Message == "" {
		t.Error("rename failure has no message")
	}
	sub := filepath.Join(dir, "folder")
	mkdirs(t, sub)
	ro(t, dir)
	if r := ok(sub, filepath.Join(dir, "folder2")); r.Success == 200 {
		t.Error("rename in a read-only folder reported success")
	} else if !strings.Contains(strings.ToLower(r.Message+string(r.Data)), "permi") && !strings.Contains(strings.ToLower(r.Message+string(r.Data)), "not permitted") {
		t.Errorf("rename failure doesn't say why: %q %s", r.Message, r.Data)
	}
}

func TestDelete(t *testing.T) {
	dir := filepath.Join(diskVol, "del") // tmpfs has no Trash by design
	makeTree(t, filepath.Join(dir, "T"), 30)
	writeRandom(t, filepath.Join(dir, "one ✓.txt"), 10, 1)
	// To the Trash (default), then restore, then delete permanently.
	r := call(t, "DELETE", "/v1/file", []string{filepath.Join(dir, "T"), filepath.Join(dir, "one ✓.txt")})
	if r.Success != 200 {
		t.Fatalf("delete to trash: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(dir, "T")); !os.IsNotExist(err) {
		t.Fatal("still there after delete")
	}
	var d struct {
		Trashed []struct {
			ID string `json:"id"`
		} `json:"trashed"`
	}
	_ = json.Unmarshal(r.Data, &d)
	if len(d.Trashed) != 2 {
		t.Fatalf("trashed %d items, want 2: %s", len(d.Trashed), r.Data)
	}
	if _, err := os.Stat(filepath.Join(diskVol, ".nivaroos-trash")); err != nil {
		t.Errorf("trash not on the item's own volume: %v", err)
	}
	r = call(t, "DELETE", "/v1/file?permanent=true", []string{filepath.Join(dir, "one ✓.txt")})
	if _, err := os.Stat(filepath.Join(dir, "one ✓.txt")); !os.IsNotExist(err) {
		t.Fatal("permanent delete left the file")
	}
	// A permanent delete that partly fails says so.
	ddir := filepath.Join(diskVol, "del-ro")
	writeRandom(t, filepath.Join(ddir, "inner", "keep.txt"), 10, 1)
	ro(t, filepath.Join(ddir, "inner"))
	r = call(t, "DELETE", "/v1/file?permanent=true", []string{ddir})
	if r.Success == 200 {
		t.Errorf("delete of a folder with an undeletable file reported success: %+v", r)
	}
}
