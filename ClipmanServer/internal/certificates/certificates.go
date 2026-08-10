package certificates

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Result struct {
	Authority, Certificate, FullChain, Key, Fingerprint string
	Expires                                             time.Time
}

func Generate(configPath string, hosts, ips []string, newCA bool) (Result, error) {
	return generate(configPath, hosts, ips, newCA, 4096, 2048, time.Now())
}
func generate(configPath string, hosts, ips []string, newCA bool, caBits, leafBits int, now time.Time) (Result, error) {
	dir := filepath.Join(filepath.Dir(configPath), "tls")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Result{}, err
	}
	caKeyPath := filepath.Join(dir, "clipman-server-ca.key")
	caPath := filepath.Join(dir, "clipman-server-ca.crt")
	leafKeyPath := filepath.Join(dir, "clipman-server.key")
	leafPath := filepath.Join(dir, "clipman-server.crt")
	fullPath := filepath.Join(dir, "clipman-server-fullchain.crt")
	var caKey *rsa.PrivateKey
	var caCert *x509.Certificate
	var caPEM []byte
	if !newCA {
		var err error
		caKey, err = readRSAKey(caKeyPath)
		if err == nil {
			caCert, caPEM, err = readCertificate(caPath)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return Result{}, fmt.Errorf("load existing certificate authority: %w", err)
		}
	}
	if caKey == nil || caCert == nil {
		var err error
		caKey, err = rsa.GenerateKey(rand.Reader, caBits)
		if err != nil {
			return Result{}, err
		}
		serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
		template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Clipman Server Private CA"}, NotBefore: now.Add(-5 * time.Minute), NotAfter: now.AddDate(10, 0, 0), IsCA: true, BasicConstraintsValid: true, MaxPathLenZero: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
		der, err := x509.CreateCertificate(rand.Reader, template, template, &caKey.PublicKey, caKey)
		if err != nil {
			return Result{}, err
		}
		caCert, err = x509.ParseCertificate(der)
		if err != nil {
			return Result{}, err
		}
		caPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
		if err = writeKey(caKeyPath, caKey); err != nil {
			return Result{}, err
		}
		if err = os.WriteFile(caPath, caPEM, 0o644); err != nil {
			return Result{}, err
		}
	}
	dns, addresses, err := normalizeNames(hosts, ips)
	if err != nil {
		return Result{}, err
	}
	leafKey, err := rsa.GenerateKey(rand.Reader, leafBits)
	if err != nil {
		return Result{}, err
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	leaf := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Clipman Server"}, NotBefore: now.Add(-5 * time.Minute), NotAfter: now.AddDate(1, 1, 0), DNSNames: dns, IPAddresses: addresses, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, leaf, caCert, &leafKey.PublicKey, caKey)
	if err != nil {
		return Result{}, err
	}
	leafPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err = writeKey(leafKeyPath, leafKey); err != nil {
		return Result{}, err
	}
	if err = os.WriteFile(leafPath, leafPEM, 0o644); err != nil {
		return Result{}, err
	}
	if err = os.WriteFile(fullPath, append(append([]byte{}, leafPEM...), caPEM...), 0o644); err != nil {
		return Result{}, err
	}
	sum := sha256.Sum256(caCert.Raw)
	return Result{caPath, leafPath, fullPath, leafKeyPath, fingerprint(sum[:]), leaf.NotAfter}, nil
}
func InspectCA(caPath, leafPath, host string) (string, *x509.Certificate, error) {
	ca, _, err := readCertificate(caPath)
	if err != nil {
		return "", nil, err
	}
	if !ca.IsCA || ca.KeyUsage&x509.KeyUsageCertSign == 0 {
		return "", nil, errors.New("configured authority cannot sign certificates")
	}
	leaf, _, err := readCertificate(leafPath)
	if err != nil {
		return "", nil, err
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	if _, err = leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: strings.Trim(host, "[]")}); err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(ca.Raw)
	return fingerprint(sum[:]), ca, nil
}
func normalizeNames(hosts, ips []string) ([]string, []net.IP, error) {
	seen := map[string]bool{}
	dns := []string{}
	addresses := []net.IP{}
	for _, value := range append(hosts, "localhost") {
		clean := strings.TrimSpace(strings.Trim(value, "[]"))
		if clean == "" || clean == "0.0.0.0" || clean == "::" {
			continue
		}
		if ip := net.ParseIP(clean); ip != nil {
			key := ip.String()
			if !seen[key] {
				seen[key] = true
				addresses = append(addresses, ip)
			}
			continue
		}
		lower := strings.ToLower(strings.TrimSuffix(clean, "."))
		if strings.ContainsAny(lower, " /\\") || len(lower) > 253 {
			return nil, nil, fmt.Errorf("invalid certificate DNS name: %s", clean)
		}
		if !seen[lower] {
			seen[lower] = true
			dns = append(dns, lower)
		}
	}
	for _, value := range append(ips, "127.0.0.1", "::1") {
		ip := net.ParseIP(strings.TrimSpace(value))
		if ip == nil {
			return nil, nil, fmt.Errorf("invalid certificate IP address: %s", value)
		}
		key := ip.String()
		if !seen[key] {
			seen[key] = true
			addresses = append(addresses, ip)
		}
	}
	return dns, addresses, nil
}
func readCertificate(path string) (*x509.Certificate, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, nil, errors.New("invalid PEM certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	return cert, pem.EncodeToMemory(block), err
}
func readRSAKey(path string) (*rsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("invalid PEM key")
	}
	if key, parseErr := x509.ParsePKCS8PrivateKey(block.Bytes); parseErr == nil {
		if rsaKey, ok := key.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		}
	}
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}
func writeKey(path string, key *rsa.PrivateKey) error {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	return os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600)
}
func fingerprint(raw []byte) string {
	hexText := strings.ToUpper(hex.EncodeToString(raw))
	parts := make([]string, 0, len(hexText)/2)
	for i := 0; i < len(hexText); i += 2 {
		parts = append(parts, hexText[i:i+2])
	}
	return strings.Join(parts, ":")
}
