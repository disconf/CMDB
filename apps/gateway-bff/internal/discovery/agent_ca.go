package discovery

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"strings"
	"time"
)

type IssuedAgentCertificate struct {
	Certificate string `json:"certificate"`
	PrivateKey  string `json:"privateKey"`
	Fingerprint string `json:"fingerprint"`
	Serial      string `json:"serial"`
	NotAfter    string `json:"notAfter"`
}

func AgentCAConfigured() bool {
	return strings.TrimSpace(os.Getenv("AGENT_MTLS_CA_CERT")) != "" && strings.TrimSpace(os.Getenv("AGENT_MTLS_CA_KEY")) != ""
}

func (s *Service) IssueAgentCertificate(agentID string) (IssuedAgentCertificate, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return IssuedAgentCertificate{}, errors.New("agent id is required")
	}
	caCert, caKey, err := loadAgentCA()
	if err != nil {
		return IssuedAgentCertificate{}, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return IssuedAgentCertificate{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return IssuedAgentCertificate{}, err
	}
	now := time.Now()
	notAfter := now.AddDate(0, 1, 0)
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: agentID, Organization: []string{"CMDB Agents"}}, NotBefore: now.Add(-time.Minute), NotAfter: notAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		return IssuedAgentCertificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return IssuedAgentCertificate{}, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	issued := IssuedAgentCertificate{Certificate: string(certPEM), PrivateKey: string(keyPEM), Fingerprint: hex.EncodeToString(der), Serial: serial.Text(16), NotAfter: notAfter.Format("2006-01-02 15:04:05")}
	if err = s.RegisterAgentCertificate(AgentCertificate{AgentID: agentID, Fingerprint: issued.Fingerprint, Serial: issued.Serial, NotAfter: issued.NotAfter}); err != nil {
		return IssuedAgentCertificate{}, err
	}
	return issued, nil
}

func loadAgentCA() (*x509.Certificate, any, error) {
	certPEM := strings.TrimSpace(os.Getenv("AGENT_MTLS_CA_CERT"))
	keyPEM := strings.TrimSpace(os.Getenv("AGENT_MTLS_CA_KEY"))
	certBlock, _ := pem.Decode([]byte(certPEM))
	keyBlock, _ := pem.Decode([]byte(keyPEM))
	if certBlock == nil || keyBlock == nil {
		return nil, nil, errors.New("invalid CA certificate or key PEM")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, err
	}
	if key, err := x509.ParseECPrivateKey(keyBlock.Bytes); err == nil {
		return cert, key, nil
	}
	if key, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes); err == nil {
		return cert, key, nil
	}
	if key, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes); err == nil {
		return cert, key, nil
	}
	return nil, nil, errors.New("unsupported CA private key format")
}
