package license

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSignVerifyAndExpiry(t *testing.T) {
	publicKey, privateKey, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	token, err := Sign(privateKey, Claims{
		LicenseID: "LIC-TEST-001", Customer: "测试客户", Edition: EditionEnterprise,
		MaxAssets: 500, DeploymentID: "deploy-test", ExpiresAt: time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := Verify(publicKey, token)
	if err != nil || claims.MaxAssets != 500 || claims.DeploymentID != "deploy-test" {
		t.Fatalf("unexpected claims: %+v %v", claims, err)
	}
	if _, err := Verify(publicKey, token+"x"); !errors.Is(err, ErrInvalidSignature) && !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("tampered token must fail, got %v", err)
	}
}

func TestCommunityAssetLimit(t *testing.T) {
	publicKey, privateKey, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{publicKey: publicKey, graceDays: 30, state: storedState{DeploymentID: "deploy-test"}}
	if err := service.ReserveAssetSlots(context.Background(), nil, 199, 1); err != nil {
		t.Fatalf("asset 200 should be allowed: %v", err)
	}
	err = service.ReserveAssetSlots(context.Background(), nil, 200, 1)
	limitErr, ok := IsAssetLimit(err)
	if !ok || limitErr.Limit != FreeAssetLimit || limitErr.Used != 200 {
		t.Fatalf("expected community quota error, got %v", err)
	}
	token, err := Sign(privateKey, Claims{LicenseID: "LIC-500", Customer: "客户", MaxAssets: 500, DeploymentID: "deploy-test"})
	if err != nil {
		t.Fatal(err)
	}
	service.state.LicenseToken = token
	if err := service.ReserveAssetSlots(context.Background(), nil, 499, 1); err != nil {
		t.Fatalf("licensed asset 500 should be allowed: %v", err)
	}
}

func TestExpiredLicenseFallsBackToCommunityLimit(t *testing.T) {
	publicKey, privateKey, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	token, err := Sign(privateKey, Claims{
		LicenseID: "LIC-EXPIRED", Customer: "客户", MaxAssets: 500, DeploymentID: "deploy-test",
		ExpiresAt: time.Now().Add(-60 * 24 * time.Hour).UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{publicKey: publicKey, graceDays: 30, state: storedState{DeploymentID: "deploy-test", LicenseToken: token}}
	status := service.Status(300)
	if status.State != "expired" || status.MaxAssets != FreeAssetLimit || status.CanCreateAssets {
		t.Fatalf("unexpected expired status: %+v", status)
	}
}
