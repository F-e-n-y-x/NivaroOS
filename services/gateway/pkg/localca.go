package pkg

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// LocalCA is this box's own certificate authority for https on the home
// network (docs/specs/2026-10-16-lan-https.md). Its name constraints keep
// it to local names and private/Tailscale addresses, so a device that
// trusts it can't be fooled by it for any internet site.
type LocalCA struct {
	dir  string
	cert *x509.Certificate
	der  []byte
	key  *ecdsa.PrivateKey

	mu   sync.RWMutex
	leaf *tls.Certificate
}

const (
	caValidity   = 10 * 365 * 24 * time.Hour
	leafValidity = 365 * 24 * time.Hour // Apple: <= 825 days for user-trusted roots
	leafRenewal  = 30 * 24 * time.Hour
)

// LocalDNSDomains are the names the CA may vouch for (plus the hostname).
var LocalDNSDomains = []string{"local", "localhost", "lan", "home.arpa", "internal"}

// LocalIPRanges are the addresses the CA may vouch for.
var LocalIPRanges = func() []*net.IPNet {
	var out []*net.IPNet
	for _, c := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "100.64.0.0/10", "127.0.0.0/8", "fc00::/7", "::1/128"} {
		_, n, _ := net.ParseCIDR(c)
		out = append(out, n)
	}
	return out
}()

// OpenLocalCA loads the CA in dir, creating it (and dir) when missing.
func OpenLocalCA(dir, hostname string) (*LocalCA, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	ca := &LocalCA{dir: dir}
	if pair, err := tls.LoadX509KeyPair(ca.path("ca.crt"), ca.path("ca.key")); err == nil {
		key, ok := pair.PrivateKey.(*ecdsa.PrivateKey)
		if !ok {
			return nil, errors.New("ca.key is not an ECDSA key")
		}
		ca.der, ca.key = pair.Certificate[0], key
		ca.cert, err = x509.ParseCertificate(ca.der)
		return ca, err
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("reading the local CA: %w", err)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	name := "NivaroOS local CA"
	if h := cleanHostname(hostname); h != "" {
		name += " (" + h + ")"
	}
	permitted := LocalDNSDomains
	if h := cleanHostname(hostname); h != "" && !slices.Contains(permitted, h) {
		permitted = append(slices.Clone(permitted), h)
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:                serial(),
		Subject:                     pkix.Name{CommonName: name, Organization: []string{"NivaroOS"}},
		NotBefore:                   now.Add(-time.Hour),
		NotAfter:                    now.Add(caValidity),
		IsCA:                        true,
		BasicConstraintsValid:       true,
		MaxPathLenZero:              true,
		KeyUsage:                    x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		PermittedDNSDomainsCritical: true,
		PermittedDNSDomains:         permitted,
		PermittedIPRanges:           LocalIPRanges,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	// Key first: a crash between the two writes leaves no cert without key.
	if err := writePEM(ca.path("ca.key"), "EC PRIVATE KEY", mustMarshalEC(key), 0o600); err != nil {
		return nil, err
	}
	if err := writePEM(ca.path("ca.crt"), "CERTIFICATE", der, 0o644); err != nil {
		return nil, err
	}
	ca.der, ca.key = der, key
	ca.cert, err = x509.ParseCertificate(der)
	return ca, err
}

// CADER is the CA certificate (DER), what devices install.
func (c *LocalCA) CADER() []byte { return c.der }

// Fingerprint is the CA certificate's SHA-256, "AB:CD:...".
func (c *LocalCA) Fingerprint() string { return Fingerprint(c.der) }

// Fingerprint formats sha256(der) as colon-separated upper-case hex.
func Fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	parts := make([]string, len(sum))
	for i, b := range sum {
		parts[i] = fmt.Sprintf("%02X", b)
	}
	return strings.Join(parts, ":")
}

// Leaf is the current server certificate, nil before Refresh.
func (c *LocalCA) Leaf() *tls.Certificate {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.leaf
}

// GetCertificate is a tls.Config.GetCertificate.
func (c *LocalCA) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	if l := c.Leaf(); l != nil {
		return l, nil
	}
	return nil, errors.New("no server certificate yet")
}

// Covers reports whether the current server certificate is valid for host
// (a name or an IP, no port).
func (c *LocalCA) Covers(host string) bool {
	l := c.Leaf()
	return l != nil && l.Leaf.VerifyHostname(strings.Trim(host, "[]")) == nil
}

// Refresh makes sure the server certificate (loaded from disk, or issued)
// is signed by this CA, covers exactly names and ips (those the CA
// permits) and is not within leafRenewal of expiry. It reports whether a
// new one was issued.
func (c *LocalCA) Refresh(names []string, ips []net.IP, now time.Time) (bool, error) {
	names, ips = c.permitted(names, ips)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.leaf == nil {
		if pair, err := tls.LoadX509KeyPair(c.path("server.crt"), c.path("server.key")); err == nil {
			pair.Certificate = append(pair.Certificate[:1], c.der)
			c.leaf = &pair
		}
	}
	if l := c.leaf; l != nil && l.Leaf != nil && l.Leaf.CheckSignatureFrom(c.cert) == nil &&
		now.Add(leafRenewal).Before(l.Leaf.NotAfter) && sameSANs(l.Leaf, names, ips) {
		return false, nil
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return false, err
	}
	cn := "localhost"
	if len(names) > 0 {
		cn = names[0]
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial(),
		Subject:      pkix.Name{CommonName: cn, Organization: []string{"NivaroOS"}},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(leafValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     names,
		IPAddresses:  ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, c.cert, &key.PublicKey, c.key)
	if err != nil {
		return false, err
	}
	keyDER := mustMarshalEC(key)
	if err := writePEM(c.path("server.key"), "EC PRIVATE KEY", keyDER, 0o600); err != nil {
		return false, err
	}
	if err := writePEM(c.path("server.crt"), "CERTIFICATE", der, 0o644); err != nil {
		return false, err
	}
	leaf, _ := x509.ParseCertificate(der)
	c.leaf = &tls.Certificate{Certificate: [][]byte{der, c.der}, PrivateKey: key, Leaf: leaf}
	return true, nil
}

// LocalSANs lists this box's names and its addresses inside LocalIPRanges,
// skipping container and VM bridges (their churn would only reissue).
func LocalSANs() ([]string, []net.IP) {
	var names []string
	if h, err := os.Hostname(); err == nil {
		if h = cleanHostname(h); h != "" {
			names = append(names, h, h+".local")
		}
	}
	names = append(names, "localhost")
	var ips []net.IP
	ifaces, _ := net.Interfaces()
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || isBridgeName(ifc.Name) {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok {
				ips = append(ips, n.IP)
			}
		}
	}
	return names, ips
}

func isBridgeName(n string) bool {
	for _, p := range []string{"docker", "br-", "veth", "virbr", "vnet", "lxc", "cni", "flannel"} {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}

// permitted keeps the names and IPs the CA's constraints allow, deduped and
// sorted so the comparison with an existing cert is order-free.
func (c *LocalCA) permitted(names []string, ips []net.IP) ([]string, []net.IP) {
	var outN []string
	for _, n := range names {
		n = strings.ToLower(strings.TrimSuffix(n, "."))
		if n != "" && !slices.Contains(outN, n) && dnsPermitted(n, c.cert.PermittedDNSDomains) {
			outN = append(outN, n)
		}
	}
	var outI []net.IP
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			ip = v4
		}
		if ip.IsLinkLocalUnicast() || slices.ContainsFunc(outI, ip.Equal) {
			continue
		}
		if slices.ContainsFunc(c.cert.PermittedIPRanges, func(r *net.IPNet) bool { return r.Contains(ip) }) {
			outI = append(outI, ip)
		}
	}
	slices.Sort(outN)
	slices.SortFunc(outI, func(a, b net.IP) int { return strings.Compare(a.String(), b.String()) })
	return outN, outI
}

func dnsPermitted(name string, domains []string) bool {
	for _, d := range domains {
		if name == d || strings.HasSuffix(name, "."+d) {
			return true
		}
	}
	return false
}

func sameSANs(cert *x509.Certificate, names []string, ips []net.IP) bool {
	got := slices.Clone(cert.DNSNames)
	slices.Sort(got)
	if !slices.Equal(got, names) || len(cert.IPAddresses) != len(ips) {
		return false
	}
	for _, ip := range ips {
		if !slices.ContainsFunc(cert.IPAddresses, ip.Equal) {
			return false
		}
	}
	return true
}

// cleanHostname lower-cases h and drops it when it isn't a usable DNS label.
func cleanHostname(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	if h == "" || h == "localhost" || strings.ContainsAny(h, "._ ") {
		return ""
	}
	return h
}

func (c *LocalCA) path(name string) string { return filepath.Join(c.dir, name) }

func serial() *big.Int {
	n, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	return n
}

func mustMarshalEC(k *ecdsa.PrivateKey) []byte {
	b, err := x509.MarshalECPrivateKey(k)
	if err != nil {
		panic(err)
	}
	return b
}

// writePEM writes atomically (temp file + rename) with mode.
func writePEM(path, typ string, der []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := pem.Encode(tmp, &pem.Block{Type: typ, Bytes: der}); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
