package main

// Talking to Syncthing's GUI over HTTPS without turning verification off.
//
// WHAT WAS HERE BEFORE
//
// `&tls.Config{InsecureSkipVerify: true}`, with a comment explaining that the
// GUI certificate is self-signed and regenerated per install so "there is
// nothing to pin it against", and that loopback has no meaningful attacker in
// the path anyway.
//
// The first half was simply wrong. The certificate is sitting in the same
// directory as the config.xml the tray already reads -- `https-cert.pem` --
// so there is exactly one certificate it should ever accept, and the tray
// knows where to find it.
//
// The second half is weaker than it sounds. Loopback is not a private channel
// on a multi-user machine, and "anything that answers on this port is
// trusted" hands the tray's API key to whatever wins a race for it. The API
// key is full control of every folder Syncthing manages.
//
// WHY THIS IS A PIN AND NOT ORDINARY VERIFICATION
//
// Syncthing generates that certificate with the device name as its common
// name and a single DNS SAN to match -- `CN=desuq, DNS:desuq` on the machine
// this was written for. **There is no IP SAN.** So a connection to
// `https://127.0.0.1:8384` can never satisfy hostname verification no matter
// what is in the trust store, which is presumably how the InsecureSkipVerify
// got there in the first place.
//
// The fix is to supply both halves from the certificate itself: trust it as
// its own root, and verify against the name it actually carries rather than
// the address dialled. That is a pin -- exactly one certificate is acceptable
// -- while still running Go's full verification rather than switching it off.
//
// WHEN THIS RUNS AT ALL
//
// Only when the GUI has TLS switched on, which is not the default and not
// what this fork seeds. On a normal install the tray talks plain HTTP to
// loopback and no TLS configuration is built at all -- which is the real
// reason the old code was harmless in practice, and a bad reason to leave it
// there.

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// httpsCertFile is where Syncthing keeps the GUI certificate, beside the
// config.xml the tray already reads out of the same directory.
const httpsCertFile = "https-cert.pem"

// loopbackTLS builds a client TLS configuration that accepts precisely the
// certificate Syncthing generated for this home directory, and nothing else.
func loopbackTLS(home string) (*tls.Config, error) {
	// Not named `pem`: that shadows the encoding/pem package this file uses.
	pemBytes, err := os.ReadFile(filepath.Join(home, httpsCertFile))
	if err != nil {
		return nil, fmt.Errorf("read GUI certificate: %w", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemBytes) {
		return nil, errors.New("GUI certificate is not readable as PEM")
	}

	name, err := certServerName(pemBytes)
	if err != nil {
		return nil, err
	}

	return &tls.Config{
		// The certificate is self-signed, so it is its own root. A pool of
		// one means one acceptable certificate.
		RootCAs: pool,
		// Verify against the name the certificate carries, not the loopback
		// address dialled -- there is no IP SAN to match against. Without
		// this, every connection fails hostname verification.
		ServerName: name,
		MinVersion: tls.VersionTLS12,
	}, nil
}

// certServerName returns the name to verify the certificate against: its first
// DNS SAN, falling back to the common name.
//
// The fallback is very unlikely to be used and is deliberately kept anyway.
// Go stopped honouring the common name for hostname verification in 1.15, so
// a certificate with no SAN at all cannot be verified whatever is passed here
// -- but returning the common name produces "certificate is not valid for
// desuq", which says what is wrong, where returning "" produces the far less
// helpful complaint that no server name was supplied.
func certServerName(pemBytes []byte) (string, error) {
	block, err := firstCertBlock(pemBytes)
	if err != nil {
		return "", err
	}
	cert, err := x509.ParseCertificate(block)
	if err != nil {
		return "", fmt.Errorf("parse GUI certificate: %w", err)
	}
	if len(cert.DNSNames) > 0 {
		return cert.DNSNames[0], nil
	}
	if cert.Subject.CommonName != "" {
		return cert.Subject.CommonName, nil
	}
	return "", errors.New("GUI certificate carries no usable name")
}

// firstCertBlock returns the DER of the first CERTIFICATE block in a PEM file,
// skipping anything else that may be in there.
func firstCertBlock(pemBytes []byte) ([]byte, error) {
	rest := pemBytes
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			return nil, errors.New("GUI certificate contains no CERTIFICATE block")
		}
		if block.Type == "CERTIFICATE" {
			return block.Bytes, nil
		}
	}
}
