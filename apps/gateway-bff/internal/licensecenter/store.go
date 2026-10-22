package licensecenter

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"cmdb/gateway-bff/internal/license"
)

type Record struct {
	ID           string   `json:"id"`
	Customer     string   `json:"customer"`
	DeploymentID string   `json:"deploymentId"`
	Edition      string   `json:"edition"`
	MaxAssets    int      `json:"maxAssets"`
	IssuedAt     string   `json:"issuedAt"`
	ExpiresAt    string   `json:"expiresAt,omitempty"`
	Features     []string `json:"features"`
	Notes        string   `json:"notes,omitempty"`
	IssuedBy     string   `json:"issuedBy"`
	LicenseKey   string   `json:"licenseKey"`
}

type Store struct {
	mu      sync.RWMutex
	path    string
	records []Record
}

func NewStore(path string) (*Store, error) {
	store := &Store{path: path, records: []Record{}}
	if strings.TrimSpace(path) == "" {
		return store, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return store, nil
	}
	if err := json.Unmarshal(data, &store.records); err != nil {
		return nil, fmt.Errorf("decode license records: %w", err)
	}
	return store, nil
}

func (s *Store) List() []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := append([]Record(nil), s.records...)
	sort.SliceStable(result, func(i, j int) bool { return result[i].IssuedAt > result[j].IssuedAt })
	return result
}

func (s *Store) Add(record Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append([]Record{record}, s.records...)
	if err := s.saveLocked(); err != nil {
		s.records = s.records[1:]
		return err
	}
	return nil
}

func (s *Store) saveLocked() error {
	if strings.TrimSpace(s.path) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(s.records, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(s.path), ".licenses-*.tmp")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(payload); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, s.path)
}

func newLicenseID(now time.Time) string {
	random := make([]byte, 4)
	_, _ = rand.Read(random)
	return "CMDB-" + now.Format("20060102") + "-" + strings.ToUpper(hex.EncodeToString(random))
}

func normalizeFeatures(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			item = strings.TrimSpace(item)
			if item == "" || seen[item] {
				continue
			}
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
}

func claimsForRecord(record Record) license.Claims {
	return license.Claims{
		Version: license.TokenVersion, LicenseID: record.ID, Customer: record.Customer,
		Edition: record.Edition, MaxAssets: record.MaxAssets, DeploymentID: record.DeploymentID,
		IssuedAt: record.IssuedAt, ExpiresAt: record.ExpiresAt, Features: record.Features,
		IssuedBy: record.IssuedBy,
	}
}
