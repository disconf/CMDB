package credentials

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

var ErrNotConfigured = errors.New("vault not configured")

type Credential struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Username string `json:"username"`
}
type Material struct {
	Username string `json:"username"`
	Secret   string `json:"secret"`
}
type Service struct {
	addr   string
	token  string
	client *http.Client
}

func NewService() *Service {
	return &Service{addr: strings.TrimRight(os.Getenv("VAULT_ADDR"), "/"), token: os.Getenv("VAULT_TOKEN"), client: &http.Client{Timeout: 8 * time.Second}}
}

func (s *Service) configured() bool { return s.addr != "" && s.token != "" }

func (s *Service) do(method, path string, payload interface{}) (*http.Response, error) {
	var body *bytes.Reader
	if payload != nil {
		raw, _ := json.Marshal(payload)
		body = bytes.NewReader(raw)
	} else {
		body = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, s.addr+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Vault-Token", s.token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return s.client.Do(req)
}

func (s *Service) Create(name, kind, username, secret string) (Credential, error) {
	if !s.configured() {
		return Credential{}, ErrNotConfigured
	}
	if strings.TrimSpace(name) == "" || strings.TrimSpace(kind) == "" || strings.TrimSpace(secret) == "" {
		return Credential{}, errors.New("name/kind/secret required")
	}
	id := fmt.Sprintf("cred-%d", time.Now().UnixNano())
	payload := map[string]interface{}{"data": map[string]string{"name": name, "kind": kind, "username": username, "secret": secret}}
	resp, err := s.do(http.MethodPost, "/v1/secret/data/cmdb/"+id, payload)
	if err != nil {
		return Credential{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return Credential{}, fmt.Errorf("vault write: %s", resp.Status)
	}
	return Credential{ID: id, Name: name, Kind: kind, Username: username}, nil
}

func (s *Service) List() ([]Credential, error) {
	if !s.configured() {
		return nil, ErrNotConfigured
	}
	resp, err := s.do("LIST", "/v1/secret/metadata/cmdb", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return []Credential{}, nil
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("vault list: %s", resp.Status)
	}
	var parsed struct {
		Data struct {
			Keys []string `json:"keys"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	out := []Credential{}
	for _, id := range parsed.Data.Keys {
		resp, err := s.do(http.MethodGet, "/v1/secret/data/cmdb/"+id, nil)
		if err != nil {
			continue
		}
		var item struct {
			Data struct {
				Data map[string]string `json:"data"`
			} `json:"data"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&item)
		resp.Body.Close()
		out = append(out, Credential{ID: id, Name: item.Data.Data["name"], Kind: item.Data.Data["kind"], Username: item.Data.Data["username"]})
	}
	return out, nil
}

func (s *Service) Resolve(id string) (Material, error) {
	if !s.configured() {
		return Material{}, ErrNotConfigured
	}
	if id == "" {
		return Material{}, errors.New("credential id required")
	}
	resp, err := s.do(http.MethodGet, "/v1/secret/data/cmdb/"+id, nil)
	if err != nil {
		return Material{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return Material{}, fmt.Errorf("vault read: %s", resp.Status)
	}
	var item struct {
		Data struct {
			Data map[string]string `json:"data"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		return Material{}, err
	}
	return Material{Username: item.Data.Data["username"], Secret: item.Data.Data["secret"]}, nil
}

func (s *Service) Delete(id string) error {
	if !s.configured() {
		return ErrNotConfigured
	}
	resp, err := s.do(http.MethodDelete, "/v1/secret/metadata/cmdb/"+id, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("vault delete: %s", resp.Status)
	}
	return nil
}
