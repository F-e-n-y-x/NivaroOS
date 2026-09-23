package service

import (
	"bytes"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func chunkReq(dest, rel, id string, n, chunkSize, total int64, data []byte) UploadChunk {
	totalChunks := (total + chunkSize - 1) / chunkSize
	start := (n - 1) * chunkSize
	end := start + chunkSize
	if n == totalChunks {
		end = total
	}
	return UploadChunk{
		Path: dest, RelativePath: rel, Identifier: id, ChunkNumber: n, ChunkSize: chunkSize,
		CurrentChunkSize: end - start, TotalChunks: totalChunks, TotalSize: total,
		Data: bytes.NewReader(data[start:end]),
	}
}

func randomData(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

func TestUploadChunksInParallelOutOfOrder(t *testing.T) {
	s := NewFileUploadService()
	defer s.Close()
	dest := t.TempDir()
	data := randomData(10*1024*1024 + 777)
	const cs = 1024 * 1024
	total := int64(len(data))
	chunks := (total + cs - 1) / cs
	var wg sync.WaitGroup
	errs := make(chan error, chunks)
	for n := chunks; n >= 1; n-- { // reverse order, all at once
		wg.Add(1)
		go func(n int64) {
			defer wg.Done()
			_, err := s.Upload(chunkReq(dest, "video.mp4", "id-1", n, cs, total, data))
			errs <- err
		}(n)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(filepath.Join(dest, "video.mp4"))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("assembled file wrong (err=%v, len=%d)", err, len(got))
	}
	assertNoUploadTemp(t, dest)
}

func TestUploadDuplicateChunkIsHarmless(t *testing.T) {
	s := NewFileUploadService()
	defer s.Close()
	dest := t.TempDir()
	data := randomData(3 * 1024)
	for _, n := range []int64{1, 1, 2, 3, 2} {
		if _, err := s.Upload(chunkReq(dest, "a.bin", "id", n, 1024, 3*1024, data)); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := os.ReadFile(filepath.Join(dest, "a.bin")); !bytes.Equal(got, data) {
		t.Fatal("content wrong")
	}
}

func TestSameNameUploadsToDifferentFoldersDontMix(t *testing.T) {
	s := NewFileUploadService()
	defer s.Close()
	d1, d2 := t.TempDir(), t.TempDir()
	a, b := randomData(4096), randomData(4096)
	// Same identifier (the browser derives it from size + name) for both.
	for n := int64(1); n <= 4; n++ {
		if _, err := s.Upload(chunkReq(d1, "IMG_0001.jpg", "4096-IMG_0001jpg", n, 1024, 4096, a)); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Upload(chunkReq(d2, "IMG_0001.jpg", "4096-IMG_0001jpg", n, 1024, 4096, b)); err != nil {
			t.Fatal(err)
		}
	}
	g1, _ := os.ReadFile(filepath.Join(d1, "IMG_0001.jpg"))
	g2, _ := os.ReadFile(filepath.Join(d2, "IMG_0001.jpg"))
	if !bytes.Equal(g1, a) || !bytes.Equal(g2, b) {
		t.Fatal("uploads with the same name/size into different folders mixed their data")
	}
}

func TestUploadRejectsPathTraversal(t *testing.T) {
	s := NewFileUploadService()
	defer s.Close()
	dest := t.TempDir()
	for _, rel := range []string{"../evil.sh", "a/../../evil.sh", "/etc/passwd"} {
		_, err := s.Upload(chunkReq(dest, rel, "x", 1, 10, 3, []byte("bad")))
		if err == nil {
			t.Errorf("%q accepted", rel)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dest), "evil.sh")); err == nil {
		t.Fatal("wrote outside the destination")
	}
}

func TestFolderUploadKeepsStructure(t *testing.T) {
	s := NewFileUploadService()
	defer s.Close()
	dest := t.TempDir()
	data := []byte("hello")
	if _, err := s.Upload(chunkReq(dest, "Trip 2024/day 1/beach.jpg", "id", 1, 1024, 5, data)); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dest, "Trip 2024", "day 1", "beach.jpg")); string(got) != "hello" {
		t.Fatal("folder structure not kept")
	}
}

func TestUploadSizeMismatchNeverBecomesAFile(t *testing.T) {
	s := NewFileUploadService()
	defer s.Close()
	dest := t.TempDir()
	req := chunkReq(dest, "short.bin", "id", 1, 1024, 100, randomData(100))
	req.Data = bytes.NewReader(randomData(60)) // client claims 100, sends 60
	if _, err := s.Upload(req); err == nil {
		t.Fatal("a short chunk was accepted")
	}
	if _, err := os.Stat(filepath.Join(dest, "short.bin")); !os.IsNotExist(err) {
		t.Fatal("an incomplete upload appeared under its real name")
	}
}

func TestChunkCheckEnablesResume(t *testing.T) {
	s := NewFileUploadService()
	defer s.Close()
	dest := t.TempDir()
	data := randomData(3 * 1024)
	if _, err := s.Upload(chunkReq(dest, "r.bin", "id", 2, 1024, 3*1024, data)); err != nil {
		t.Fatal(err)
	}
	probe := UploadChunk{Path: dest, RelativePath: "r.bin", Identifier: "id", TotalSize: 3 * 1024}
	probe.ChunkNumber = 2
	if !s.HasChunk(probe) {
		t.Error("received chunk not reported")
	}
	probe.ChunkNumber = 1
	if s.HasChunk(probe) {
		t.Error("missing chunk reported as received")
	}
}

func assertNoUploadTemp(t *testing.T, dir string) {
	t.Helper()
	filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && strings.Contains(filepath.Base(p), ".nvupload-") {
			t.Errorf("leftover upload temp %s", p)
		}
		return nil
	})
}
