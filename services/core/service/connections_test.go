package service

import (
	"strings"
	"testing"
)

// The port field was ignored and a comma in the password cut the options
// string (so the rest became bogus mount options).
func TestCIFSOptionsCarryPortAndEscapeCommas(t *testing.T) {
	got := cifsOptions("ayush", "pa,ss=word", "4455", "192.168.10.20")
	for _, want := range []string{"username=ayush", "password=pa,,ss=word", "port=4455", "ip=192.168.10.20"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q missing %q", got, want)
		}
	}
	if strings.Count(got, ",") < 4 {
		t.Errorf("unexpected layout %q", got)
	}
}
