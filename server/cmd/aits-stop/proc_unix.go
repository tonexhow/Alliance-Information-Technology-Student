//go:build !windows

package main

import "syscall"

func stopProcess(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGTERM)
}
