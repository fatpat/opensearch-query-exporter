package metrics

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/opensearch-query-exporter/pkg/config"
	"github.com/sapcc/opensearch-query-exporter/pkg/opensearch"
	"github.com/sapcc/opensearch-query-exporter/pkg/parser"
)

// Collector implements the prometheus.Collector interface
type Collector struct {
	client *opensearch.Client
	config *config.Config

	// Metrics
	up                  *prometheus.Desc
	queryDuration       *prometheus.Desc
	querySuccess        *prometheus.Desc
	clusterHealthStatus *prometheus.Desc
	clusterHealthNodes  *prometheus.Desc
	clusterHealthShards *prometheus.Desc

	// Query results cache
	queryResults map[string]*queryResult
	resultsMutex sync.RWMutex

	// Background query execution
	stopChan chan struct{}
	wg       sync.WaitGroup
}

type queryResult struct {
	metrics   []prometheus.Metric
	timestamp time.Time
	err       error
}

// NewCollector creates a new metrics collector
func NewCollector(client *opensearch.Client, cfg *config.Config) *Collector {
	c := &Collector{
		client:       client,
		config:       cfg,
		queryResults: make(map[string]*queryResult),
		stopChan:     make(chan struct{}),

		up: prometheus.NewDesc(
			"opensearch_up",
			"Whether the OpenSearch cluster is reachable",
			nil, nil,
		),
		queryDuration: prometheus.NewDesc(
			"opensearch_query_duration_seconds",
			"Duration of the query in seconds",
			[]string{"query", "team"}, nil,
		),
		querySuccess: prometheus.NewDesc(
			"opensearch_query_success",
			"Whether the query was successful",
			[]string{"query", "team"}, nil,
		),
		clusterHealthStatus: prometheus.NewDesc(
			"opensearch_cluster_health_status",
			"Cluster health status (0=green, 1=yellow, 2=red)",
			[]string{"cluster"}, nil,
		),
		clusterHealthNodes: prometheus.NewDesc(
			"opensearch_cluster_health_nodes_total",
			"Total number of nodes in the cluster",
			[]string{"cluster"}, nil,
		),
		clusterHealthShards: prometheus.NewDesc(
			"opensearch_cluster_health_shards_total",
			"Total number of shards in the cluster",
			[]string{"cluster", "type"}, nil,
		),
	}

	// Start background query execution
	c.startQueryExecutors()

	return c
}

// Describe implements prometheus.Collector
func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.up
	ch <- c.queryDuration
	ch <- c.querySuccess
	ch <- c.clusterHealthStatus
	ch <- c.clusterHealthNodes
	ch <- c.clusterHealthShards
}

// Collect implements prometheus.Collector
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	// Check if OpenSearch is up
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.client.Ping(ctx); err != nil {
		if !c.config.OpensearchUpMetricDisabled {
			ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 0)
		}
		log.Printf("OpenSearch ping failed: %v", err)
		return
	}
	if !c.config.OpensearchUpMetricDisabled {
		ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 1)
	}

	// Collect cluster health metrics
	if !c.config.ClusterHealthMetricsDisabled {
		c.collectClusterHealth(ctx, ch)
	}

	// Collect query results
	c.collectQueryResults(ch)
}

func (c *Collector) collectClusterHealth(ctx context.Context, ch chan<- prometheus.Metric) {
	health, err := c.client.ClusterHealth(ctx)
	if err != nil {
		log.Printf("Failed to get cluster health: %v", err)
		return
	}

	// Extract cluster name
	clusterName, _ := health["cluster_name"].(string)
	if clusterName == "" {
		clusterName = "unknown"
	}

	// Health status
	status, _ := health["status"].(string)
	statusValue := float64(2) // default to red
	switch status {
	case "green":
		statusValue = 0
	case "yellow":
		statusValue = 1
	}
	ch <- prometheus.MustNewConstMetric(c.clusterHealthStatus, prometheus.GaugeValue, statusValue, clusterName)

	// Number of nodes
	if nodes, ok := health["number_of_nodes"].(float64); ok {
		ch <- prometheus.MustNewConstMetric(c.clusterHealthNodes, prometheus.GaugeValue, nodes, clusterName)
	}

	// Shards
	if shards, ok := health["active_primary_shards"].(float64); ok {
		ch <- prometheus.MustNewConstMetric(c.clusterHealthShards, prometheus.GaugeValue, shards, clusterName, "primary")
	}
	if shards, ok := health["active_shards"].(float64); ok {
		ch <- prometheus.MustNewConstMetric(c.clusterHealthShards, prometheus.GaugeValue, shards, clusterName, "active")
	}
}

func (c *Collector) collectQueryResults(ch chan<- prometheus.Metric) {
	c.resultsMutex.RLock()
	defer c.resultsMutex.RUnlock()

	for queryName, result := range c.queryResults {
		if result.err != nil {
			// Find the query config to get team name
			var teamName string
			for _, q := range c.config.Queries {
				if q.Name == queryName {
					teamName = q.Team
					break
				}
			}
			ch <- prometheus.MustNewConstMetric(c.querySuccess, prometheus.GaugeValue, 0, queryName, teamName)
		} else {
			for _, metric := range result.metrics {
				ch <- metric
			}
		}
	}
}

func (c *Collector) startQueryExecutors() {
	for _, query := range c.config.Queries {
		c.wg.Add(1)
		go c.executeQueryPeriodically(query)
	}
}

func (c *Collector) executeQueryPeriodically(query config.Query) {
	defer c.wg.Done()

	// Execute immediately
	c.executeQuery(query)

	ticker := time.NewTicker(query.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.executeQuery(query)
		case <-c.stopChan:
			return
		}
	}
}

func (c *Collector) executeQuery(query config.Query) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), c.config.Timeout)
	defer cancel()

	log.Printf("Executing query %s for team %s", query.Name, query.Team)

	// Execute the search
	response, err := c.client.Search(ctx, query.Indices, query.Query)
	duration := time.Since(start).Seconds()

	result := &queryResult{
		timestamp: time.Now(),
		err:       err,
	}

	if err != nil {
		log.Printf("Query %s failed: %v", query.Name, err)
	} else {
		// Parse the response and extract metrics
		metrics, err := parser.ParseResponse(response, query)
		if err != nil {
			log.Printf("Failed to parse response for query %s: %v", query.Name, err)
			result.err = err
		} else {
			result.metrics = metrics
			// Add query metadata metrics
			result.metrics = append(result.metrics,
				prometheus.MustNewConstMetric(c.queryDuration, prometheus.GaugeValue, duration, query.Name, query.Team),
				prometheus.MustNewConstMetric(c.querySuccess, prometheus.GaugeValue, 1, query.Name, query.Team),
			)
		}
	}

	// Store the result
	c.resultsMutex.Lock()
	c.queryResults[query.Name] = result
	c.resultsMutex.Unlock()
}

// Stop gracefully stops the collector
func (c *Collector) Stop() {
	close(c.stopChan)
	c.wg.Wait()
}
