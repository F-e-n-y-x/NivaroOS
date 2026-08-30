package command

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"time"
)

func OnlyExec(cmdStr string) (string, error) {
	cmd := exec.Command("/bin/bash", "-c", cmdStr)
	println(cmd.String())
	buf, err := cmd.CombinedOutput()
	println(string(buf))
	return string(buf), err
}

func ExecResultStr(cmdStr string) (string, error) {
	cmd := exec.Command("/bin/bash", "-c", cmdStr)
	println(cmd.String())
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}

	defer stdout.Close()
	if err := cmd.Start(); err != nil {
		return "", err
	}

	buf, err := io.ReadAll(stdout)
	if err != nil {
		return "", err
	}

	return string(buf), cmd.Wait()
}

// exec smart
func ExecSmartCTLByPath(path string) []byte {
	timeout := 6
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()
	//smartctl -i -n standby /dev/sdc  TODO:https://www.ippa.top/956.html
	cmd := exec.CommandContext(ctx, "smartctl", "-a", "-n", "standby", path, "-j")
	println(cmd.String())

	output, err := cmd.Output()
	if err != nil {
		fmt.Println(string(output))
		return nil
	}
	return output
}

func ExecEnabledSMART(path string) ([]byte, error) {
	return exec.Command("smartctl", "-s", "on", path).CombinedOutput()
}

// ExecSmartCTLFullByPath is like ExecSmartCTLByPath but without "-n standby" -
// used for an explicit, user-triggered info/test-status fetch where waking a
// sleeping drive is expected and fine, unlike the periodic list-population
// read above which deliberately avoids it.
func ExecSmartCTLFullByPath(path string) []byte {
	timeout := 15
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "smartctl", "-a", path, "-j")
	println(cmd.String())

	output, err := cmd.Output()
	if err != nil {
		fmt.Println(string(output))
		// smartctl's exit status is a bitmask of warning bits (e.g. "attribute
		// below threshold") - the JSON body is still usable even when it's
		// non-zero, unlike a hard failure that produced no output at all.
		if len(output) == 0 {
			return nil
		}
	}
	return output
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
