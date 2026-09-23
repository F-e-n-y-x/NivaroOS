package v1

import (
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

// verifyPassword checks pw against u's stored hash, upgrading an MD5 hash.
func verifyPassword(u *model2.UserDBModel, pw string) bool {
	if strings.HasPrefix(u.Password, "$2") {
		return bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(pw)) == nil
	}
	if u.Password == "" || u.Password != encryption.GetMD5ByStr(pw) {
		return false
	}
	if h := hashPassword(pw); h != "" {
		u.Password = h
		service.MyService.User().UpdateUserPassword(*u)
	}
	return true
}

// Login attempts are limited per client address and per account name (one
// global limiter let anyone lock the owner out with five bad guesses).
type loginLimits struct {
	mu      sync.Mutex
	buckets map[string]*rate.Limiter
	seen    map[string]time.Time
}

var loginLimiter = &loginLimits{buckets: map[string]*rate.Limiter{}, seen: map[string]time.Time{}}

func (l *loginLimits) allow(keys ...string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if len(l.seen) > 5000 {
		for k, t := range l.seen {
			if now.Sub(t) > time.Hour {
				delete(l.seen, k)
				delete(l.buckets, k)
			}
		}
	}
	ok := true
	for _, k := range keys {
		b := l.buckets[k]
		if b == nil {
			b = rate.NewLimiter(rate.Every(12*time.Second), 5) // 5 at once, then 5/min
			l.buckets[k] = b
		}
		l.seen[k] = now
		if !b.Allow() {
			ok = false
		}
	}
	return ok
}

// reset clears a client's bucket after a successful login.
func (l *loginLimits) reset(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, k := range keys {
		delete(l.buckets, k)
	}
}

// HashPassword is hashPassword for the command-line password reset.
func HashPassword(pw string) string { return hashPassword(pw) }
