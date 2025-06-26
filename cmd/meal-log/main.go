// cmd/meal-log/main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"mcp-meal-log/internal/server"
)

var (
	transport = flag.String("transport", "http", "Transport mode: http")
	port      = flag.Int("port", 8011, "Port for HTTP transport")
	host      = flag.String("host", "0.0.0.0", "Host address")
	address   = flag.String("address", "", "Address (alias for host)")
	dbPath    = flag.String("db-path", "/data/meal-log.db", "Database path")
	version   = flag.Bool("version", false, "Show version")
)

func main() {
	flag.Parse()

	if *version {
		fmt.Println("mcp-meal-log version 1.0.0")
		os.Exit(0)
	}

	// Use address if provided, otherwise use host
	hostAddr := *host
	if *address != "" {
		hostAddr = *address
	}

	config := &server.Config{
		Transport: *transport,
		Host:      hostAddr,
		Port:      *port,
		DBPath:    *dbPath,
	}

	// Create server
	srv, err := server.NewMealLogServer(config)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	// Setup graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start server in goroutine
	errCh := make(chan error, 1)
	go func() {
		log.Printf("Starting meal log server on %s:%d", hostAddr, *port)
		if err := srv.Start(ctx); err != nil {
			errCh <- err
		}
	}()

	// Wait for shutdown signal or error
	select {
	case <-sigCh:
		log.Println("Received shutdown signal")
	case err := <-errCh:
		log.Printf("Server error: %v", err)
	}

	// Graceful shutdown
	log.Println("Shutting down...")
	cancel()
	if err := srv.Stop(); err != nil {
		log.Printf("Error during shutdown: %v", err)
	}
}
