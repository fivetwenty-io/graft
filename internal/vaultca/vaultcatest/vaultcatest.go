// Package vaultcatest builds throwaway certificate authorities and fake
// Vault servers that serve TLS with a certificate one of them signs, so
// tests can prove which roots a vault client trusts. Only tests import
// it.
package vaultcatest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// CA is a self-signed certificate authority made for one test.
type CA struct {
	// Cert is the parsed CA certificate.
	Cert *x509.Certificate
	// PEM is Cert encoded as a single PEM CERTIFICATE block.
	PEM []byte

	key *ecdsa.PrivateKey
}

// NewCA generates a fresh ECDSA P-256 certificate authority named name.
func NewCA(t testing.TB, name string) *CA {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating CA key: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber:          serial(t),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating CA certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parsing CA certificate: %v", err)
	}

	return &CA{
		Cert: cert,
		PEM:  pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		key:  key,
	}
}

// Pool returns a pool that holds the given CAs and nothing else.
func Pool(cas ...*CA) *x509.CertPool {
	pool := x509.NewCertPool()
	for _, ca := range cas {
		pool.AddCert(ca.Cert)
	}

	return pool
}

// Server starts a TLS server for 127.0.0.1, ::1, and localhost whose
// certificate ca signs. It answers KVHandler(path, key, value) and is
// closed when the test ends.
func (ca *CA) Server(t testing.TB, path, key, value string) *httptest.Server {
	t.Helper()

	return ca.Serve(t, KVHandler(path, key, value))
}

// Serve is Server with a caller-supplied handler.
func (ca *CA) Serve(t testing.TB, h http.Handler) *httptest.Server {
	t.Helper()

	srv := httptest.NewUnstartedServer(h)
	srv.TLS = &tls.Config{
		Certificates: []tls.Certificate{ca.leaf(t)},
		MinVersion:   tls.VersionTLS12,
	}
	Quiet(srv)
	srv.StartTLS()
	t.Cleanup(srv.Close)

	return srv
}

// Quiet discards srv's error log, so the handshake failures a test
// expects do not clutter its output. Call it before starting srv.
func Quiet(srv *httptest.Server) {
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
}

// KVHandler answers a KV v1 read of path (such as "secret/app") with
// {"key": value} and returns 404 for every other request, including the
// KV mount lookup, which makes the vault reader assume KV v1.
func KVHandler(path, key, value string) http.Handler {
	body, err := json.Marshal(map[string]any{
		"data": map[string]string{key: value},
	})
	if err != nil {
		panic(err)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/"+path || r.Header.Get("X-Vault-Token") == "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
}

// WriteFile writes data to dir/name, creating any parent directories, and
// returns the file's path.
func WriteFile(t testing.TB, dir, name string, data []byte) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("creating %s: %v", filepath.Dir(path), err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}

	return path
}

// leaf issues a server certificate signed by ca.
func (ca *CA) leaf(t testing.TB) tls.Certificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating server key: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial(t),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.Cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatalf("creating server certificate: %v", err)
	}

	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func serial(t testing.TB) *big.Int {
	t.Helper()

	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
	if err != nil {
		t.Fatalf("generating serial number: %v", err)
	}

	return n
}
