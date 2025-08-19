package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempConfig(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}
	return path
}

func TestLoadConfig_Success_Insecure(t *testing.T) {
	yaml := `
opensearch_url: https://localhost:9200
credentials:
  - username: user
    password: pass
insecure: true
timeout: 5s
queries:
  - name: q1
    team: t1
    description: test
    query:
      size: 0
      query:
        match_all: {}
`
	path := writeTempConfig(t, yaml)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if cfg.OpenSearchURL == "" || len(cfg.Credentials) != 1 || !cfg.Insecure {
		t.Fatalf("unexpected cfg: %+v", cfg)
	}
}

func TestLoadConfig_Fail_HTTP_URL(t *testing.T) {
	yaml := `
opensearch_url: http://localhost:9200
credentials:
  - username: user
    password: pass
insecure: true
queries:
  - name: q1
    team: t1
    description: test
    query:
      size: 0
      query:
        match_all: {}
`
	path := writeTempConfig(t, yaml)
	if _, err := LoadConfig(path); err == nil {
		t.Fatalf("expected error for non-https opensearch_url")
	}
}

func TestLoadConfig_Fail_NoCredentials(t *testing.T) {
	yaml := `
opensearch_url: https://localhost:9200
insecure: true
queries:
  - name: q1
    team: t1
    description: test
    query:
      size: 0
      query:
        match_all: {}
`
	path := writeTempConfig(t, yaml)
	if _, err := LoadConfig(path); err == nil {
		t.Fatalf("expected error for missing credentials")
	}
}

func TestLoadConfig_Fail_EmptyCredentialFields(t *testing.T) {
	yaml := `
opensearch_url: https://localhost:9200
credentials:
  - username: ""
    password: pass
insecure: true
queries:
  - name: q1
    team: t1
    description: test
    query:
      size: 0
      query:
        match_all: {}
`
	path := writeTempConfig(t, yaml)
	if _, err := LoadConfig(path); err == nil {
		t.Fatalf("expected error for empty username")
	}
}
