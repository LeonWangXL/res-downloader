package core

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

const legacyCertPEM = `-----BEGIN CERTIFICATE-----
MIIDwzCCAqugAwIBAgIUFAnC6268dp/z1DR9E1UepiWgWzkwDQYJKoZIhvcNAQEL
BQAwcDELMAkGA1UEBhMCQ04xEjAQBgNVBAgMCUNob25ncWluZzESMBAGA1UEBwwJ
Q2hvbmdxaW5nMQ4wDAYDVQQKDAVnb3dhczEWMBQGA1UECwwNSVQgRGVwYXJ0bWVu
dDERMA8GA1UEAwwIZ293YXMuY24wIBcNMjQwMjE4MDIwOTI2WhgPMjEyNDAxMjUw
MjA5MjZaMHAxCzAJBgNVBAYTAkNOMRIwEAYDVQQIDAlDaG9uZ3FpbmcxEjAQBgNV
BAcMCUNob25ncWluZzEOMAwGA1UECgwFZ293YXMxFjAUBgNVBAsMDUlUIERlcGFy
dG1lbnQxETAPBgNVBAMMCGdvd2FzLmNuMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8A
MIIBCgKCAQEA3A7dt7eoqAaBxv2Npjo8Z7VkGvXT93jZfpgAuuNuQ5RLcnOnMzQC
CrrjPcLfsAMA0AIK3eUWsXXKSR9SZTJBLQRZCJHZ9AIPfA+58JVQPTjd8UIuQZJf
rDf6FjhPJTsLzcjTU+mT7t6lEimPEl2VWN9eXWqs9nkVrJtqLao6m1hoYfXOxRh6
96/WgBtPHcmjujryteBiSITVflDjx+YQzDGsbqw7fM52klMPd2+w/vmhJ4pxq6P7
Ni2OBvdXYDPIuLfPFFqG16arORjBkyNCJy19iOuh5LXh+EUX11wvbLwNgsTd8j9v
eBSD+4HUUNQhiXiXJbs7I7cdFYthvb609QIDAQABo1MwUTAdBgNVHQ4EFgQUdI8p
aY1A47rWCRvQKSTRCCk6FoMwHwYDVR0jBBgwFoAUdI8paY1A47rWCRvQKSTRCCk6
FoMwDwYDVR0TAQH/BAUwAwEB/zANBgkqhkiG9w0BAQsFAAOCAQEArMCAfqidgXL7
cW5TAZTCqnUeKzbbqMJgk6iFsma8scMRsUXz9ZhF0UVf98376KvoJpy4vd81afbi
TehQ8wVBuKTtkHeh/MkXMWC/FU4HqSjtvxpic2+Or5dMjIrfa5VYPgzfqNaBIUh4
InD5lo8b/n5V+jdwX7RX9VYAKug6QZlCg5YSKIvgNRChb36JmrGcvsp5R0Vejnii
e3oowvgwikqm6XR6BEcRpPkztqcKST7jPFGHiXWsAqiibc+/plMW9qebhfMXEGhQ
5yVNeSxX2zqasZvP/fRy+3I5iVilxtKvJuVpPZ0UZzGS0CJ/lF67ntibktiPa3sR
D8HixYbEDg==
-----END CERTIFICATE-----
`

func newSessionToken() string {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		panic(err)
	}
	return hex.EncodeToString(data)
}
func (a *App) loadCA() error {
	filename := filepath.Join(a.UserDir, "ca.pem")
	bundle, err := os.ReadFile(filename)
	if err == nil {
		pair, err := tls.X509KeyPair(bundle, bundle)
		if err != nil {
			return fmt.Errorf("invalid saved CA: %w", err)
		}
		cert, err := x509.ParseCertificate(pair.Certificate[0])
		if err != nil {
			return err
		}
		if cert.IsCA && time.Now().Before(cert.NotAfter.Add(-30*24*time.Hour)) {
			a.PublicCrt = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
			for len(bundle) > 0 {
				block, rest := pem.Decode(bundle)
				if block == nil {
					break
				}
				if block.Type == "RSA PRIVATE KEY" {
					a.PrivateKey = pem.EncodeToMemory(block)
					return nil
				}
				bundle = rest
			}
			return fmt.Errorf("missing CA private key")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	cert, key, err := generateCA()
	if err != nil {
		return err
	}
	if err := atomicWriteFile(filename, append(append([]byte{}, cert...), key...), 0600); err != nil {
		return err
	}
	a.PublicCrt, a.PrivateKey = cert, key
	return nil
}
func generateCA() ([]byte, []byte, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, nil, err
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "res-downloader local CA " + serial.Text(16), Organization: []string{"res-downloader"}}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(5, 0, 0), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), nil
}
