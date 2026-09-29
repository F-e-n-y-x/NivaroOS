package termsession

import (
	"os"
	"strconv"
	"strings"
)

// procRoot is a var so tests can point it at a fixture.
var procRoot = "/proc"

// ForegroundInfo describes the foreground job of the terminal that pid
// (a shell) is attached to - its command name and working directory -
// falling back to pid itself. Linux /proc only; zero when unavailable.
func ForegroundInfo(pid int) ProcInfo {
	if pid <= 0 {
		return ProcInfo{}
	}
	fg := pid
	if tp := tpgid(pid); tp > 0 {
		fg = tp
	}
	pi := procInfo(fg)
	if pi == (ProcInfo{}) && fg != pid {
		pi = procInfo(pid)
	}
	return pi
}

func procInfo(pid int) ProcInfo {
	dir := procRoot + "/" + strconv.Itoa(pid)
	var pi ProcInfo
	if cwd, err := os.Readlink(dir + "/cwd"); err == nil {
		pi.Cwd = cwd
	}
	if comm, err := os.ReadFile(dir + "/comm"); err == nil {
		pi.Command = strings.TrimSpace(string(comm))
	}
	return pi
}

// tpgid is field 8 of /proc/<pid>/stat: the foreground process group of
// the process's controlling terminal.
func tpgid(pid int) int {
	b, err := os.ReadFile(procRoot + "/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0
	}
	s := string(b)
	// comm (field 2) is parenthesised and may contain spaces/parens.
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return 0
	}
	f := strings.Fields(s[i+1:])
	// f[0]=state(3) f[1]=ppid(4) f[2]=pgrp(5) f[3]=session(6) f[4]=tty_nr(7) f[5]=tpgid(8)
	if len(f) < 6 {
		return 0
	}
	n, err := strconv.Atoi(f[5])
	if err != nil {
		return 0
	}
	return n
}
