package opensearch

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sapcc/opensearch-query-exporter/pkg/config"
)

// generateSelfSignedCert returns PEM-encoded cert and key bytes.
func generateSelfSignedCert(t *testing.T, host string) ([]byte, []byte) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: host},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	if ip := net.ParseIP(host); ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	} else {
		tmpl.DNSNames = []string{host}
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
	return certPEM, keyPEM
}

func TestClient_TLS_WithCACertAndFailover(t *testing.T) {
	// Start HTTPS server with self-signed cert
	certPEM, keyPEM := generateSelfSignedCert(t, "127.0.0.1")
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		u, p, _ := r.BasicAuth()
		if u == "good" && p == "pwd" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("ok"))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	})

	srv := httptest.NewUnstartedServer(mux)
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv.Listener = ln
	srv.StartTLS()
	t.Cleanup(func() { srv.Close() })

	// Write CA to temp file (server cert acts as CA for this test)
	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(caPath, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		OpenSearchURL: "https://" + srv.Listener.Addr().String(),
		Credentials: []config.Credential{
			{Username: "bad", Password: "bad"},
			{Username: "good", Password: "pwd"},
		},
		CACertPath:      caPath,
		Timeout:         5 * time.Second,
		GlobalPrefix:    "opensearch_",
		QueryNamePrefix: "opensearch_query_",
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	// Should succeed via failover to second credential
	if err := client.Ping(t.Context()); err != nil {
		t.Fatalf("ping failed: %v", err)
	}
}

func TestClient_TLS_InsecureSkipsVerify(t *testing.T) {
	// HTTPS server with self-signed cert, no CA provided, but insecure enabled
	certPEM, keyPEM := generateSelfSignedCert(t, "127.0.0.1")
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv.Listener = ln
	srv.StartTLS()
	t.Cleanup(func() { srv.Close() })

	cfg := &config.Config{
		OpenSearchURL:   "https://" + srv.Listener.Addr().String(),
		Credentials:     []config.Credential{{Username: "u", Password: "p"}},
		Insecure:        true,
		Timeout:         5 * time.Second,
		GlobalPrefix:    "opensearch_",
		QueryNamePrefix: "opensearch_query_",
	}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if err := client.Ping(t.Context()); err != nil {
		t.Fatalf("ping failed with insecure: %v", err)
	}
}

func TestClient_Search_And_Health_Endpoints(t *testing.T) {
	// HTTPS server requiring auth and serving JSON endpoints
	certPEM, keyPEM := generateSelfSignedCert(t, "127.0.0.1")
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := r.BasicAuth(); !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/_cluster/health", func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := r.BasicAuth(); !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"cluster_name":"test","status":"green","number_of_nodes":2,"active_primary_shards":1,"active_shards":2}`))
	})
	mux.HandleFunc("/_nodes/stats", func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := r.BasicAuth(); !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"_nodes":{"total":1}}`))
	})
	mux.HandleFunc("/_stats", func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := r.BasicAuth(); !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"_all":{"primaries":{}}}`))
	})
	mux.HandleFunc("/idx/_search", func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := r.BasicAuth(); !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"took":1,"hits":{"total":{"value":0}}}`))
	})

	srv := httptest.NewUnstartedServer(mux)
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv.Listener = ln
	srv.StartTLS()
	t.Cleanup(func() { srv.Close() })

	cfg := &config.Config{
		OpenSearchURL:   "https://" + srv.Listener.Addr().String(),
		Credentials:     []config.Credential{{Username: "u", Password: "p"}},
		Insecure:        true,
		Timeout:         3 * time.Second,
		GlobalPrefix:    "opensearch_",
		QueryNamePrefix: "opensearch_query_",
	}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	if err := client.Ping(t.Context()); err != nil {
		t.Fatalf("ping: %v", err)
	}

	if _, err := client.ClusterHealth(t.Context()); err != nil {
		t.Fatalf("cluster health: %v", err)
	}
	if _, err := client.NodesStats(t.Context()); err != nil {
		t.Fatalf("nodes stats: %v", err)
	}
	if _, err := client.IndicesStats(t.Context()); err != nil {
		t.Fatalf("indices stats: %v", err)
	}

	q := map[string]interface{}{"size": 0}
	if _, err := client.Search(t.Context(), "idx", q); err != nil {
		t.Fatalf("search: %v", err)
	}
}

func TestClient_Search_FailoverOnUnauthorized(t *testing.T) {
	// First credential unauthorized, second authorized
	certPEM, keyPEM := generateSelfSignedCert(t, "127.0.0.1")
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/idx/_search", func(w http.ResponseWriter, r *http.Request) {
		u, p, _ := r.BasicAuth()
		if u == "bad" && p == "bad" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"took":1,"hits":{"total":{"value":0}}}`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	srv := httptest.NewUnstartedServer(mux)
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv.Listener = ln
	srv.StartTLS()
	t.Cleanup(func() { srv.Close() })

	cfg := &config.Config{
		OpenSearchURL:   "https://" + srv.Listener.Addr().String(),
		Credentials:     []config.Credential{{Username: "bad", Password: "bad"}, {Username: "u", Password: "p"}},
		Insecure:        true,
		Timeout:         3 * time.Second,
		GlobalPrefix:    "opensearch_",
		QueryNamePrefix: "opensearch_query_",
	}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if _, err := client.Search(t.Context(), "idx", map[string]interface{}{"size": 0}); err != nil {
		t.Fatalf("search with failover should succeed: %v", err)
	}
}
