package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the main configuration structure
type Config struct {
	OpenSearchURL                string        `yaml:"opensearch_url"`
	Credentials                  []Credential  `yaml:"credentials"`
	CACertPath                   string        `yaml:"ca_cert_path"`
	Insecure                     bool          `yaml:"insecure"`
	Timeout                      time.Duration `yaml:"timeout"`
	Queries                      []Query       `yaml:"queries"`
	ClusterHealthMetricsDisabled bool          `yaml:"cluster_health_metrics_disabled"`
	OpensearchUpMetricDisabled   bool          `yaml:"opensearch_up_metric_disabled"`
	PromInternalMetricsDisabled  bool          `yaml:"prometheus_internal_metrics_disabled"`
}

// Credential represents a set of authentication credentials
type Credential struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// Query represents a team's query configuration
type Query struct {
	Name        string                 `yaml:"name"`
	Team        string                 `yaml:"team"`
	Description string                 `yaml:"description"`
	Interval    time.Duration          `yaml:"interval"`
	Indices     string                 `yaml:"indices"`
	Query       map[string]interface{} `yaml:"query"`
	Metrics     []MetricMapping        `yaml:"metrics"`
}

// MetricMapping defines how to extract metrics from query results
type MetricMapping struct {
	Name       string            `yaml:"name"`
	Path       string            `yaml:"path"`
	Labels     map[string]string `yaml:"labels"`
	LabelPaths map[string]string `yaml:"label_paths"`
	Help       string            `yaml:"help"`
}

// LoadConfig loads configuration from a YAML file
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Set defaults
	if config.OpenSearchURL == "" {
		config.OpenSearchURL = "https://localhost:9200"
	}
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}

	// Enforce HTTPS only
	if !strings.HasPrefix(strings.ToLower(config.OpenSearchURL), "https://") {
		return nil, fmt.Errorf("opensearch_url must use https scheme")
	}

	// Validate credentials
	if len(config.Credentials) == 0 {
		return nil, fmt.Errorf("at least one credential pair must be provided")
	}

	// Validate each credential
	for i, cred := range config.Credentials {
		if cred.Username == "" || cred.Password == "" {
			return nil, fmt.Errorf("credential %d: both username and password must be provided", i+1)
		}

		// Note: CA path validation is performed during client initialization.
		// If Insecure is false, a valid ca_cert_path must be provided there.
	}

	// Validate queries
	for i := range config.Queries {
		if config.Queries[i].Name == "" {
			return nil, fmt.Errorf("query #%d: name is required", i+1)
		}
		if config.Queries[i].Interval == 0 {
			config.Queries[i].Interval = 60 * time.Second
		}
		if config.Queries[i].Indices == "" {
			config.Queries[i].Indices = "_all"
		}
		if config.Queries[i].Query == nil {
			return nil, fmt.Errorf("query %s: query body is required", config.Queries[i].Name)
		}
	}

	return &config, nil
}

// LoadConfigDir loads all YAML files from a directory and merges them
func LoadConfigDir(dir string) (*Config, error) {
	// This is a simplified version - in production you might want to
	// support loading multiple config files from a directory
	return LoadConfig(dir)
}
