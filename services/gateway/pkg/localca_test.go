package pkg

import (
	"crypto/rand"
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLocalCA(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tls")
	ca, err := OpenLocalCA(dir, "NivaroNAS")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"ca.key"} {
		if st, _ := os.Stat(filepath.Join(dir, f)); st.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode %v", f, st.Mode().Perm())
		}
	}
	if st, _ := os.Stat(dir); st.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %v", st.Mode().Perm())
	}

	now := time.Now()
	names := []string{"nivaronas", "nivaronas.local", "localhost", "evil.example.com"}
	ips := []net.IP{net.ParseIP("192.168.1.20"), net.ParseIP("8.8.8.8"), net.ParseIP("100.101.1.2"), net.ParseIP("fe80::1")}
	if changed, err := ca.Refresh(names, ips, now); err != nil || !changed {
		t.Fatalf("first refresh: %v %v", changed, err)
	}
	if !ca.Covers("192.168.1.20") || !ca.Covers("nivaronas.local") || !ca.Covers("nivaronas") || !ca.Covers("100.101.1.2") {
		t.Fatal("cert misses a local name")
	}
	if ca.Covers("evil.example.com") || ca.Covers("8.8.8.8") {
		t.Fatal("cert covers a public name")
	}

	// Chain verifies against the CA as a browser would, name constraints included.
	pool := x509.NewCertPool()
	root, _ := x509.ParseCertificate(ca.CADER())
	pool.AddCert(root)
	if _, err := ca.Leaf().Leaf.Verify(x509.VerifyOptions{Roots: pool, DNSName: "nivaronas.local"}); err != nil {
		t.Fatal(err)
	}

	// A cert the CA would sign for a public name must fail the constraints.
	if len(root.PermittedDNSDomains) == 0 || !root.PermittedDNSDomainsCritical || !root.MaxPathLenZero {
		t.Fatal("CA lacks name constraints / path length")
	}
	evilTmpl := &x509.Certificate{SerialNumber: serial(), NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		DNSNames: []string{"bank.example.com"}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	evilDER, _ := x509.CreateCertificate(rand.Reader, evilTmpl, ca.cert, &ca.key.PublicKey, ca.key)
	evil, _ := x509.ParseCertificate(evilDER)
	if _, err := evil.Verify(x509.VerifyOptions{Roots: pool, DNSName: "bank.example.com"}); err == nil {
		t.Fatal("a leaked CA key could vouch for an internet site")
	}

	// Same SANs (other order, dupes): no reissue.
	if changed, _ := ca.Refresh([]string{"localhost", "nivaronas.local", "nivaronas", "localhost"}, []net.IP{net.ParseIP("100.101.1.2"), net.ParseIP("192.168.1.20").To16()}, now); changed {
		t.Fatal("reissued for the same SANs")
	}
	// IP changed (DHCP): reissue, same CA.
	if changed, _ := ca.Refresh(names, []net.IP{net.ParseIP("192.168.1.33")}, now); !changed || !ca.Covers("192.168.1.33") || ca.Covers("192.168.1.20") {
		t.Fatal("no reissue on IP change")
	}
	// Close to expiry: renew.
	if changed, _ := ca.Refresh(names, []net.IP{net.ParseIP("192.168.1.33")}, now.Add(340*24*time.Hour)); !changed {
		t.Fatal("no renewal near expiry")
	}

	// Reopen from disk: same CA, the saved leaf is reused.
	fp := ca.Fingerprint()
	ca2, err := OpenLocalCA(dir, "other-name")
	if err != nil || ca2.Fingerprint() != fp {
		t.Fatalf("reopen: %v %s != %s", err, ca2.Fingerprint(), fp)
	}
	if changed, _ := ca2.Refresh(names, []net.IP{net.ParseIP("192.168.1.33")}, now.Add(340*24*time.Hour)); changed {
		t.Fatal("saved leaf not reused")
	}
	if len(ca2.Leaf().Certificate) != 2 {
		t.Fatal("chain should carry the CA")
	}

	// A leaf from another CA on disk is replaced.
	other, _ := OpenLocalCA(filepath.Join(t.TempDir(), "x"), "h")
	if _, err := other.Refresh(names, nil, now); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(other.dir, "server.crt"))
	k, _ := os.ReadFile(filepath.Join(other.dir, "server.key"))
	os.WriteFile(filepath.Join(dir, "server.crt"), b, 0o644)
	os.WriteFile(filepath.Join(dir, "server.key"), k, 0o600)
	ca3, _ := OpenLocalCA(dir, "")
	if changed, _ := ca3.Refresh(names, nil, now); !changed {
		t.Fatal("foreign leaf kept")
	}
}

func TestFingerprintFormat(t *testing.T) {
	fp := Fingerprint([]byte("x"))
	if len(fp) != 32*3-1 || fp[2] != ':' {
		t.Fatal(fp)
	}
}

func TestIsBridgeName(t *testing.T) {
	for n, want := range map[string]bool{"docker0": true, "br-1a2b": true, "veth12": true, "virbr0": true, "eth0": false, "enp3s0": false, "tailscale0": false, "wlan0": false} {
		if isBridgeName(n) != want {
			t.Errorf("%s", n)
		}
	}
}
