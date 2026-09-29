package secret

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	b, err := LoadOrCreate(filepath.Join(t.TempDir(), "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	for _, pw := range []string{"hunter2", "comma,and:colon", "ünïcode 密码", strings.Repeat("x", 500)} {
		s, err := b.Seal(pw, "o_connections.password")
		if err != nil {
			t.Fatal(err)
		}
		if !IsSealed(s) || strings.Contains(s, pw) {
			t.Fatalf("sealed value %q leaks or isn't marked", s)
		}
		got, err := b.Open(s, "o_connections.password")
		if err != nil || got != pw {
			t.Fatalf("Open = %q, %v; want %q", got, err, pw)
		}
	}
	a, _ := b.Seal("same", "c")
	c, _ := b.Seal("same", "c")
	if a == c {
		t.Fatal("two seals of one value are identical (nonce reused)")
	}
}

func TestEmptyStaysEmptyAndPlaintextPassesThrough(t *testing.T) {
	b, _ := LoadOrCreate(filepath.Join(t.TempDir(), "k"))
	if s, _ := b.Seal("", "c"); s != "" {
		t.Fatalf("empty sealed to %q", s)
	}
	if got, err := b.Open("legacy-plain", "c"); err != nil || got != "legacy-plain" {
		t.Fatalf("legacy plaintext: %q %v", got, err)
	}
}

func TestWrongContextOrKeyOrTamperingFails(t *testing.T) {
	dir := t.TempDir()
	b1, _ := LoadOrCreate(filepath.Join(dir, "a"))
	b2, _ := LoadOrCreate(filepath.Join(dir, "b"))
	s, _ := b1.Seal("pw", "o_connections.password")
	if _, err := b1.Open(s, "other.field"); err == nil {
		t.Fatal("opened under another context")
	}
	if _, err := b2.Open(s, "o_connections.password"); err == nil {
		t.Fatal("opened with another host's key")
	}
	bad := s[:len(s)-2] + "AA"
	if _, err := b1.Open(bad, "o_connections.password"); err == nil {
		t.Fatal("tampered value opened")
	}
}

func TestKeyFileIsCreatedRootOnlyAndReused(t *testing.T) {
	p := filepath.Join(t.TempDir(), "etc", "secret.key")
	b1, err := LoadOrCreate(p)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("key mode %o, want 600", fi.Mode().Perm())
	}
	s, _ := b1.Seal("pw", "c")
	b2, err := LoadOrCreate(p) // second start: same key
	if err != nil {
		t.Fatal(err)
	}
	if got, err := b2.Open(s, "c"); err != nil || got != "pw" {
		t.Fatalf("key not reused: %q %v", got, err)
	}
	// A key someone loosened is tightened again.
	os.Chmod(p, 0o644)
	if _, err := LoadOrCreate(p); err != nil {
		t.Fatal(err)
	}
	fi, _ = os.Stat(p)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("loosened key left at %o", fi.Mode().Perm())
	}
}

func TestLoadNeverCreates(t *testing.T) {
	p := filepath.Join(t.TempDir(), "missing.key")
	if _, err := Load(p); !errors.Is(err, ErrNoKey) {
		t.Fatalf("Load of a missing key: %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("Load created the key")
	}
}

func TestBadKeyFileIsAnError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "k")
	os.WriteFile(p, []byte("not hex"), 0o600)
	if _, err := Load(p); err == nil {
		t.Fatal("garbage key accepted")
	}
	os.WriteFile(p, []byte("abcd"), 0o600)
	if _, err := Load(p); err == nil {
		t.Fatal("short key accepted")
	}
}
