//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

func configureStart(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000 | 0x00000200}
}
