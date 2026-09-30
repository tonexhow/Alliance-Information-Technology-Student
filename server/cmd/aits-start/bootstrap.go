package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func loadRootEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key == "" || value == "" || strings.TrimSpace(os.Getenv(key)) != "" {
			continue
		}
		_ = os.Setenv(key, value)
	}
	return scanner.Err()
}

func bootstrapPostgres(root string, logWriter io.Writer) error {
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		return fmt.Errorf("DATABASE_URL is not configured")
	}

	cfg, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	databaseName := strings.TrimSpace(cfg.Database)
	if databaseName == "" {
		return fmt.Errorf("DATABASE_URL does not contain a database name")
	}

	// Fast path: the configured database already exists and accepts this account.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	conn, targetErr := pgx.ConnectConfig(ctx, cfg)
	cancel()
	if targetErr == nil {
		conn.Close(context.Background())
		return nil
	}

	// If the target database is missing, connect to the standard maintenance DB.
	// The configured PostgreSQL role must have CREATEDB (or superuser) privilege.
	maintenance := cfg.Copy()
	maintenance.Database = "postgres"
	ctx, cancel = context.WithTimeout(context.Background(), 12*time.Second)
	admin, err := pgx.ConnectConfig(ctx, maintenance)
	cancel()
	if err != nil {
		return fmt.Errorf("target database is unavailable (%v); maintenance connection also failed: %w", targetErr, err)
	}
	defer admin.Close(context.Background())

	var exists bool
	ctx, cancel = context.WithTimeout(context.Background(), 8*time.Second)
	err = admin.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname=$1)", databaseName).Scan(&exists)
	cancel()
	if err != nil {
		return fmt.Errorf("check PostgreSQL database %q: %w", databaseName, err)
	}
	if exists {
		return fmt.Errorf("database %q exists but AITS could not connect to it: %w", databaseName, targetErr)
	}

	fmt.Fprintf(logWriter, "[DATABASE] Creating missing PostgreSQL database %s...\n", databaseName)
	ctx, cancel = context.WithTimeout(context.Background(), 20*time.Second)
	_, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{databaseName}.Sanitize())
	cancel()
	if err != nil {
		return fmt.Errorf("create PostgreSQL database %q (the DATABASE_URL role needs CREATEDB privilege): %w", databaseName, err)
	}

	if err := restoreConfiguredDump(root, databaseURL, logWriter); err != nil {
		return err
	}

	// Confirm that the newly-created/restored database can be reached before the
	// AITS backend starts and applies SCHEMA.sql.
	ctx, cancel = context.WithTimeout(context.Background(), 12*time.Second)
	conn, err = pgx.ConnectConfig(ctx, cfg)
	cancel()
	if err != nil {
		return fmt.Errorf("connect to newly-created PostgreSQL database %q: %w", databaseName, err)
	}
	conn.Close(context.Background())
	return nil
}

func restoreConfiguredDump(root, databaseURL string, logWriter io.Writer) error {
	dump := strings.TrimSpace(os.Getenv("AITS_PG_DUMP"))
	if dump != "" && !filepath.IsAbs(dump) {
		dump = filepath.Join(root, filepath.FromSlash(dump))
	}
	if dump == "" {
		candidates := []string{
			filepath.Join(root, "data", "database", "aits.backup"),
			filepath.Join(root, "data", "database", "aits.dump"),
			filepath.Join(root, "data", "database", "pg_dump.backup"),
			filepath.Join(root, "data", "database", "pg_dump.sql"),
		}
		for _, candidate := range candidates {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				dump = candidate
				break
			}
		}
	}
	if dump == "" {
		fmt.Fprintln(logWriter, "[DATABASE] No pg_dump restore file configured; SCHEMA.sql will initialize the new database.")
		return nil
	}
	if info, err := os.Stat(dump); err != nil || info.IsDir() {
		return fmt.Errorf("configured PostgreSQL dump was not found: %s", dump)
	}

	ext := strings.ToLower(filepath.Ext(dump))
	var command string
	var args []string
	if ext == ".sql" {
		command = "psql"
		args = []string{"--dbname", databaseURL, "--set", "ON_ERROR_STOP=on", "--file", dump}
	} else {
		command = "pg_restore"
		args = []string{"--dbname", databaseURL, "--no-owner", "--no-privileges", "--exit-on-error", dump}
	}
	exe, err := exec.LookPath(command)
	if err != nil {
		return fmt.Errorf("%s is required to restore %s but was not found in PATH", command, dump)
	}

	fmt.Fprintf(logWriter, "[DATABASE] Restoring PostgreSQL dump %s...\n", dump)
	cmd := exec.Command(exe, args...)
	cmd.Dir = root
	cmd.Stdout = logWriter
	cmd.Stderr = logWriter
	configureStart(cmd)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("restore PostgreSQL dump %s: %w", dump, err)
	}
	fmt.Fprintln(logWriter, "[DATABASE] PostgreSQL dump restore completed.")
	return nil
}
