package main

// See tlspin.go.
//
// The point of a pin is that it says no. `InsecureSkipVerify: true` also
// connects successfully to the right server, so "it works" proves nothing at
// all -- these tests exist mostly to make it refuse.
//
// The certificate built below is deliberately shaped like the one Syncthing
// generates: a common name and one DNS SAN taken from the device name, and
// **no IP SAN**. That shape is the whole reason this needs a pin rather than
// ordinary verification, so a fixture that quietly added `127.0.0.1` would
// test a situation that never occurs.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
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
	"strings"
	"testing"
	"time"
)

// syncthingShapedCert returns a self-signed certificate carrying name as its
// common name and only DNS SAN, plus its PEM encoding.
func syncthingShapedCert(t *testing.T, name string) (tls.Certificate, []byte) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(42),
		Subject: pkix.Name{
			Organization:       []string{"Syncthing"},
			OrganizationalUnit: []string{"Automatically Generated"},
			CommonName:         name,
		},
		DNSNames:  []string{name},
		NotBefore: time.Now().Add(-time.Hour),
		NotAfter:  time.Now().Add(24 * time.Hour),
		KeyUsage:  x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
		// Deliberately not a CA, matching what Syncthing writes. A pin has to
		// work against a bare leaf.
		BasicConstraintsValid: true,
		IsCA:                  false,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  key,
		Leaf:        leaf,
	}, certPEM
}

// homeWith writes a cert into a temporary home directory, as Syncthing would.
func homeWith(t *testing.T, certPEM []byte) string {
	t.Helper()
	home := t.TempDir()
	if certPEM != nil {
		if err := os.WriteFile(filepath.Join(home, httpsCertFile), certPEM, 0o600); err != nil {
			t.Fatalf("write cert: %v", err)
		}
	}
	return home
}

// serveTLS starts a loopback HTTPS server presenting the given certificate.
func serveTLS(t *testing.T, cert tls.Certificate) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

func TestPinAcceptsTheCertificateItWasGiven(t *testing.T) {
	t.Parallel()

	cert, certPEM := syncthingShapedCert(t, "desuq")
	home := homeWith(t, certPEM)
	srv := serveTLS(t, cert)

	cfg, err := loopbackTLS(home)
	if err != nil {
		t.Fatalf("loopbackTLS: %v", err)
	}

	// The address dialled is 127.0.0.1 and the certificate carries no IP SAN,
	// so this only succeeds because ServerName is taken from the certificate.
	// It is the case ordinary verification cannot handle.
	if cfg.ServerName != "desuq" {
		t.Fatalf("ServerName = %q, want the certificate's own name", cfg.ServerName)
	}

	resp, err := (&http.Client{
		Transport: &http.Transport{TLSClientConfig: cfg},
		Timeout:   5 * time.Second,
	}).Get(srv.URL)
	if err != nil {
		t.Fatalf("pinned client could not reach its own server: %v", err)
	}
	resp.Body.Close()
	if _, _, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "https://")); err != nil {
		t.Fatalf("expected a host:port URL, got %q", srv.URL)
	}
}

func TestPinRefusesADifferentCertificate(t *testing.T) {
	t.Parallel()

	// The home directory holds the real certificate; the server presents a
	// different one carrying the same name. This is the attack the old
	// InsecureSkipVerify allowed outright: something else answering on
	// loopback and being handed the API key, which is full control of every
	// folder Syncthing manages.
	_, realPEM := syncthingShapedCert(t, "desuq")
	imposter, _ := syncthingShapedCert(t, "desuq")

	home := homeWith(t, realPEM)
	srv := serveTLS(t, imposter)

	cfg, err := loopbackTLS(home)
	if err != nil {
		t.Fatalf("loopbackTLS: %v", err)
	}

	resp, err := (&http.Client{
		Transport: &http.Transport{TLSClientConfig: cfg},
		Timeout:   5 * time.Second,
	}).Get(srv.URL)
	if err == nil {
		resp.Body.Close()
		t.Fatal("the pin accepted a certificate it had never seen")
	}
	if !strings.Contains(err.Error(), "certificate") {
		t.Errorf("expected a certificate error, got: %v", err)
	}
}

func TestPinRefusesWhenTheCertificateIsMissingOrJunk(t *testing.T) {
	t.Parallel()

	// A missing file must be an error rather than a silently unverified
	// connection -- falling back to "no TLS config" here is exactly the bug
	// this change removes.
	if _, err := loopbackTLS(homeWith(t, nil)); err == nil {
		t.Error("a missing certificate should be an error")
	}

	junk := homeWith(t, []byte("this is not a certificate\n"))
	if _, err := loopbackTLS(junk); err == nil {
		t.Error("an unparseable certificate should be an error")
	}
}

func TestCertServerNamePrefersTheSAN(t *testing.T) {
	t.Parallel()

	_, certPEM := syncthingShapedCert(t, "some-laptop")
	name, err := certServerName(certPEM)
	if err != nil {
		t.Fatalf("certServerName: %v", err)
	}
	if name != "some-laptop" {
		t.Errorf("got %q, want the DNS SAN", name)
	}

	if _, err := certServerName([]byte("-----BEGIN RSA PRIVATE KEY-----\nnope\n-----END RSA PRIVATE KEY-----\n")); err == nil {
		t.Error("a PEM file with no CERTIFICATE block should be an error")
	}
}
