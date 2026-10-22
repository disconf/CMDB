package license

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	licenseStateSchema = `
CREATE TABLE IF NOT EXISTS cmdb_license_state (
  id integer PRIMARY KEY CHECK (id = 1),
  deployment_id text NOT NULL,
  license_token text NOT NULL DEFAULT '',
  activated_at timestamptz,
  activated_by text NOT NULL DEFAULT '',
  updated_at timestamptz NOT NULL DEFAULT now()
);
`
	quotaAdvisoryLock int64 = 0x434d44424c4943
)

// defaultPublicKeyPEM is replaced with the product verification key generated
// by cmd/license-keygen. It is public information and safe to distribute.
const defaultPublicKeyPEM = `-----BEGIN PUBLIC KEY-----
MCowBQYDK2VwAyEAvs6cLCve6tnIlqHU8A5Q8crozEx+lGMOKULWEEqQSKI=
-----END PUBLIC KEY-----`

type storedState struct {
	DeploymentID string
	LicenseToken string
	ActivatedAt  string
	ActivatedBy  string
}

type Status struct {
	Edition         string   `json:"edition"`
	State           string   `json:"state"`
	LicenseID       string   `json:"licenseId,omitempty"`
	Customer        string   `json:"customer,omitempty"`
	DeploymentID    string   `json:"deploymentId"`
	MaxAssets       int      `json:"maxAssets"`
	FreeAssetLimit  int      `json:"freeAssetLimit"`
	UsedAssets      int      `json:"usedAssets"`
	RemainingAssets int      `json:"remainingAssets"`
	UsagePercent    float64  `json:"usagePercent"`
	IssuedAt        string   `json:"issuedAt,omitempty"`
	ExpiresAt       string   `json:"expiresAt,omitempty"`
	GraceUntil      string   `json:"graceUntil,omitempty"`
	ActivatedAt     string   `json:"activatedAt,omitempty"`
	ActivatedBy     string   `json:"activatedBy,omitempty"`
	Features        []string `json:"features"`
	CanCreateAssets bool     `json:"canCreateAssets"`
	Message         string   `json:"message"`
}

type Service struct {
	mu        sync.RWMutex
	db        *sql.DB
	publicKey ed25519.PublicKey
	state     storedState
	graceDays int
}

func NewService(db *sql.DB) (*Service, error) {
	publicValue := strings.TrimSpace(os.Getenv("LICENSE_PUBLIC_KEY"))
	if publicValue == "" {
		publicValue = defaultPublicKeyPEM
	}
	publicKey, err := ParsePublicKey(publicValue)
	if err != nil {
		return nil, fmt.Errorf("configure LICENSE_PUBLIC_KEY: %w", err)
	}
	graceDays := 30
	if raw := strings.TrimSpace(os.Getenv("LICENSE_GRACE_DAYS")); raw != "" {
		if parsed, parseErr := strconv.Atoi(raw); parseErr == nil && parsed >= 0 && parsed <= 365 {
			graceDays = parsed
		}
	}
	service := &Service{db: db, publicKey: publicKey, graceDays: graceDays}
	if db == nil {
		service.state.DeploymentID = newDeploymentID()
	} else {
		if err := service.initialize(context.Background()); err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(service.state.LicenseToken) == "" {
		if bootstrap := strings.TrimSpace(os.Getenv("LICENSE_KEY")); bootstrap != "" {
			if _, err := service.Activate(bootstrap, "environment"); err != nil {
				return nil, fmt.Errorf("activate bootstrap LICENSE_KEY: %w", err)
			}
		}
	}
	return service, nil
}

func (s *Service) initialize(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, licenseStateSchema); err != nil {
		return fmt.Errorf("initialize license schema: %w", err)
	}
	var deploymentID string
	if err := s.db.QueryRowContext(ctx, `SELECT deployment_id FROM cmdb_license_state WHERE id=1`).Scan(&deploymentID); err == nil {
		s.state.DeploymentID = deploymentID
	} else if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read license state: %w", err)
	}
	if strings.TrimSpace(s.state.DeploymentID) == "" {
		s.state.DeploymentID = newDeploymentID()
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO cmdb_license_state(id,deployment_id)
		VALUES(1,$1)
		ON CONFLICT (id) DO NOTHING`, s.state.DeploymentID); err != nil {
		return fmt.Errorf("initialize license state: %w", err)
	}
	var activatedAt sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT deployment_id,license_token,activated_at,activated_by
		FROM cmdb_license_state WHERE id=1`).Scan(
		&s.state.DeploymentID, &s.state.LicenseToken, &activatedAt, &s.state.ActivatedBy,
	)
	if err != nil {
		return fmt.Errorf("load license state: %w", err)
	}
	if activatedAt.Valid {
		s.state.ActivatedAt = activatedAt.Time.Local().Format(time.RFC3339)
	}
	return nil
}

func (s *Service) DeploymentID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.DeploymentID
}

func (s *Service) Activate(token, actor string) (Status, error) {
	claims, err := Verify(s.publicKey, token)
	if err != nil {
		return Status{}, err
	}
	s.mu.RLock()
	deploymentID := s.state.DeploymentID
	s.mu.RUnlock()
	if deploymentID != "" && claims.DeploymentID != deploymentID {
		return Status{}, ErrDeploymentMismatch
	}
	now := time.Now()
	if expiresAt, ok := Expiry(claims); ok {
		if expiresAt.IsZero() {
			return Status{}, ErrInvalidToken
		}
		if !now.Before(expiresAt) {
			return Status{}, ErrExpiredLicense
		}
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "system"
	}
	s.mu.Lock()
	previous := s.state
	s.state.LicenseToken = strings.TrimSpace(token)
	s.state.ActivatedAt = now.Format(time.RFC3339)
	s.state.ActivatedBy = actor
	state := s.state
	s.mu.Unlock()
	if s.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := s.db.ExecContext(ctx, `
			UPDATE cmdb_license_state
			SET license_token=$1,activated_at=now(),activated_by=$2,updated_at=now()
			WHERE id=1`, state.LicenseToken, state.ActivatedBy); err != nil {
			s.mu.Lock()
			s.state = previous
			s.mu.Unlock()
			return Status{}, fmt.Errorf("persist license: %w", err)
		}
	}
	return s.statusFromState(state, 0, now), nil
}

func (s *Service) Status(used int) Status {
	if s.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = s.refreshFromDB(ctx, nil)
		cancel()
	}
	s.mu.RLock()
	state := s.state
	s.mu.RUnlock()
	return s.statusFromState(state, used, time.Now())
}

func (s *Service) refreshFromDB(ctx context.Context, tx *sql.Tx) error {
	var row *sql.Row
	if tx != nil {
		row = tx.QueryRowContext(ctx, `SELECT deployment_id,license_token,activated_at,activated_by FROM cmdb_license_state WHERE id=1`)
	} else if s.db != nil {
		row = s.db.QueryRowContext(ctx, `SELECT deployment_id,license_token,activated_at,activated_by FROM cmdb_license_state WHERE id=1`)
	}
	if row == nil {
		return nil
	}
	var deploymentID, token, actor string
	var activatedAt sql.NullTime
	if err := row.Scan(&deploymentID, &token, &activatedAt, &actor); err != nil {
		return err
	}
	s.mu.Lock()
	s.state.DeploymentID = deploymentID
	s.state.LicenseToken = token
	s.state.ActivatedBy = actor
	if activatedAt.Valid {
		s.state.ActivatedAt = activatedAt.Time.Local().Format(time.RFC3339)
	} else {
		s.state.ActivatedAt = ""
	}
	s.mu.Unlock()
	return nil
}

func (s *Service) statusFromState(state storedState, used int, now time.Time) Status {
	result := Status{
		Edition: DefaultEdition, State: "community", DeploymentID: state.DeploymentID,
		MaxAssets: FreeAssetLimit, FreeAssetLimit: FreeAssetLimit, UsedAssets: max(used, 0),
		Features: []string{"cmdb", "monitoring", "automation", "knowledge"}, Message: "免费版可管理 200 个资产",
	}
	claims, err := Verify(s.publicKey, state.LicenseToken)
	if err != nil {
		result.CanCreateAssets = result.UsedAssets < result.MaxAssets
		result.RemainingAssets = max(result.MaxAssets-result.UsedAssets, 0)
		result.UsagePercent = usagePercent(result.UsedAssets, result.MaxAssets)
		if result.UsedAssets >= result.MaxAssets {
			result.State = "over_limit"
			result.Message = "已达到免费版资产额度，新增资产需要商业授权"
		}
		return result
	}
	expiresAt, hasExpiry := Expiry(claims)
	if hasExpiry && expiresAt.IsZero() {
		result.CanCreateAssets = result.UsedAssets < result.MaxAssets
		result.RemainingAssets = max(result.MaxAssets-result.UsedAssets, 0)
		result.UsagePercent = usagePercent(result.UsedAssets, result.MaxAssets)
		result.State = "invalid"
		result.Message = "许可证有效期格式无效，已回退到免费版额度"
		return result
	}
	result.Edition = firstNonEmpty(claims.Edition, EditionEnterprise)
	result.LicenseID = claims.LicenseID
	result.Customer = claims.Customer
	result.MaxAssets = claims.MaxAssets
	result.IssuedAt = claims.IssuedAt
	result.ExpiresAt = claims.ExpiresAt
	result.ActivatedAt = state.ActivatedAt
	result.ActivatedBy = state.ActivatedBy
	result.Features = append([]string(nil), claims.Features...)
	result.State = "licensed"
	result.Message = "商业授权有效"
	if hasExpiry && !now.Before(expiresAt) {
		graceUntil := expiresAt.Add(time.Duration(s.graceDays) * 24 * time.Hour)
		result.GraceUntil = graceUntil.Format(time.RFC3339)
		if now.Before(graceUntil) {
			result.State = "grace"
			result.Message = fmt.Sprintf("授权已到期，宽限期至 %s", graceUntil.Local().Format("2006-01-02 15:04"))
		} else {
			result.Edition = DefaultEdition
			result.MaxAssets = FreeAssetLimit
			result.State = "expired"
			result.Message = "授权和宽限期均已结束，已回退到免费版额度"
		}
	}
	result.CanCreateAssets = result.UsedAssets < result.MaxAssets
	result.RemainingAssets = max(result.MaxAssets-result.UsedAssets, 0)
	result.UsagePercent = usagePercent(result.UsedAssets, result.MaxAssets)
	if !result.CanCreateAssets && result.State != "expired" {
		result.State = "over_limit"
		result.Message = "已达到当前授权资产额度，继续新增资产需要扩容授权"
	}
	if len(result.Features) == 0 {
		result.Features = []string{}
	}
	return result
}

// ReserveAssetSlots is called by the CMDB service inside the asset insert
// transaction. PostgreSQL advisory locking keeps the count correct across all
// gateway replicas.
func (s *Service) ReserveAssetSlots(ctx context.Context, tx *sql.Tx, current, additional int) error {
	if additional <= 0 {
		return nil
	}
	used := current
	ownedTx := false
	if s.db != nil {
		if tx == nil {
			var err error
			tx, err = s.db.BeginTx(ctx, nil)
			if err != nil {
				return err
			}
			ownedTx = true
			defer tx.Rollback()
		}
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, quotaAdvisoryLock); err != nil {
			return err
		}
		if err := s.refreshFromDB(ctx, tx); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM cmdb_assets`).Scan(&used); err != nil {
			return err
		}
	}
	s.mu.RLock()
	state := s.state
	s.mu.RUnlock()
	status := s.statusFromState(state, used, time.Now())
	if used+additional > status.MaxAssets {
		return &AssetLimitError{Used: used, Limit: status.MaxAssets, Requested: additional, Edition: status.Edition}
	}
	if ownedTx {
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func IsAssetLimit(err error) (*AssetLimitError, bool) {
	var target *AssetLimitError
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}

func newDeploymentID() string {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return fmt.Sprintf("deployment-%d", time.Now().UnixNano())
	}
	return "deploy-" + hex.EncodeToString(data)
}

func usagePercent(used, limit int) float64 {
	if limit <= 0 {
		return 0
	}
	return float64(int(float64(used)*1000/float64(limit)+0.5)) / 10
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
