package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(exe), "..", ".."))
	server := filepath.Join(filepath.Dir(exe), serverBinaryName())
	if _, err := os.Stat(server); err != nil {
		return
	}
	logs := filepath.Join(root, "data", "logs")
	_ = os.MkdirAll(logs, 0755)
	logFile, err := os.OpenFile(filepath.Join(logs, "aits-server.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer logFile.Close()
	if err := loadRootEnv(filepath.Join(root, ".env")); err != nil {
		fmt.Fprintf(logFile, "[CONFIG] Failed to load .env: %v\n", err)
		return
	}
	if err := bootstrapPostgres(root, logFile); err != nil {
		fmt.Fprintf(logFile, "[DATABASE] Startup bootstrap failed: %v\n", err)
		return
	}
	cmd := exec.Command(server)
	cmd.Dir = root
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = append(os.Environ(), "AITS_SCHEMA_PATH="+filepath.Join(root, "data", "database", "SCHEMA.sql"))
	configureStart(cmd)
	if err := cmd.Start(); err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(logs, "aits-server.pid"), []byte(fmt.Sprintf("%d\n", cmd.Process.Pid)), 0644)
}

func serverBinaryName() string {
	if filepath.Ext(os.Args[0]) == ".exe" {
		return "aits-server.exe"
	}
	return "aits-server"
}
