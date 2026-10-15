package service

import (
	"sync"
	"testing"
	"time"
)

// L3: a plain map written by unauthenticated requests crashed the service
// on concurrent use, and keys never expired.
func TestRegisterKeysOnceAndExpire(t *testing.T) {
	r := &registerKeys{ttl: time.Minute, m: map[string]time.Time{}}
	k := r.New()
	if !r.Valid(k) || !r.Take(k) || r.Take(k) || r.Valid(k) {
		t.Fatal("a key must work exactly once")
	}
	old := &registerKeys{ttl: -time.Second, m: map[string]time.Time{}}
	if k := old.New(); old.Valid(k) || old.Take(k) {
		t.Fatal("an expired key worked")
	}
	if r.Take("") || r.Take("made-up") {
		t.Fatal("an unknown key worked")
	}
}

func TestRegisterKeysConcurrent(t *testing.T) {
	r := &registerKeys{ttl: time.Minute, m: map[string]time.Time{}}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Take(r.New())
		}()
	}
	wg.Wait()
}
