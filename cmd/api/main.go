package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"phantomframe/internal/api"
	"syscall"
	"time"
)

func main() {
	config, err := api.LoadConfig()
	if err != nil {
		log.Fatal(err)
	}
	app, err := api.Open(config)
	if err != nil {
		log.Fatal(err)
	}
	defer app.Close()
	if err = app.Migrate(); err != nil {
		log.Fatal(err)
	}
	app.Seed()
	if path := os.Getenv("PROVIDER_INVENTORY_FILE"); path != "" && !config.SupplierInstances {
		result, err := app.ImportProviderInventoryFile(path)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("provider inventory routes=%v providers=%v excluded3D=%v applied=%v pricesInherited=%v", result["routes"], result["providers"], result["excluded3D"], result["applied"], result["pricesInherited"])
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	worker := app.StartWorker(ctx)
	server := &http.Server{Addr: net.JoinHostPort(config.Host, config.Port), Handler: app.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 60 * time.Second, WriteTimeout: 120 * time.Second, IdleTimeout: 90 * time.Second}
	go func() {
		log.Printf("PhantomFrame Go API http://%s (MySQL + Redis)", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server: %v", err)
			stop()
		}
	}()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdown)
	select {
	case <-worker:
	case <-shutdown.Done():
	}
}
