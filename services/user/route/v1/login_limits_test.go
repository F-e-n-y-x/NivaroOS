package v1

import "testing"

// One client failing over and over backs off on its own (and a success
// clears it); another client of the same account is unaffected.
func TestRepeatedFailuresBackOffPerClient(t *testing.T) {
	l := newLoginLimits()
	a, b := loginKeys("Owner", "192.168.10.66"), loginKeys("owner", "192.168.10.67")
	for i := 0; i < 5; i++ {
		l.fail(a...)
	}
	l.reset(a[0]) // only the per-address bucket: the backoff is what's under test
	if l.allow(a...) {
		t.Fatal("after 5 failures the same client got straight back in")
	}
	if !l.allow(b...) {
		t.Fatal("another client of the account was blocked")
	}
	l.reset(a...)
	if !l.allow(a...) {
		t.Fatal("a success didn't clear the backoff")
	}
}

func TestIsInternetIP(t *testing.T) {
	for ip, want := range map[string]bool{
		"203.0.113.9": true, "2001:db8::1": true, "garbage": true, "": true,
		"127.0.0.1": false, "::1": false, "192.168.10.20": false, "10.1.2.3": false,
		"100.100.10.1": false, "fd7a:115c:a1e0::1": false, "::ffff:192.168.1.2": false, "fe80::1": false,
	} {
		if got := isInternetIP(ip); got != want {
			t.Errorf("isInternetIP(%q) = %v, want %v", ip, got, want)
		}
	}
}
