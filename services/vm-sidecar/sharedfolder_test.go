package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSharedFolder_ImageCreationAndCopy(t *testing.T) {
	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "source")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}

	testFile := filepath.Join(sourceDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("offline share content"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	imagePath := filepath.Join(tempDir, "test.img")

	// 1. Create sparse file
	cmd := exec.Command("truncate", "-s", "64M", imagePath)
	if err := cmd.Run(); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	// 2. Format FAT32
	mkfs := exec.Command("mkfs.vfat", "-F", "32", "-n", "NIVAROTEST", imagePath)
	if err := mkfs.Run(); err != nil {
		t.Fatalf("mkfs.vfat: %v", err)
	}

	// 3. Copy files using mcopy
	mcopy := exec.Command("mcopy", "-s", "-o", "-i", imagePath, testFile, "::/")
	if err := mcopy.Run(); err != nil {
		t.Fatalf("mcopy into image: %v", err)
	}

	// 4. Sync back to a new directory
	destDir := filepath.Join(tempDir, "dest")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("mkdir dest: %v", err)
	}

	mcopyBack := exec.Command("mcopy", "-s", "-o", "-i", imagePath, "::/*", destDir+"/")
	if err := mcopyBack.Run(); err != nil {
		t.Fatalf("mcopy back: %v", err)
	}

	syncedFile := filepath.Join(destDir, "test.txt")
	content, err := os.ReadFile(syncedFile)
	if err != nil {
		t.Fatalf("read synced file: %v", err)
	}
	if string(content) != "offline share content" {
		t.Fatalf("expected content %q, got %q", "offline share content", string(content))
	}
}
