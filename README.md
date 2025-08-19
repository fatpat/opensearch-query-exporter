# OpenSearch Query Exporter

A Prometheus exporter for OpenSearch queries, written in Go. Teams can define custom queries via YAML configuration and expose results as Prometheus metrics.

## Key Features

- **Team-Oriented**: YAML-based query configuration per team
- **Secure**: TLS-only with credential failover support
- **Efficient**: Concurrent query execution with minimal resource usage
- **Comprehensive**: Automatic metric extraction from hits, aggregations, and cluster health

## Quick Start

```bash
# Using Docker
git clone https://github.com/sapcc/opensearch-query-exporter.git
cd opensearch-query-exporter
docker-compose up -d

# View metrics at http://localhost:9206/metrics
```

### Building from Source

```bash
# Build the binary
make build

# Run with example config
./opensearch-query-exporter -config configs/example-config.yaml
```

## Configuration

The exporter uses YAML configuration files. Teams can define their queries by creating YAML files with the following structure:

```yaml
# Connection settings (can be overridden by command-line flags)
opensearch_url: https://opensearch.example.com:9200
ca_cert_path: /etc/ssl/certs/opensearch-ca.pem
# insecure: true  # Skip TLS verification (testing only)

# Authentication (tries each credential until one succeeds)
credentials:
  - username: "primary_user"
    password: "primary_password"
  - username: "backup_user"
    password: "backup_password"
  # Add more credentials as needed for failover

# Queries
queries:
  - name: error_count
    team: sre
    interval: 60s
    indices: "logs-*"
    query:
      size: 0
      query:
        bool:
          must:
            - match: { level: "ERROR" }
            - range: { "@timestamp": { gte: "now-5m" } }
```

## Generated Metrics

### Automatic Metrics (per query)

- `opensearch_query_{name}_hits_total`
- `opensearch_query_{name}_took_milliseconds`
- `opensearch_query_{name}_success`
- `opensearch_query_{name}_duration_seconds`

### Cluster Metrics

- `opensearch_up`
- `opensearch_cluster_health_status` (0=green, 1=yellow, 2=red)
- `opensearch_cluster_health_nodes_total`
- `opensearch_cluster_health_shards_total`

### Aggregation Metrics

Automatically extracted from OpenSearch aggregation results with appropriate labels.

## Command-Line Options

```bash
opensearch-query-exporter [OPTIONS]

Options:
  -config string           Configuration file path (default "config.yaml")
  -listen-address string   Metrics server address (default ":9206")
  -opensearch-url string   OpenSearch URL (default "https://localhost:9200")
  -insecure               Skip TLS verification
  -timeout duration        Query timeout (default 30s)
  -log-level string        Log level (default "info")
```

## Advanced Configuration

### Custom Metric Extraction

```yaml
queries:
  - name: custom_metrics
    team: platform
    query:
      # Your OpenSearch query
    metrics:
      - name: response_time_p99
        path: aggregations.response_time.values.99.0
        help: "99th percentile response time"
        labels:
          service: "api"
        label_paths:
          region: aggregations.by_region.key
```

## Development

### Project Structure

```
.
├── cmd/exporter/          # Main application
├── pkg/
│   ├── config/           # Configuration handling
│   ├── opensearch/       # OpenSearch client
│   ├── metrics/          # Prometheus metrics collection
│   └── parser/           # Response parsing
├── configs/              # Example configurations
└── Dockerfile            # Container build
```

### Running Tests

```bash
make test
```

### Building

```bash
# Build for current platform
make build

# Build for multiple platforms
make build-all

# Build Docker image
make docker-build
```
