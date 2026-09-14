package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cmdb/gateway-bff/internal/events"
	"cmdb/gateway-bff/internal/httpapi"
)

func main() {
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		client := &http.Client{Timeout: 3 * time.Second}
		response, err := client.Get("http://127.0.0.1" + addr + "/api/v1/health")
		if err != nil || response.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		_ = response.Body.Close()
		return
	}
	api := httpapi.NewServer()
	server := &http.Server{Addr: addr, Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second}
	runContext, stopPublisher := context.WithCancel(context.Background())
	go api.MonitorService().RunEscalations(runContext)
	go api.DiscoveryService().RunAgentHealth(runContext)
	go api.DiscoveryService().AutoScanLoops(runContext)
	go api.DiscoveryService().RunTerminalRetention(runContext)
	var publisher *events.OutboxPublisher
	if os.Getenv("KAFKA_BROKERS") != "" {
		var err error
		publisher, err = events.NewOutboxPublisher(os.Getenv("DATABASE_URL"), os.Getenv("KAFKA_BROKERS"))
		if err != nil {
			slog.Error("initialize outbox publisher", "error", err)
			os.Exit(1)
		}
		go publisher.Run(runContext)
		go api.DiscoveryService().Consume(runContext, os.Getenv("KAFKA_BROKERS"))
		go api.MonitorService().ConsumeNotifications(runContext, os.Getenv("KAFKA_BROKERS"))
	}
	go func() {
		slog.Info("gateway-bff started", "address", addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("serve", "error", err)
			os.Exit(1)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	stopPublisher()
	if publisher != nil {
		_ = publisher.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		slog.Error("shutdown", "error", err)
	}
}
