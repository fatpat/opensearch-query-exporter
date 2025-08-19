package parser

import (
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/sapcc/opensearch-query-exporter/pkg/config"
)

func TestExtractValueFromPath_Success(t *testing.T) {
	data := map[string]interface{}{
		"a": map[string]interface{}{
			"b": map[string]interface{}{"c": 42},
		},
	}
	v, err := extractValueFromPath(data, "a.b.c")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.(int) != 42 {
		t.Fatalf("expected 42, got %v", v)
	}
}

func TestExtractValueFromPath_NotFound(t *testing.T) {
	data := map[string]interface{}{"a": map[string]interface{}{"b": 1}}
	if _, err := extractValueFromPath(data, "a.x.c"); err == nil {
		t.Fatalf("expected error for missing key")
	}
}

func TestExtractFloat_VariousTypes(t *testing.T) {
	cases := []interface{}{float64(1.5), float32(2.5), int(3), int32(4), int64(5), uint(6), uint32(7), uint64(8)}
	for _, c := range cases {
		if _, ok := extractFloat(c); !ok {
			t.Fatalf("extractFloat failed for %T", c)
		}
	}
}

func TestSanitizeHelpers(t *testing.T) {
	if got := sanitizeMetricName("Err ors-By@Service"); got != "err_ors_by_service" {
		t.Fatalf("sanitizeMetricName got %q", got)
	}
	if got := sanitizeLabelName("Err ors-By@Service"); got != "Err_ors_By_Service" {
		t.Fatalf("sanitizeLabelName got %q", got)
	}
}

func TestParseResponse_HitsAndTook(t *testing.T) {
	resp := map[string]interface{}{
		"hits": map[string]interface{}{
			"total": map[string]interface{}{"value": 123.0},
		},
		"took": 15.0,
	}
	q := config.Query{Name: "my query", Team: "core"}
	metrics, err := ParseResponse(resp, q)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(metrics) != 2 {
		t.Fatalf("expected 2 metrics, got %d", len(metrics))
	}
	// Verify values and labels
	for _, m := range metrics {
		var metric dto.Metric
		if err := m.Write(&metric); err != nil {
			t.Fatalf("write metric: %v", err)
		}
		if metric.Gauge == nil || metric.Gauge.Value == nil {
			t.Fatalf("expected gauge metric")
		}
		// must include team label
		foundTeam := false
		for _, lp := range metric.Label {
			if lp.GetName() == "team" && lp.GetValue() == "core" {
				foundTeam = true
			}
		}
		if !foundTeam {
			t.Fatalf("expected team label 'core' in metric labels")
		}
	}
}

func TestParseResponse_MetricMappingAndAggs(t *testing.T) {
	resp := map[string]interface{}{
		"hits": map[string]interface{}{"total": map[string]interface{}{"value": 10.0}},
		"aggregations": map[string]interface{}{
			"error_count": map[string]interface{}{"value": 5.0},
			"by_service": map[string]interface{}{
				"buckets": []interface{}{
					map[string]interface{}{"key": "svc-a", "doc_count": 3.0},
					map[string]interface{}{"key": "svc-b", "doc_count": 2.0},
				},
			},
		},
		"custom": map[string]interface{}{
			"value":  7.0,
			"labels": map[string]interface{}{"dyn": "x"},
		},
	}
	q := config.Query{
		Name: "errors_by_service",
		Team: "sre",
		Metrics: []config.MetricMapping{
			{Name: "custom_metric", Path: "custom.value", LabelPaths: map[string]string{"dyn": "custom.labels.dyn"}},
		},
	}
	metrics, err := ParseResponse(resp, q)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(metrics) < 4 {
		t.Fatalf("expected at least 4 metrics (hits, custom, agg value, bucket counts), got %d", len(metrics))
	}
	// Check at least one metric contains our dynamic label
	hasDyn := false
	for _, m := range metrics {
		var metric dto.Metric
		_ = m.Write(&metric)
		for _, lp := range metric.Label {
			if lp.GetName() == "dyn" && lp.GetValue() == "x" {
				hasDyn = true
			}
		}
	}
	if !hasDyn {
		t.Fatalf("expected a metric with dynamic label dyn=x")
	}
}

