package license

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	TokenPrefix       = "CMDB-LIC-v1"
	TokenVersion      = 1
	FreeAssetLimit    = 200
	DefaultEdition    = "community"
	EditionEnterprise = "enterprise"
)

var (
	ErrInvalidToken       = errors.New("invalid license token")
	ErrInvalidSignature   = errors.New("invalid license signature")
	ErrInvalidPublicKey   = errors.New("invalid license public key")
	ErrInvalidPrivateKey  = errors.New("invalid license private key")
	ErrExpiredLicense     = errors.New("license expired")
	ErrDeploymentMismatch = errors.New("license deployment mismatch")
)

type Claims struct {
	Version      int      `json:"version"`
	LicenseID    string   `json:"licenseId"`
	Customer     string   `json:"customer"`
	Edition      string   `json:"edition"`
	MaxAssets    int      `json:"maxAssets"`
	DeploymentID string   `json:"deploymentId"`
	IssuedAt     string   `json:"issuedAt"`
	ExpiresAt    string   `json:"expiresAt,omitempty"`
	Features     []string `json:"features,omitempty"`
	IssuedBy     string   `json:"issuedBy,omitempty"`
}

type AssetLimitError struct {
	Used      int
	Limit     int
	Requested int
	Edition   string
}

func (e *AssetLimitError) Error() string {
	if e.Edition == DefaultEdition {
		return fmt.Sprintf("免费版最多管理 %d 个资产，当前已有 %d 个，本次需要新增 %d 个", e.Limit, e.Used, e.Requested)
	}
	return fmt.Sprintf("当前授权最多管理 %d 个资产，当前已有 %d 个，本次需要新增 %d 个", e.Limit, e.Used, e.Requested)
}

func Sign(privateKey ed25519.PrivateKey, claims Claims) (string, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return "", ErrInvalidPrivateKey
	}
	claims.Version = TokenVersion
	claims.LicenseID = strings.TrimSpace(claims.LicenseID)
	claims.Customer = strings.TrimSpace(claims.Customer)
	claims.DeploymentID = strings.TrimSpace(claims.DeploymentID)
	if claims.LicenseID == "" || claims.Customer == "" || claims.DeploymentID == "" || claims.MaxAssets <= 0 {
		return "", fmt.Errorf("%w: missing required claims", ErrInvalidToken)
	}
	if claims.Edition == "" {
		claims.Edition = EditionEnterprise
	}
	if claims.IssuedAt == "" {
		claims.IssuedAt = time.Now().UTC().Format(time.RFC3339)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payloadPart := base64.RawURLEncoding.EncodeToString(payload)
	signingInput := TokenPrefix + "." + payloadPart
	signature := ed25519.Sign(privateKey, []byte(signingInput))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func Verify(publicKey ed25519.PublicKey, token string) (Claims, error) {
	if len(publicKey) != ed25519.PublicKeySize {
		return Claims{}, ErrInvalidPublicKey
	}
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 || parts[0] != TokenPrefix {
		return Claims{}, ErrInvalidToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(signature) != ed25519.SignatureSize {
		return Claims{}, ErrInvalidToken
	}
	if !ed25519.Verify(publicKey, []byte(parts[0]+"."+parts[1]), signature) {
		return Claims{}, ErrInvalidSignature
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Claims{}, ErrInvalidToken
	}
	if claims.Version != TokenVersion || claims.LicenseID == "" || claims.Customer == "" ||
		claims.DeploymentID == "" || claims.MaxAssets <= 0 {
		return Claims{}, ErrInvalidToken
	}
	return claims, nil
}

func GenerateKeyPair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
}

func ParsePublicKey(value string) (ed25519.PublicKey, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, ErrInvalidPublicKey
	}
	if block, _ := pem.Decode([]byte(value)); block != nil {
		parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, ErrInvalidPublicKey
		}
		key, ok := parsed.(ed25519.PublicKey)
		if !ok || len(key) != ed25519.PublicKeySize {
			return nil, ErrInvalidPublicKey
		}
		return append(ed25519.PublicKey(nil), key...), nil
	}
	raw, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(value)
	}
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, ErrInvalidPublicKey
	}
	return ed25519.PublicKey(raw), nil
}

func ParsePrivateKey(value string) (ed25519.PrivateKey, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, ErrInvalidPrivateKey
	}
	if block, _ := pem.Decode([]byte(value)); block != nil {
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, ErrInvalidPrivateKey
		}
		key, ok := parsed.(ed25519.PrivateKey)
		if !ok || len(key) != ed25519.PrivateKeySize {
			return nil, ErrInvalidPrivateKey
		}
		return append(ed25519.PrivateKey(nil), key...), nil
	}
	raw, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(value)
	}
	if err != nil || len(raw) != ed25519.PrivateKeySize {
		return nil, ErrInvalidPrivateKey
	}
	return ed25519.PrivateKey(raw), nil
}

func PublicKeyPEM(publicKey ed25519.PublicKey) ([]byte, error) {
	der, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), nil
}

func PrivateKeyPEM(privateKey ed25519.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

func LoadKeyFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func Expiry(claims Claims) (time.Time, bool) {
	if strings.TrimSpace(claims.ExpiresAt) == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, claims.ExpiresAt)
	if err != nil {
		return time.Time{}, true
	}
	return parsed, true
}
