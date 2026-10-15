package v1

import (
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/user/pkg/utils/encryption"
	"github.com/F-e-n-y-x/NivaroOS/services/user/service"
	model2 "github.com/F-e-n-y-x/NivaroOS/services/user/service/model"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/time/rate"
)

// Passwords were stored as unsalted MD5. New ones are bcrypt; an old MD5
// hash still verifies and is replaced by bcrypt on that successful login.

func hashPassword(pw string) string {
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return ""
	}
	return string(h)
}

var dummyHash = sync.OnceValue(func() []byte { return []byte(hashPassword("no such account")) })

// verifyPassword checks pw against u's stored hash, upgrading an MD5 hash.
func verifyPassword(u *model2.UserDBModel, pw string) bool {
	if strings.HasPrefix(u.Password, "$2") {
		return bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(pw)) == nil
	}
	if u.Password == "" {
		// No such account: spend a bcrypt anyway, so the reply time doesn't
		// tell which names exist.
		_ = bcrypt.CompareHashAndPassword(dummyHash(), []byte(pw))
		return false
	}
	if u.Password != encryption.GetMD5ByStr(pw) {
		return false
	}
	if h := hashPassword(pw); h != "" {
		u.Password = h
		service.MyService.User().UpdateUserPassword(*u)
	}
	return true
}

// Login attempts are limited three ways:
//   - per client address ("ip:"): 5 at once, then 1 per 12 s;
//   - per account name and client ("pair:"): after 5 failures each further
//     failure doubles a wait (15 s up to 15 min), cleared by a success;
//   - per account name for internet clients only ("user:"): 10 at once,
//     then 1 a minute in all. LAN, tailnet and loopback clients skip it, so
//     someone on the internet can't lock the owner out at home or over
//     Tailscale (one per-name bucket for everyone used to do exactly that).
type loginLimits struct {
	mu      sync.Mutex
	buckets map[string]*rate.Limiter
	fails   map[string]int
	until   map[string]time.Time
	seen    map[string]time.Time
}

var loginLimiter = newLoginLimits()

func newLoginLimits() *loginLimits {
	return &loginLimits{buckets: map[string]*rate.Limiter{}, fails: map[string]int{}, until: map[string]time.Time{}, seen: map[string]time.Time{}}
}

// loginKeys are the limiter keys for a login of name from client ip.
func loginKeys(name, ip string) []string {
	name = strings.ToLower(name)
	keys := []string{"ip:" + ip, "pair:" + name + "|" + ip}
	if isInternetIP(ip) {
		keys = append(keys, "user:"+name)
	}
	return keys
}

var cgnat = netip.MustParsePrefix("100.64.0.0/10") // Tailscale

// isInternetIP: anything but loopback, private (LAN), link-local or tailnet.
// An address that doesn't parse counts as internet.
func isInternetIP(ip string) bool {
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return true
	}
	a = a.Unmap()
	return !(a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || cgnat.Contains(a))
}

func (l *loginLimits) allow(keys ...string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if len(l.seen) > 5000 {
		for k, t := range l.seen {
			if now.Sub(t) > time.Hour && now.After(l.until[k]) {
				delete(l.seen, k)
				delete(l.buckets, k)
				delete(l.fails, k)
				delete(l.until, k)
			}
		}
	}
	ok := true
	for _, k := range keys {
		l.seen[k] = now
		if strings.HasPrefix(k, "pair:") {
			if now.Before(l.until[k]) {
				ok = false
			}
			continue
		}
		b := l.buckets[k]
		if b == nil {
			if strings.HasPrefix(k, "user:") {
				b = rate.NewLimiter(rate.Every(time.Minute), 10)
			} else {
				b = rate.NewLimiter(rate.Every(12*time.Second), 5) // 5 at once, then 5/min
			}
			l.buckets[k] = b
		}
		if !b.Allow() {
			ok = false
		}
	}
	return ok
}

// fail records a wrong password for the "pair:" keys among keys.
func (l *loginLimits) fail(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, k := range keys {
		if !strings.HasPrefix(k, "pair:") {
			continue
		}
		l.fails[k]++
		if n := l.fails[k]; n >= 5 {
			wait := 15 * time.Second << min(n-5, 6) // 15 s .. 16 min
			l.until[k] = time.Now().Add(min(wait, 15*time.Minute))
		}
	}
}

// reset clears a client's bucket and backoff after a successful login. The
// internet-wide "user:" bucket is never reset (a success doesn't buy an
// attacker more guesses).
func (l *loginLimits) reset(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, k := range keys {
		if strings.HasPrefix(k, "user:") {
			continue
		}
		delete(l.buckets, k)
		delete(l.fails, k)
		delete(l.until, k)
	}
}

// HashPassword is hashPassword for the command-line password reset.
func HashPassword(pw string) string { return hashPassword(pw) }
