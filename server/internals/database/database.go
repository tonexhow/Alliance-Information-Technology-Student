package database

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func envInt(name string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil || value < 1 {
		return fallback
	}
	return value
}

func envInt32(name string, fallback int32) int32 {
	value := envInt(name, int(fallback))
	if value > 500 {
		value = 500
	}
	return int32(value)
}

type Store struct {
	pool *pgxpool.Pool
}

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}

	config.MaxConns = envInt32("DATABASE_MAX_CONNS", 20)
	config.MinConns = envInt32("DATABASE_MIN_CONNS", 2)
	if config.MinConns > config.MaxConns {
		config.MinConns = config.MaxConns
	}
	config.MaxConnIdleTime = time.Duration(envInt("DATABASE_MAX_CONN_IDLE_MINUTES", 5)) * time.Minute
	config.MaxConnLifetime = time.Duration(envInt("DATABASE_MAX_CONN_LIFETIME_MINUTES", 30)) * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}

	store := &Store{pool: pool}
	if err := store.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	if err := store.ApplySchema(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.pool == nil {
		return fmt.Errorf("database is not initialized")
	}
	if err := s.pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}
	return nil
}

func loadSchemaSQL() ([]byte, string, error) {
	if configured := os.Getenv("AITS_SCHEMA_PATH"); configured != "" {
		body, err := os.ReadFile(configured)
		return body, configured, err
	}

	candidates := []string{
		filepath.Join("..", "data", "database", "SCHEMA.sql"),
		filepath.Join("data", "database", "SCHEMA.sql"),
		filepath.Join("..", "..", "data", "database", "SCHEMA.sql"),
	}
	for _, candidate := range candidates {
		body, err := os.ReadFile(candidate)
		if err == nil {
			return body, candidate, nil
		}
	}
	return nil, "", fmt.Errorf("SCHEMA.sql was not found under data/database")
}

func (s *Store) ApplySchema(ctx context.Context) error {
	if s == nil || s.pool == nil {
		return fmt.Errorf("database is not initialized")
	}
	schemaSQL, source, err := loadSchemaSQL()
	if err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, string(schemaSQL)); err != nil {
		return fmt.Errorf("apply database schema from %s: %w", source, err)
	}
	return nil
}

func (s *Store) Pool() *pgxpool.Pool {
	return s.pool
}
