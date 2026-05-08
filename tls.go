package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"log"
	"math/big"
	"net"
	"os"
	"time"
)

// ensureTLSCerts generates a self-signed cert+key if they don't exist.
func ensureTLSCerts(certFile, keyFile, host string) {
	if _, err := os.Stat(certFile); err == nil {
		if _, err := os.Stat(keyFile); err == nil {
			return // both exist, use them
		}
	}

	log.Printf("generating self-signed TLS cert for %s", host)

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatalf("failed to generate TLS key: %v", err)
	}

	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{Organization: []string{"anthropic-model-rewrite-proxy"}},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	if ip := net.ParseIP(host); ip != nil {
		tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
	} else {
		tmpl.DNSNames = append(tmpl.DNSNames, host)
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		log.Fatalf("failed to create TLS cert: %v", err)
	}

	cf, _ := os.Create(certFile)
	defer cf.Close()
	pem.Encode(cf, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})

	kf, _ := os.Create(keyFile)
	defer kf.Close()
	keyBytes, _ := x509.MarshalECPrivateKey(priv)
	pem.Encode(kf, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	log.Printf("TLS cert written to %s, key to %s", certFile, keyFile)
}

func tlsConfig(certFile, keyFile, host string) *tls.Config {
	// Only auto-generate if no cert paths were provided
	if certFile == "" && keyFile == "" {
		certFile = "cert.pem"
		keyFile = "key.pem"
	}
	ensureTLSCerts(certFile, keyFile, host)

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		log.Fatalf("failed to load TLS cert/key: %v", err)
	}

	return &tls.Config{Certificates: []tls.Certificate{cert}}
}
