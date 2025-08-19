package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sapcc/opensearch-query-exporter/pkg/config"
	"github.com/sapcc/opensearch-query-exporter/pkg/metrics"
	"github.com/sapcc/opensearch-query-exporter/pkg/opensearch"
)

var (
	listenAddress = flag.String("listen-address", ":9206", "Address to listen on for metrics")
	configPath    = flag.String("config", "config.yaml", "Path to configuration file")
	opensearchURL = flag.String("opensearch-url", "https://localhost:9200", "OpenSearch URL (must be https)")
	insecure      = flag.Bool("insecure", false, "Skip TLS certificate verification (insecure)")
	timeout       = flag.Duration("timeout", 30*time.Second, "Query timeout")
	logLevel      = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
)

func main() {
	flag.Parse()

	// Set up logging
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	// Load configuration
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Override config with command line flags if provided
	if *opensearchURL != "https://localhost:9200" {
		cfg.OpenSearchURL = *opensearchURL
	}
	cfg.Insecure = *insecure
	cfg.Timeout = *timeout

	// Create OpenSearch client
	client, err := opensearch.NewClient(cfg)
	if err != nil {
		log.Fatalf("Failed to create OpenSearch client: %v", err)
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Ping(ctx); err != nil {
		log.Fatalf("Failed to connect to OpenSearch: %v", err)
	}

	log.Printf("Successfully connected to OpenSearch at %s", cfg.OpenSearchURL)

	// Create metrics collector
	collector := metrics.NewCollector(client, cfg)
	prometheus.MustRegister(collector)

	// Set up HTTP server
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
<head><title>OpenSearch Exporter</title></head>
<body>
<h1>OpenSearch Exporter</h1>
<p><a href="/metrics">Metrics</a></p>
</body>
</html>`))
	})

	// Start server
	server := &http.Server{
		Addr:         *listenAddress,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("Starting server on %s", *listenAddress)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	<-sigChan
	log.Println("Shutting down server...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	log.Println("Server stopped")
}
