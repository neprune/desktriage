package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"desktriage.davea.me/config"
	"desktriage.davea.me/db"
	"desktriage.davea.me/db/dbgen"
	"desktriage.davea.me/srv"
)

func defaultDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "desktriage.db"
	}
	return filepath.Join(home, ".config", "desktriage", "desktriage.db")
}

var (
	flagListenAddr = flag.String("listen", ":8086", "address to listen on")
	flagDBPath     = flag.String("db", defaultDBPath(), "path to SQLite database")
	flagEnvFile    = flag.String("env", ".env", "path to .env file for initial seed")
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	flag.Parse()

	// Ensure DB directory exists
	dbDir := filepath.Dir(*flagDBPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return fmt.Errorf("create db dir %s: %w", dbDir, err)
	}

	// Open DB and run migrations
	dbase, err := db.Open(*flagDBPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	if err := db.RunMigrations(dbase); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	// Seed config from .env if present
	q := dbgen.New(dbase)
	ctx := context.Background()
	if _, err := os.Stat(*flagEnvFile); err == nil {
		n, err := config.SeedFromEnv(ctx, q, *flagEnvFile)
		if err != nil {
			slog.Warn("seed config from .env", "error", err)
		} else if n > 0 {
			slog.Info("seeded config from .env", "keys", n)
		}
	}

	// Load config from DB
	cfg, err := config.LoadFromDB(ctx, q)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	server, err := srv.NewWithDB(dbase, cfg)
	if err != nil {
		return fmt.Errorf("create server: %w", err)
	}

	// Set up SIGHUP handler to trigger graceful restart.
	restart := false
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP)

	srvCtx, srvCancel := context.WithCancel(ctx)
	defer srvCancel()

	go func() {
		<-sigCh
		slog.Info("received SIGHUP, restarting...")
		restart = true
		srvCancel()
	}()

	err = server.ServeWithContext(srvCtx, *flagListenAddr)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	if !restart {
		return nil
	}

	// Close DB before re-exec
	dbase.Close()

	// Re-execute ourselves with the same arguments.
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	slog.Info("re-executing", "path", exe)
	return syscall.Exec(exe, os.Args, os.Environ())
}
