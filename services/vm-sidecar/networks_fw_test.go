package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeFW simulates iptables: docker says whether DOCKER-USER exists; rules
// holds the rules present per tool.
func fakeFW(t *testing.T, docker bool) (rules map[string][]string, calls *[]string) {
	t.Helper()
	rules = map[string][]string{}
	cl := []string{}
	old := runFW
	runFW = func(tool string, args ...string) error {
		c := strings.Join(args, " ")
		cl = append(cl, tool+" "+c)
		switch {
		case strings.Contains(c, "-L DOCKER-USER"):
			if docker {
				return nil
			}
			return errors.New("no chain")
		case strings.HasPrefix(c, "-w -C "):
			for _, r := range rules[tool] {
				if r == strings.TrimPrefix(c, "-w -C ") {
					return nil
				}
			}
			return errors.New("no rule")
		case strings.HasPrefix(c, "-w -I "):
			rules[tool] = append(rules[tool], strings.TrimPrefix(c, "-w -I "))
		case strings.HasPrefix(c, "-w -D "):
			r := strings.TrimPrefix(c, "-w -D ")
			for i, x := range rules[tool] {
				if x == r {
					rules[tool] = append(rules[tool][:i], rules[tool][i+1:]...)
					return nil
				}
			}
			return errors.New("no rule")
		}
		return nil
	}
	t.Cleanup(func() { runFW = old })
	return rules, &cl
}

func TestBridgeForwardingRuleIsIdempotentAndRemovable(t *testing.T) {
	rules, _ := fakeFW(t, true)
	if !ensureBridgeForwarding("br0") {
		t.Fatal("Docker's chain exists: must report it")
	}
	ensureBridgeForwarding("br0") // again: no duplicate
	for _, tool := range fwTools {
		if len(rules[tool]) != 1 || !strings.HasPrefix(rules[tool][0], "DOCKER-USER -i br0 -o br0 -j ACCEPT") {
			t.Fatalf("%s rules %q", tool, rules[tool])
		}
	}
	removeBridgeForwarding("br0")
	for _, tool := range fwTools {
		if len(rules[tool]) != 0 {
			t.Fatalf("%s rule left after removal: %q", tool, rules[tool])
		}
	}
}

func TestBridgeForwardingWithoutDockerDoesNothing(t *testing.T) {
	rules, calls := fakeFW(t, false)
	if ensureBridgeForwarding("br0") {
		t.Fatal("no Docker chain: must say so")
	}
	if len(rules) != 0 {
		t.Fatalf("rules added without Docker: %v", rules)
	}
	for _, c := range *calls {
		if strings.Contains(c, " -I ") {
			t.Fatalf("inserted without Docker: %s", c)
		}
	}
}

// At boot Docker may come up after the sidecar: the startup pass retries
// until DOCKER-USER exists, then adds every registered bridge's rule.
func TestEnsureBridgesForwardingWaitsForDocker(t *testing.T) {
	reg := filepath.Join(t.TempDir(), "bridges.json")
	_ = addBridgeToRegistry(reg, BridgeNetwork{Name: "br0", HostNIC: "enp7s0"})
	_ = addBridgeToRegistry(reg, BridgeNetwork{Name: "br1", HostNIC: "enp8s0", Backend: backendNM})
	rules, _ := fakeFW(t, false)
	old := fwRetry
	fwRetry.every, fwRetry.tries = time.Millisecond, 50
	t.Cleanup(func() { fwRetry = old })
	ready := runFW
	n := 0
	runFW = func(tool string, args ...string) error {
		if strings.Contains(strings.Join(args, " "), "-L DOCKER-USER") {
			n++
			if n < 7 {
				return errors.New("docker not up yet")
			}
			return nil
		}
		return ready(tool, args...)
	}
	EnsureBridgesForwarding(reg)
	got := strings.Join(rules["iptables"], "\n")
	for _, b := range []string{"-i br0 -o br0", "-i br1 -o br1"} {
		if !strings.Contains(got, b) {
			t.Errorf("missing rule for %q after Docker came up:\n%s", b, got)
		}
	}
}
