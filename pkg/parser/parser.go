package parser

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/opensearch-query-exporter/pkg/config"
)

// ParseResponse parses an OpenSearch response and extracts metrics based on the query configuration
func ParseResponse(response map[string]interface{}, query config.Query) ([]prometheus.Metric, error) {
	var metrics []prometheus.Metric

	// Parse hits.total
	if hits, ok := response["hits"].(map[string]interface{}); ok {
		if total := extractHitsTotal(hits); total >= 0 {
			desc := prometheus.NewDesc(
				fmt.Sprintf("opensearch_query_%s_hits_total", sanitizeMetricName(query.Name)),
				"Total number of hits for the query",
				[]string{"team"}, nil,
			)
			metrics = append(metrics, prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, total, query.Team))
		}
	}

	// Parse took time
	if took, ok := extractFloat(response["took"]); ok {
		desc := prometheus.NewDesc(
			fmt.Sprintf("opensearch_query_%s_took_milliseconds", sanitizeMetricName(query.Name)),
			"Time taken for the query in milliseconds",
			[]string{"team"}, nil,
		)
		metrics = append(metrics, prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, took, query.Team))
	}

	// Parse configured metrics
	for _, metricConfig := range query.Metrics {
		metric, err := extractMetric(response, query, metricConfig)
		if err != nil {
			// Log but don't fail the entire parsing
			continue
		}
		if metric != nil {
			metrics = append(metrics, metric)
		}
	}

	// Parse aggregations if present
	if aggs, ok := response["aggregations"].(map[string]interface{}); ok {
		aggMetrics := parseAggregations(aggs, query.Name, query.Team, nil)
		metrics = append(metrics, aggMetrics...)
	}

	return metrics, nil
}

func extractHitsTotal(hits map[string]interface{}) float64 {
	// OpenSearch 2.x format
	if total, ok := hits["total"].(map[string]interface{}); ok {
		if value, ok := extractFloat(total["value"]); ok {
			return value
		}
	}
	// Legacy format
	if total, ok := extractFloat(hits["total"]); ok {
		return total
	}
	return -1
}

func extractMetric(response map[string]interface{}, query config.Query, metricConfig config.MetricMapping) (prometheus.Metric, error) {
	// Extract value from path
	value, err := extractValueFromPath(response, metricConfig.Path)
	if err != nil {
		return nil, err
	}

	floatValue, ok := extractFloat(value)
	if !ok {
		return nil, fmt.Errorf("path %s does not resolve to a numeric value", metricConfig.Path)
	}

	// Prepare labels
	labels := make(map[string]string)
	labelNames := []string{"team"}
	labelValues := []string{query.Team}

	// Add static labels
	for k, v := range metricConfig.Labels {
		labels[k] = v
		labelNames = append(labelNames, k)
		labelValues = append(labelValues, v)
	}

	// Add dynamic labels from paths
	for labelName, labelPath := range metricConfig.LabelPaths {
		labelValue, err := extractValueFromPath(response, labelPath)
		if err == nil {
			labelNames = append(labelNames, labelName)
			labelValues = append(labelValues, fmt.Sprintf("%v", labelValue))
		}
	}

	// Create metric
	metricName := fmt.Sprintf("opensearch_query_%s_%s", sanitizeMetricName(query.Name), sanitizeMetricName(metricConfig.Name))
	help := metricConfig.Help
	if help == "" {
		help = fmt.Sprintf("Metric %s from query %s", metricConfig.Name, query.Name)
	}

	desc := prometheus.NewDesc(metricName, help, labelNames, nil)
	return prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, floatValue, labelValues...), nil
}

func parseAggregations(aggs map[string]interface{}, queryName, teamName string, parentLabels map[string]string) []prometheus.Metric {
	var metrics []prometheus.Metric

	for aggName, aggData := range aggs {
		if aggMap, ok := aggData.(map[string]interface{}); ok {
			// Handle bucket aggregations
			if buckets, ok := aggMap["buckets"].([]interface{}); ok {
				for _, bucket := range buckets {
					if bucketMap, ok := bucket.(map[string]interface{}); ok {
						labels := make(map[string]string)
						for k, v := range parentLabels {
							labels[k] = v
						}

						// Extract bucket key
						if key, ok := bucketMap["key"]; ok {
							labels[sanitizeLabelName(aggName)] = fmt.Sprintf("%v", key)
						}

						// Extract metrics from bucket
						bucketMetrics := extractBucketMetrics(bucketMap, queryName, teamName, labels)
						metrics = append(metrics, bucketMetrics...)

						// Recursively parse sub-aggregations
						subMetrics := parseAggregations(bucketMap, queryName, teamName, labels)
						metrics = append(metrics, subMetrics...)
					}
				}
			} else {
				// Handle metric aggregations (value-based)
				if value, ok := extractFloat(aggMap["value"]); ok {
					labelNames := []string{"team"}
					labelValues := []string{teamName}

					for k, v := range parentLabels {
						labelNames = append(labelNames, k)
						labelValues = append(labelValues, v)
					}

					metricName := fmt.Sprintf("opensearch_query_%s_%s", sanitizeMetricName(queryName), sanitizeMetricName(aggName))
					desc := prometheus.NewDesc(metricName, fmt.Sprintf("Aggregation %s from query %s", aggName, queryName), labelNames, nil)
					metrics = append(metrics, prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, value, labelValues...))
				}
			}
		}
	}

	return metrics
}

func extractBucketMetrics(bucket map[string]interface{}, queryName, teamName string, labels map[string]string) []prometheus.Metric {
	var metrics []prometheus.Metric

	// Extract doc_count
	if docCount, ok := extractFloat(bucket["doc_count"]); ok {
		labelNames := []string{"team"}
		labelValues := []string{teamName}

		for k, v := range labels {
			labelNames = append(labelNames, k)
			labelValues = append(labelValues, v)
		}

		metricName := fmt.Sprintf("opensearch_query_%s_doc_count", sanitizeMetricName(queryName))
		desc := prometheus.NewDesc(metricName, fmt.Sprintf("Document count from query %s", queryName), labelNames, nil)
		metrics = append(metrics, prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, docCount, labelValues...))
	}

	return metrics
}

func extractValueFromPath(data interface{}, path string) (interface{}, error) {
	parts := strings.Split(path, ".")
	current := data

	for _, part := range parts {
		switch v := current.(type) {
		case map[string]interface{}:
			var ok bool
			current, ok = v[part]
			if !ok {
				return nil, fmt.Errorf("key %s not found in path %s", part, path)
			}
		default:
			return nil, fmt.Errorf("cannot navigate path %s: %s is not a map", path, part)
		}
	}

	return current, nil
}

func extractFloat(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	default:
		// Try reflection for other numeric types
		rv := reflect.ValueOf(value)
		switch rv.Kind() {
		case reflect.Float32, reflect.Float64:
			return rv.Float(), true
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return float64(rv.Int()), true
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return float64(rv.Uint()), true
		}
	}
	return 0, false
}

func sanitizeMetricName(name string) string {
	// Replace non-alphanumeric characters with underscores
	var result strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			result.WriteRune(r)
		} else {
			result.WriteRune('_')
		}
	}
	return strings.ToLower(result.String())
}

func sanitizeLabelName(name string) string {
	// Similar to metric name but preserve case for labels
	var result strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			result.WriteRune(r)
		} else {
			result.WriteRune('_')
		}
	}
	return result.String()
}
