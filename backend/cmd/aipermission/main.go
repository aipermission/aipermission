package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aipermission/aipermission/backend/internal/api"
	"github.com/aipermission/aipermission/backend/internal/config"
	"github.com/aipermission/aipermission/backend/internal/connectors/builtin"
	"github.com/aipermission/aipermission/backend/internal/migration"
)

const shutdownTimeout = 10 * time.Second

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	if os.Getenv("AIPERMISSION_MIGRATION_MODE") == "1" {
		return runMigrationServer(ctx)
	}
	return runGatewayServer(ctx)
}

func runGatewayServer(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	registry, err := builtin.NewRegistry()
	if err != nil {
		return err
	}
	server := api.NewLockedServer(cfg, api.WithConnectorRegistry(registry))
	defer server.Close()

	log.Printf("aipermission backend listening on %s", cfg.Address())
	httpServer := &http.Server{
		Addr:              cfg.Address(),
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       60 * time.Second,
	}
	// Shutdown does not close hijacked connections such as console WebSockets.
	// Closing gateway runtimes here releases them while HTTP shutdown drains.
	httpServer.RegisterOnShutdown(server.Close)
	return listenAndServe(ctx, httpServer, shutdownTimeout)
}

func runMigrationServer(ctx context.Context) error {
	cfg, err := migration.LoadConfig()
	if err != nil {
		return err
	}
	server := migration.NewServer(cfg)
	log.Printf("aipermission migration helper listening on %s", cfg.Address())
	return listenAndServe(ctx, &http.Server{
		Addr:              cfg.Address(),
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       60 * time.Second,
	}, shutdownTimeout)
}

func listenAndServe(ctx context.Context, server *http.Server, gracePeriod time.Duration) error {
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", server.Addr, err)
	}
	return serveHTTP(ctx, server, listener, gracePeriod)
}

func serveHTTP(ctx context.Context, server *http.Server, listener net.Listener, gracePeriod time.Duration) error {
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), gracePeriod)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		return fmt.Errorf("graceful HTTP shutdown: %w", err)
	}
	err := <-serveErr
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
