package main

import (
	"log"
	"net/http"
	"time"

	"github.com/aipermission/aipermission/backend/internal/api"
	"github.com/aipermission/aipermission/backend/internal/config"
	"github.com/aipermission/aipermission/backend/internal/connectors/builtin"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	registry, err := builtin.NewRegistry()
	if err != nil {
		log.Fatal(err)
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
	if err := httpServer.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
