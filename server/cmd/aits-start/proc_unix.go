//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

func configureStart(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
