package main

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func newUploadRequest(t *testing.T, filename string, content []byte) *http.Request {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("iso", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/isos", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

func TestListISOs_EmptyWhenDirMissing(t *testing.T) {
	isos, err := listISOs(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("listISOs: %v", err)
	}
	if len(isos) != 0 {
		t.Fatalf("expected no ISOs, got %+v", isos)
	}
}

func TestListISOs_FiltersToISOFilesOnly(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "debian.iso"), []byte("fake-iso-bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	isos, err := listISOs(dir)
	if err != nil {
		t.Fatalf("listISOs: %v", err)
	}
	if len(isos) != 1 || isos[0].Name != "debian.iso" {
		t.Fatalf("expected only debian.iso, got %+v", isos)
	}
}

func TestUploadISO_WritesFile(t *testing.T) {
	dir := t.TempDir()
	req := newUploadRequest(t, "ubuntu.iso", []byte("fake-iso-bytes"))

	iso, err := uploadISO(dir, req)
	if err != nil {
		t.Fatalf("uploadISO: %v", err)
	}
	if iso.Name != "ubuntu.iso" {
		t.Fatalf("expected name ubuntu.iso, got %q", iso.Name)
	}
	if _, err := os.Stat(filepath.Join(dir, "ubuntu.iso")); err != nil {
		t.Fatalf("expected file on disk: %v", err)
	}
}

func TestUploadISO_RejectsNonISOExtension(t *testing.T) {
	dir := t.TempDir()
	req := newUploadRequest(t, "not-an-iso.txt", []byte("hello"))

	if _, err := uploadISO(dir, req); err == nil {
		t.Fatal("expected an error for a non-.iso upload, got nil")
	}
}

func TestDeleteISO_RemovesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "debian.iso")
	if err := os.WriteFile(path, []byte("fake-iso-bytes"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := deleteISO(dir, "debian.iso"); err != nil {
		t.Fatalf("deleteISO: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected file to be removed, stat err: %v", err)
	}
}

func TestDeleteISO_RejectsNonISOExtension(t *testing.T) {
	dir := t.TempDir()
	if err := deleteISO(dir, "notes.txt"); err == nil {
		t.Fatal("expected an error deleting a non-.iso name, got nil")
	}
}

func TestDeleteISO_ErrorsOnMissingFile(t *testing.T) {
	dir := t.TempDir()
	if err := deleteISO(dir, "does-not-exist.iso"); err == nil {
		t.Fatal("expected an error deleting a nonexistent ISO, got nil")
	}
}

// Regression-style test mirroring TestUploadISO_StripsDirectoryComponentsFromFilename:
// a crafted name with directory components must never escape isoDir.
func TestDeleteISO_StripsDirectoryComponentsFromName(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "evil.iso")
	if err := os.WriteFile(outside, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	// "../../evil.iso" reduces (via filepath.Base) to "evil.iso" inside
	// dir, which doesn't exist there - this must fail as "not found", not
	// silently remove the file living outside dir at `outside`.
	err := deleteISO(dir, "../../evil.iso")
	if err == nil {
		t.Fatal("expected an error - the reduced name doesn't exist inside isoDir")
	}
	if _, statErr := os.Stat(outside); statErr != nil {
		t.Fatalf("expected the file outside isoDir to survive untouched: %v", statErr)
	}
}

func TestUploadISO_StripsDirectoryComponentsFromFilename(t *testing.T) {
	dir := t.TempDir()
	req := newUploadRequest(t, "../../etc/evil.iso", []byte("hello"))

	iso, err := uploadISO(dir, req)
	if err != nil {
		t.Fatalf("uploadISO: %v", err)
	}
	if iso.Name != "evil.iso" {
		t.Fatalf("expected filename to be reduced to evil.iso, got %q", iso.Name)
	}
	if _, err := os.Stat(filepath.Join(dir, "evil.iso")); err != nil {
		t.Fatalf("expected file written inside isoDir: %v", err)
	}
}
