package opensearch

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/sapcc/opensearch-query-exporter/pkg/config"
)

// Client represents an OpenSearch client
type Client struct {
	baseURL     string
	httpClient  *http.Client
	credentials []config.Credential
}

// NewClient creates a new OpenSearch client
func NewClient(cfg *config.Config) (*Client, error) {
	// Build TLS config based on insecure flag or provided CA
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if cfg.Insecure {
		tlsConfig.InsecureSkipVerify = true
	} else {
		caCertPool := x509.NewCertPool()
		caCert, err := os.ReadFile(cfg.CACertPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate: %w", err)
		}
		if ok := caCertPool.AppendCertsFromPEM(caCert); !ok {
			return nil, fmt.Errorf("failed to append CA certificate from %s", cfg.CACertPath)
		}
		tlsConfig.RootCAs = caCertPool
	}

	transport := &http.Transport{TLSClientConfig: tlsConfig}

	client := &Client{
		baseURL: strings.TrimRight(cfg.OpenSearchURL, "/"),
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   cfg.Timeout,
		},
		credentials: cfg.Credentials,
	}

	return client, nil
}

// Ping checks if the OpenSearch cluster is reachable
func (c *Client) Ping(ctx context.Context) error {
	resp, err := c.executeWithFailover(ctx, "GET", "/", nil)
	if err != nil {
		return fmt.Errorf("failed to ping OpenSearch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("OpenSearch returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// Search executes a search query
func (c *Client) Search(ctx context.Context, indices string, query map[string]interface{}) (map[string]interface{}, error) {
	path := fmt.Sprintf("/%s/_search", indices)

	body, err := json.Marshal(query)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal query: %w", err)
	}

	resp, err := c.executeWithFailover(ctx, "POST", path, body)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search returned status %d: %s", resp.StatusCode, string(responseBody))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return result, nil
}

// ClusterHealth returns cluster health information
func (c *Client) ClusterHealth(ctx context.Context) (map[string]interface{}, error) {
	resp, err := c.executeWithFailover(ctx, "GET", "/_cluster/health", nil)
	if err != nil {
		return nil, fmt.Errorf("cluster health request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("cluster health returned status %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse cluster health: %w", err)
	}

	return result, nil
}

// NodesStats returns nodes statistics
func (c *Client) NodesStats(ctx context.Context) (map[string]interface{}, error) {
	resp, err := c.executeWithFailover(ctx, "GET", "/_nodes/stats", nil)
	if err != nil {
		return nil, fmt.Errorf("nodes stats request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("nodes stats returned status %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse nodes stats: %w", err)
	}

	return result, nil
}

// IndicesStats returns indices statistics
func (c *Client) IndicesStats(ctx context.Context) (map[string]interface{}, error) {
	resp, err := c.executeWithFailover(ctx, "GET", "/_stats", nil)
	if err != nil {
		return nil, fmt.Errorf("indices stats request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("indices stats returned status %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse indices stats: %w", err)
	}

	return result, nil
}

// newRequest creates a new HTTP request without authentication
func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	return req, nil
}

// executeWithFailover executes a request trying each credential until one succeeds
func (c *Client) executeWithFailover(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	var lastErr error

	// Try each credential
	for i, cred := range c.credentials {
		var bodyReader io.Reader
		if body != nil {
			bodyReader = bytes.NewReader(body)
		}

		req, err := c.newRequest(ctx, method, path, bodyReader)
		if err != nil {
			return nil, err
		}

		// Set authentication (always required)
		req.SetBasicAuth(cred.Username, cred.Password)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("credential %d: %w", i+1, err)
			continue
		}

		// Check if authentication succeeded
		if resp.StatusCode == http.StatusUnauthorized {
			resp.Body.Close()
			lastErr = fmt.Errorf("credential %d: authentication failed", i+1)
			continue
		}

		// Authentication succeeded or error is not auth-related
		return resp, nil
	}

	// All credentials failed
	if lastErr != nil {
		return nil, fmt.Errorf("all credentials failed: %w", lastErr)
	}
	return nil, fmt.Errorf("no credentials configured")
}
