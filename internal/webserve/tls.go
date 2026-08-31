package webserve

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LoadOrCreateCert returns the serve certificate: the user's own files when
// given, otherwise a self-signed pair persisted next to the database — minted
// on first use and reused forever after, so its fingerprint is a stable thing
// a device can pin. The fingerprint returned is the SHA-256 of the leaf
// certificate, the same value every client shows for confirmation.
func LoadOrCreateCert(dataDir, certFile, keyFile string) (tls.Certificate, string, error) {
	if certFile == "" {
		certFile = filepath.Join(dataDir, "serve-cert.pem")
		keyFile = filepath.Join(dataDir, "serve-key.pem")
		// Either file missing means regenerate the pair: a crash between the
		// two writes must not leave an install that can never start again.
		// (The key is written first for the same reason — the cert is the
		// last thing to land, so its presence implies a complete pair.)
		_, certErr := os.Stat(certFile)
		_, keyErr := os.Stat(keyFile)
		if certErr != nil || keyErr != nil {
			if err := generateSelfSigned(certFile, keyFile); err != nil {
				return tls.Certificate{}, "", fmt.Errorf("could not create a self-signed certificate: %w", err)
			}
		}
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	return cert, Fingerprint(cert.Certificate[0]), nil
}

// Fingerprint renders a certificate's SHA-256 in the colon-separated form
// every TLS tool prints, so comparing by eye is comparing like with like.
func Fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	hexed := strings.ToUpper(hex.EncodeToString(sum[:]))
	parts := make([]string, 0, len(hexed)/2)
	for i := 0; i < len(hexed); i += 2 {
		parts = append(parts, hexed[i:i+2])
	}
	return strings.Join(parts, ":")
}

// generateSelfSigned mints a ten-year ECDSA certificate for this machine.
//
// The subject names are best-effort — the hostname, loopback, and whatever
// addresses the box holds right now. Clients that pin the fingerprint never
// look at names; the names are for the one case that can use them, a browser
// asked to trust the certificate manually.
func generateSelfSigned(certFile, keyFile string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "AgentMux", Organization: []string{"AgentMux"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}
	if host, err := os.Hostname(); err == nil && host != "" {
		tmpl.DNSNames = append(tmpl.DNSNames, host)
	}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLoopback() {
				tmpl.IPAddresses = append(tmpl.IPAddresses, ipn.IP)
			}
		}
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	// Key first: the caller treats the pair as complete when both files
	// exist, so the failure mode of a crash here is a retried generation,
	// never a cert without its key.
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		return err
	}
	return os.WriteFile(certFile, certPEM, 0o644)
}
