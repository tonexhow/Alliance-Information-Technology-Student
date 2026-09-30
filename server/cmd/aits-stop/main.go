package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func main() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(exe), "..", ".."))
	pidFile := filepath.Join(root, "data", "logs", "aits-server.pid")
	body, err := os.ReadFile(pidFile)
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(body)))
	if err != nil || pid < 1 {
		return
	}
	stopProcess(pid)
	_ = os.Remove(pidFile)
}
