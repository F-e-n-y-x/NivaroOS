package command

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"time"
)

// (OnlyExec / ExecResultStr - `bash -c <string>` helpers - were removed:
// every caller pasted request or device data into the string. Run commands
// with exec.Command and separate arguments; for local-storage-helper.sh use
// service.RunHelper.)

// ExecSmartCTLByPath: smartctl --json -a -l devstat with "-n standby", so
// the periodic read never wakes a sleeping drive. nil: no output at all
// (smartctl missing, timeout).
func ExecSmartCTLByPath(path string) []byte {
	return execSmartctl(6*time.Second, path, "-n", "standby")
}

// ExecSmartCTLFullByPath is like ExecSmartCTLByPath but without "-n standby" -
// used for an explicit, user-triggered info/test-status fetch where waking a
// sleeping drive is expected and fine, unlike the periodic list-population
// read above which deliberately avoids it.
func ExecSmartCTLFullByPath(path string) []byte {
	return execSmartctl(15*time.Second, path)
}

// execSmartctl: smartctl's exit status is a bitmask (a sleeping drive with
// "-n standby" exits 2, "error log has entries" sets 64) but the JSON is
// still there, so only "no output" is a failure. A USB enclosure smartctl
// doesn't know ("Unknown USB bridge ... specify device type") is tried
// once more as a SAT bridge, which nearly all of them are.
var smartctlOutput = func(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "smartctl", args...).Output()
}

func execSmartctl(timeout time.Duration, path string, extra ...string) []byte {
	run := func(dev ...string) []byte {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		args := append(append([]string{"--json", "-a", "-l", "devstat"}, extra...), dev...)
		out, err := smartctlOutput(ctx, append(args, path)...)
		if err != nil && (len(out) == 0 || ctx.Err() != nil) {
			return nil
		}
		return out
	}
	out := run()
	if out != nil && bytes.Contains(out, []byte("specify device type")) {
		if sat := run("-d", "sat"); sat != nil {
			return sat
		}
	}
	return out
}

func ExecEnabledSMART(path string) ([]byte, error) {
	return exec.Command("smartctl", "-s", "on", path).CombinedOutput()
}

// ExecSmartCTLSelfTest starts a SMART self-test ("short" or "long") on the
// drive itself - the drive runs it in the background and progress/result is
// read back later via a normal "smartctl -a" call.
func ExecSmartCTLSelfTest(path, testType string) (string, error) {
	cmd := exec.Command("smartctl", "-t", testType, path)
	println(cmd.String())
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// ExecHdparmSetStandby applies a spindown timer immediately via hdparm -S -
// the persisted /etc/hdparm.conf entry (see service/hdparm_config.go) is what
// makes it survive a reboot/reconnect; this just makes it take effect now too.
func ExecHdparmSetStandby(path string, code int) (string, error) {
	cmd := exec.Command("hdparm", "-S", strconv.Itoa(code), path)
	println(cmd.String())
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// 执行 lsblk 命令
func ExecLSBLKByPath(path string) []byte {
	output, err := exec.Command("lsblk", path, "-O", "-J", "-b").Output()
	if err != nil {
		fmt.Println("lsblk", err)
		return nil
	}
	return output
}

// 执行 lsblk 命令
func ExecLSBLK() []byte {
	output, err := exec.Command("lsblk", "-O", "-J", "-b").Output()
	if err != nil {
		fmt.Println("lsblk", err)
		return nil
	}
	return output
}
