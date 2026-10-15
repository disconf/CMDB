package projection

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// openSearchIndex is the single index that holds every projected CMDB asset.
const openSearchIndex = "cmdb-assets"

// openSearchBatchSize keeps bulk payloads small enough to stay well below the
// default http.max_content_length of OpenSearch.
const openSearchBatchSize = 200

type Service struct {
	neo4jURL, neo4jUser, neo4jPassword                string
	openSearchURL, openSearchUser, openSearchPassword string
	client                                            *http.Client
}

type Status struct {
	Neo4jConfigured      bool   `json:"neo4jConfigured"`
	OpenSearchConfigured bool   `json:"openSearchConfigured"`
	Neo4jURL             string `json:"neo4jUrl,omitempty"`
	OpenSearchURL        string `json:"openSearchUrl,omitempty"`
	Neo4jAssets          int    `json:"neo4jAssets"`
	OpenSearchAssets     int    `json:"openSearchAssets"`
	Detail               string `json:"detail,omitempty"`
}

func NewFromEnv() *Service {
	return &Service{
		neo4jURL: strings.TrimRight(os.Getenv("NEO4J_URL"), "/"), neo4jUser: os.Getenv("NEO4J_USERNAME"), neo4jPassword: os.Getenv("NEO4J_PASSWORD"),
		openSearchURL: strings.TrimRight(os.Getenv("OPENSEARCH_URL"), "/"), openSearchUser: os.Getenv("OPENSEARCH_USERNAME"), openSearchPassword: os.Getenv("OPENSEARCH_PASSWORD"),
		client: &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}},
	}
}

// Status reports static configuration only. Use Inspect for live counters.
func (s *Service) Status() Status {
	return Status{Neo4jConfigured: s.neo4jURL != "", OpenSearchConfigured: s.openSearchURL != ""}
}

// Inspect probes both projections and reports how many assets each one holds.
func (s *Service) Inspect(ctx context.Context) Status {
	status := Status{Neo4jConfigured: s.neo4jURL != "", OpenSearchConfigured: s.openSearchURL != "", Neo4jURL: s.neo4jURL, OpenSearchURL: s.openSearchURL}
	problems := []string{}
	if s.neo4jURL != "" {
		count, err := s.countNeo4j(ctx)
		if err != nil {
			problems = append(problems, "neo4j: "+err.Error())
		} else {
			status.Neo4jAssets = count
		}
	}
	if s.openSearchURL != "" {
		count, err := s.countOpenSearch(ctx)
		if err != nil {
			problems = append(problems, "opensearch: "+err.Error())
		} else {
			status.OpenSearchAssets = count
		}
	}
	status.Detail = strings.Join(problems, "; ")
	return status
}

type Asset struct {
	ID, Name, Type, Status, Environment, ProjectGroup, Owner, IP, Location string
	Relations                                                              []map[string]string
}

// ProjectAssets mirrors every asset into Neo4j and OpenSearch.
//
// Asset identifiers such as "k8s-pod/ops-qa/gateway-abc" contain slashes, so the
// OpenSearch side uses the bulk API where identifiers travel inside the request
// body instead of the URL path. That keeps ids intact and cuts the number of
// round trips from one per asset down to one per batch.
func (s *Service) ProjectAssets(ctx context.Context, assets []Asset) (int, int, error) {
	neo4jCount, openSearchCount := 0, 0
	if len(assets) == 0 {
		return 0, 0, nil
	}
	if s.neo4jURL != "" {
		_ = s.ensureNeo4jConstraint(ctx)
		if err := s.projectNeo4j(ctx, assets); err != nil {
			return 0, 0, err
		}
		neo4jCount = len(assets)
	}
	if s.openSearchURL != "" {
		if err := s.ensureOpenSearchIndex(ctx); err != nil {
			return neo4jCount, 0, err
		}
		for start := 0; start < len(assets); start += openSearchBatchSize {
			end := start + openSearchBatchSize
			if end > len(assets) {
				end = len(assets)
			}
			if err := s.projectOpenSearch(ctx, assets[start:end]); err != nil {
				return neo4jCount, openSearchCount, err
			}
			openSearchCount += end - start
		}
	}
	return neo4jCount, openSearchCount, nil
}

type statement struct {
	Statement  string         `json:"statement"`
	Parameters map[string]any `json:"parameters"`
}

// ensureNeo4jConstraint creates the uniqueness constraint used by every MERGE.
// Neo4j rejects schema changes mixed into a write transaction, so this runs on
// its own; a failure only costs lookup speed and never blocks the sync.
func (s *Service) ensureNeo4jConstraint(ctx context.Context) error {
	return s.commitNeo4j(ctx, []statement{{Statement: "CREATE CONSTRAINT asset_id_unique IF NOT EXISTS FOR (a:Asset) REQUIRE a.id IS UNIQUE", Parameters: map[string]any{}}})
}

func (s *Service) commitNeo4j(ctx context.Context, statements []statement) error {
	payload, err := json.Marshal(map[string]any{"statements": statements})
	if err != nil {
		return err
	}
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
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<16))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("neo4j returned %s: %s", response.Status, truncate(body, 300))
	}
	var result struct {
		Errors []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &result); err == nil && len(result.Errors) > 0 {
		return fmt.Errorf("neo4j error %s: %s", result.Errors[0].Code, result.Errors[0].Message)
	}
	return nil
}

func (s *Service) projectNeo4j(ctx context.Context, assets []Asset) error {
	rows := make([]map[string]any, 0, len(assets))
	relations := make([]map[string]string, 0)
	for _, asset := range assets {
		rows = append(rows, map[string]any{
			"id": asset.ID, "name": asset.Name, "type": asset.Type, "status": asset.Status,
			"environment": asset.Environment, "projectGroup": asset.ProjectGroup, "owner": asset.Owner,
			"ip": asset.IP, "location": asset.Location,
		})
		for _, relation := range asset.Relations {
			target := strings.TrimSpace(relation["targetId"])
			if target == "" || target == asset.ID {
				continue
			}
			relations = append(relations, map[string]string{"sourceId": asset.ID, "targetId": target, "type": relation["type"]})
		}
	}
	statements := []statement{
		{Statement: "UNWIND $assets AS row MERGE (a:Asset {id: row.id}) SET a.name=row.name, a.type=row.type, a.status=row.status, a.environment=row.environment, a.projectGroup=row.projectGroup, a.owner=row.owner, a.ip=row.ip, a.location=row.location", Parameters: map[string]any{"assets": rows}},
	}
	if len(relations) > 0 {
		statements = append(statements,
			statement{Statement: "MATCH ()-[r:RELATED]->() DELETE r", Parameters: map[string]any{}},
			statement{Statement: "UNWIND $relations AS rel MATCH (a:Asset {id: rel.sourceId}), (t:Asset {id: rel.targetId}) MERGE (a)-[r:RELATED {type: rel.type}]->(t)", Parameters: map[string]any{"relations": relations}},
		)
	}
	return s.commitNeo4j(ctx, statements)
}

func (s *Service) countNeo4j(ctx context.Context) (int, error) {
	payload := []byte(`{"statements":[{"statement":"MATCH (a:Asset) RETURN count(a) AS total","resultDataContents":["row"]}]}`)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.neo4jURL+"/db/neo4j/tx/commit", bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	request.Header.Set("Content-Type", "application/json")
	if s.neo4jUser != "" {
		request.SetBasicAuth(s.neo4jUser, s.neo4jPassword)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<16))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return 0, fmt.Errorf("%s: %s", response.Status, truncate(body, 200))
	}
	var result struct {
		Results []struct {
			Data []struct {
				Row []any `json:"row"`
			} `json:"data"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, err
	}
	if len(result.Results) == 0 || len(result.Results[0].Data) == 0 || len(result.Results[0].Data[0].Row) == 0 {
		return 0, nil
	}
	if value, ok := result.Results[0].Data[0].Row[0].(float64); ok {
		return int(value), nil
	}
	return 0, nil
}

func (s *Service) ensureOpenSearchIndex(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodHead, s.openSearchURL+"/"+openSearchIndex, nil)
	if err != nil {
		return err
	}
	if s.openSearchUser != "" {
		request.SetBasicAuth(s.openSearchUser, s.openSearchPassword)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusNotFound:
	default:
		return fmt.Errorf("opensearch index probe returned %s", response.Status)
	}
	mapping := map[string]any{
		"settings": map[string]any{"index": map[string]any{"number_of_shards": 1, "number_of_replicas": 0}},
		"mappings": map[string]any{"properties": map[string]any{
			"ID":           map[string]any{"type": "keyword"},
			"Name":         map[string]any{"type": "text", "fields": map[string]any{"keyword": map[string]any{"type": "keyword"}}},
			"Type":         map[string]any{"type": "keyword"},
			"Status":       map[string]any{"type": "keyword"},
			"Environment":  map[string]any{"type": "keyword"},
			"ProjectGroup": map[string]any{"type": "keyword"},
			"Owner":        map[string]any{"type": "keyword"},
			"IP":           map[string]any{"type": "keyword"},
			"Location":     map[string]any{"type": "keyword"},
			"Relations":    map[string]any{"type": "object", "enabled": false},
		}},
	}
	body, err := json.Marshal(mapping)
	if err != nil {
		return err
	}
	request, err = http.NewRequestWithContext(ctx, http.MethodPut, s.openSearchURL+"/"+openSearchIndex, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	if s.openSearchUser != "" {
		request.SetBasicAuth(s.openSearchUser, s.openSearchPassword)
	}
	response, err = s.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, _ = io.ReadAll(io.LimitReader(response.Body, 1<<16))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("opensearch index create returned %s: %s", response.Status, truncate(body, 300))
	}
	return nil
}

func (s *Service) projectOpenSearch(ctx context.Context, assets []Asset) error {
	var payload bytes.Buffer
	for _, asset := range assets {
		action, err := json.Marshal(map[string]any{"index": map[string]any{"_index": openSearchIndex, "_id": asset.ID}})
		if err != nil {
			return err
		}
		document, err := json.Marshal(asset)
		if err != nil {
			return err
		}
		payload.Write(action)
		payload.WriteByte('\n')
		payload.Write(document)
		payload.WriteByte('\n')
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.openSearchURL+"/_bulk?refresh=true", bytes.NewReader(payload.Bytes()))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-ndjson")
	if s.openSearchUser != "" {
		request.SetBasicAuth(s.openSearchUser, s.openSearchPassword)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("opensearch bulk returned %s: %s", response.Status, truncate(body, 300))
	}
	var result struct {
		Errors bool `json:"errors"`
		Items  []map[string]struct {
			ID     string `json:"_id"`
			Status int    `json:"status"`
			Error  *struct {
				Type   string `json:"type"`
				Reason string `json:"reason"`
			} `json:"error"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("opensearch bulk response unreadable: %w", err)
	}
	if result.Errors {
		for _, item := range result.Items {
			for _, entry := range item {
				if entry.Error != nil {
					return fmt.Errorf("opensearch rejected %s (%s, status %d): %s", entry.ID, entry.Error.Type, entry.Status, entry.Error.Reason)
				}
			}
		}
		return fmt.Errorf("opensearch bulk reported errors")
	}
	return nil
}

func (s *Service) countOpenSearch(ctx context.Context) (int, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.openSearchURL+"/"+openSearchIndex+"/_count", nil)
	if err != nil {
		return 0, err
	}
	if s.openSearchUser != "" {
		request.SetBasicAuth(s.openSearchUser, s.openSearchPassword)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<16))
	if response.StatusCode == http.StatusNotFound {
		return 0, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return 0, fmt.Errorf("%s: %s", response.Status, truncate(body, 200))
	}
	var result struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, err
	}
	return result.Count, nil
}

func truncate(body []byte, size int) string {
	text := strings.TrimSpace(string(body))
	if len(text) > size {
		return text[:size] + "..."
	}
	return text
}
