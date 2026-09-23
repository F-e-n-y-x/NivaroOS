package v1

import (
	"errors"
	"testing"
)

// A password is written to chpasswd as "user:password\n"; a newline in it
// started a second line - "x\nroot:owned" set root's password.
func TestSystemPasswordCantInjectAnotherAccount(t *testing.T) {
	for _, bad := range []string{"x\nroot:owned1", "abcdefgh\r", "pass\x00word", "short"} {
		if err := checkSystemPassword(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	for _, good := range []string{"correct horse", "p:a:s:s:word", "ünïcödé-pass"} {
		if err := checkSystemPassword(good); err != nil {
			t.Errorf("rejected %q: %v", good, err)
		}
	}
}

func fakeAccounts(name string) (int, error) {
	switch name {
	case "root":
		return 0, nil
	case "www-data":
		return 33, nil
	case "owner":
		return 1000, nil
	case "alice":
		return 1001, nil
	case "nobody":
		return 65534, nil
	}
	return 0, errors.New("unknown user")
}

// Only regular accounts can be changed from the web UI - never root,
// service accounts or the protected owner account.
func TestOnlyRegularUnprotectedAccountsAreManageable(t *testing.T) {
	protected := func(n string) bool { return n == "owner" }
	cases := map[string]bool{
		"alice":    true,
		"root":     false,
		"www-data": false,
		"nobody":   false,
		"owner":    false,
		"ghost":    false, // doesn't exist
		"Bad;Name": false,
	}
	for name, want := range cases {
		err := manageableAccount(name, fakeAccounts, protected)
		if (err == nil) != want {
			t.Errorf("%s: manageable=%v, want %v (err=%v)", name, err == nil, want, err)
		}
	}
}
