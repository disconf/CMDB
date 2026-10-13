package projection

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

type Service struct {
	neo4jURL, neo4jUser, neo4jPassword                string
	openSearchURL, openSearchUser, openSearchPassword string
	client                                            *http.Client
}

type Status struct {
	Neo4jConfigured      bool `json:"neo4jConfigured"`
	OpenSearchConfigured bool `json:"openSearchConfigured"`
}

func NewFromEnv() *Service {
	return &Service{
		neo4jURL: strings.TrimRight(os.Getenv("NEO4J_URL"), "/"), neo4jUser: os.Getenv("NEO4J_USERNAME"), neo4jPassword: os.Getenv("NEO4J_PASSWORD"),
		openSearchURL: strings.TrimRight(os.Getenv("OPENSEARCH_URL"), "/"), openSearchUser: os.Getenv("OPENSEARCH_USERNAME"), openSearchPassword: os.Getenv("OPENSEARCH_PASSWORD"),
		client: &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}},
	}
}

func (s *Service) Status() Status {
	return Status{Neo4jConfigured: s.neo4jURL != "", OpenSearchConfigured: s.openSearchURL != ""}
}

type Asset struct {
	ID, Name, Type, Status, Environment, ProjectGroup, Owner, IP, Location string
	Relations                                                              []map[string]string
}

func (s *Service) ProjectAssets(ctx context.Context, assets []Asset) (int, int, error) {
	neo4jCount, openSearchCount := 0, 0
	for _, asset := range assets {
		if s.neo4jURL != "" {
			if err := s.projectNeo4j(ctx, asset); err != nil {
				return neo4jCount, openSearchCount, err
			}
			neo4jCount++
		}
		if s.openSearchURL != "" {
			if err := s.projectOpenSearch(ctx, asset); err != nil {
				return neo4jCount, openSearchCount, err
			}
			openSearchCount++
		}
	}
	return neo4jCount, openSearchCount, nil
}

func (s *Service) projectNeo4j(ctx context.Context, asset Asset) error {
	statement := "MERGE (a:Asset {id: $id}) SET a.name=$name, a.type=$type, a.status=$status, a.environment=$environment, a.projectGroup=$projectGroup, a.owner=$owner, a.ip=$ip, a.location=$location"
	payload, _ := json.Marshal(map[string]any{"statements": []map[string]any{{"statement": statement, "parameters": map[string]any{"id": asset.ID, "name": asset.Name, "type": asset.Type, "status": asset.Status, "environment": asset.Environment, "projectGroup": asset.ProjectGroup, "owner": asset.Owner, "ip": asset.IP, "location": asset.Location}}}})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.neo4jURL+"/db/neo4j/tx/commit", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	if s.neo4jUser != "" {
		request.SetBasicAuth(s.neo4jUser, s.neo4jPassword)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("neo4j returned %s", response.Status)
	}
	return nil
}

func (s *Service) projectOpenSearch(ctx context.Context, asset Asset) error {
	payload, _ := json.Marshal(asset)
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, s.openSearchURL+"/cmdb-assets/_doc/"+asset.ID, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	if s.openSearchUser != "" {
		request.SetBasicAuth(s.openSearchUser, s.openSearchPassword)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("opensearch returned %s", response.Status)
	}
	return nil
}
