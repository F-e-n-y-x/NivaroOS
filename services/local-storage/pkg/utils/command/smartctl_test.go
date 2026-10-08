package command

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSmartctlRetriesUnknownUSBBridgeAsSAT(t *testing.T) {
	defer func(f func(context.Context, ...string) ([]byte, error)) { smartctlOutput = f }(smartctlOutput)
	var calls []string
	smartctlOutput = func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		if strings.Contains(calls[len(calls)-1], "-d sat") {
			return []byte(`{"ok":1}`), nil
		}
		return []byte(`{"smartctl":{"messages":[{"string":"Unknown USB bridge\nPlease specify device type with the -d option."}]}}`), errors.New("exit status 1")
	}
	out := ExecSmartCTLByPath("/dev/sde")
	if string(out) != `{"ok":1}` || len(calls) != 2 || calls[1] != "--json -a -l devstat -n standby -d sat /dev/sde" {
		t.Fatalf("out=%s calls=%q", out, calls)
	}

	// no output at all (smartctl missing): nil, no retry
	calls = nil
	smartctlOutput = func(context.Context, ...string) ([]byte, error) { calls = append(calls, "x"); return nil, errors.New("not found") }
	if ExecSmartCTLFullByPath("/dev/sda") != nil || len(calls) != 1 {
		t.Fatal("missing smartctl")
	}
}
