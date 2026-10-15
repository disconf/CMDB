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

// Enabled reports whether at least one projection target is configured.
func (s *Service) Enabled() bool {
	return s != nil && (s.neo4jURL != "" || s.openSearchURL != "")
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

// ProjectAssets performs a full rebuild: every asset node is upserted, each one
// has its outgoing RELATED edges recreated, and projections for assets that no
// longer exist in PostgreSQL are removed. Use UpsertAssets for change-driven
// updates of a subset.
func (s *Service) ProjectAssets(ctx context.Context, assets []Asset) (int, int, error) {
	if s.openSearchURL != "" {
		// Normalise before writing: the prune query below needs ID to be an
		// exact keyword field, and a full rebuild rewrites every document anyway.
		if err := s.normalizeOpenSearchIndex(ctx); err != nil {
			return 0, 0, err
		}
	}
	neo4jCount, openSearchCount, err := s.UpsertAssets(ctx, assets)
	if err != nil {
		return neo4jCount, openSearchCount, err
	}
	identifiers := make([]string, 0, len(assets))
	for _, asset := range assets {
		identifiers = append(identifiers, asset.ID)
	}
	if err = s.pruneProjections(ctx, identifiers); err != nil {
		return neo4jCount, openSearchCount, err
	}
	return neo4jCount, openSearchCount, nil
}

// pruneProjections drops graph nodes and search documents whose asset no longer
// exists in PostgreSQL, so a rebuild always converges on the source of truth.
func (s *Service) pruneProjections(ctx context.Context, keep []string) error {
	if s.neo4jURL != "" {
		if err := s.commitNeo4j(ctx, []statement{{Statement: "MATCH (a:Asset) WHERE NOT a.id IN $ids DETACH DELETE a", Parameters: map[string]any{"ids": keep}}}); err != nil {
			return err
		}
	}
	if s.openSearchURL == "" {
		return nil
	}
	// An empty terms list is rejected by OpenSearch, so fall back to match_all.
	// ID is guaranteed to be an exact keyword field by normalizeOpenSearchIndex.
	selector := map[string]any{"match_all": map[string]any{}}
	if len(keep) > 0 {
		selector = map[string]any{"bool": map[string]any{"must_not": []any{map[string]any{"terms": map[string]any{"ID": keep}}}}}
	}
	payload, err := json.Marshal(map[string]any{"query": selector})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.openSearchURL+"/"+openSearchIndex+"/_delete_by_query?refresh=true&conflicts=proceed", bytes.NewReader(payload))
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
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<16))
	if response.StatusCode == http.StatusNotFound {
		return nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("opensearch prune returned %s: %s", response.Status, truncate(body, 300))
	}
	return nil
}

// UpsertAssets mirrors the given assets into Neo4j and OpenSearch without
// removing graph edges that belong to other assets.
func (s *Service) UpsertAssets(ctx context.Context, assets []Asset) (int, int, error) {
	neo4jCount, openSearchCount := 0, 0
	if len(assets) == 0 {
		return 0, 0, nil
	}
	if s.neo4jURL != "" {
		// Schema changes must run in their own transaction, and a missing
		// constraint only costs lookup speed, so failures never block the sync.
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

// DeleteAssets removes the given assets from both projections.
func (s *Service) DeleteAssets(ctx context.Context, identifiers []string) error {
	clean := make([]string, 0, len(identifiers))
	for _, identifier := range identifiers {
		if identifier = strings.TrimSpace(identifier); identifier != "" {
			clean = append(clean, identifier)
		}
	}
	if len(clean) == 0 {
		return nil
	}
	if s.neo4jURL != "" {
		if err := s.commitNeo4j(ctx, []statement{{Statement: "UNWIND $ids AS id MATCH (a:Asset {id: id}) DETACH DELETE a", Parameters: map[string]any{"ids": clean}}}); err != nil {
			return err
		}
	}
	if s.openSearchURL != "" {
		var payload bytes.Buffer
		for _, identifier := range clean {
			action, err := json.Marshal(map[string]any{"delete": map[string]any{"_index": openSearchIndex, "_id": identifier}})
			if err != nil {
				return err
			}
			payload.Write(action)
			payload.WriteByte('\n')
		}
		if err := s.sendOpenSearchBulk(ctx, payload.Bytes()); err != nil {
			return err
		}
	}
	return nil
}

// Statements are executed in a single Neo4j transaction, so readers never see a
// half rebuilt graph.
type statement struct {
	Statement  string         `json:"statement"`
	Parameters map[string]any `json:"parameters"`
}

func relationStatement(assets []Asset) statement {
	relations := make([]map[string]string, 0)
	for _, asset := range assets {
		for _, relation := range asset.Relations {
			target := strings.TrimSpace(relation["targetId"])
			if target == "" || target == asset.ID {
				continue
			}
			relations = append(relations, map[string]string{"sourceId": asset.ID, "targetId": target, "type": relation["type"]})
		}
	}
	return statement{Statement: "UNWIND $relations AS rel MATCH (a:Asset {id: rel.sourceId}), (t:Asset {id: rel.targetId}) MERGE (a)-[r:RELATED {type: rel.type}]->(t)", Parameters: map[string]any{"relations": relations}}
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

// projectNeo4j upserts the asset nodes and refreshes only the outgoing edges of
// the assets in this batch.
func (s *Service) projectNeo4j(ctx context.Context, assets []Asset) error {
	rows := make([]map[string]any, 0, len(assets))
	identifiers := make([]string, 0, len(assets))
	hasRelations := false
	for _, asset := range assets {
		rows = append(rows, map[string]any{
			"id": asset.ID, "name": asset.Name, "type": asset.Type, "status": asset.Status,
			"environment": asset.Environment, "projectGroup": asset.ProjectGroup, "owner": asset.Owner,
			"ip": asset.IP, "location": asset.Location,
		})
		identifiers = append(identifiers, asset.ID)
		if len(asset.Relations) > 0 {
			hasRelations = true
		}
	}
	statements := []statement{
		{Statement: "UNWIND $assets AS row MERGE (a:Asset {id: row.id}) SET a.name=row.name, a.type=row.type, a.status=row.status, a.environment=row.environment, a.projectGroup=row.projectGroup, a.owner=row.owner, a.ip=row.ip, a.location=row.location", Parameters: map[string]any{"assets": rows}},
		{Statement: "UNWIND $ids AS id MATCH (a:Asset {id: id}) OPTIONAL MATCH (a)-[stale:RELATED]->() DELETE stale", Parameters: map[string]any{"ids": identifiers}},
	}
	if hasRelations {
		statements = append(statements, relationStatement(assets))
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

// indexIDField reports how the ID property is mapped: "exact" for a pure
// keyword field, "subfield" when dynamic mapping added a keyword sub field, and
// "missing" when the index or the property does not exist yet.
func (s *Service) indexIDField(ctx context.Context) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.openSearchURL+"/"+openSearchIndex+"/_mapping", nil)
	if err != nil {
		return "", err
	}
	if s.openSearchUser != "" {
		request.SetBasicAuth(s.openSearchUser, s.openSearchPassword)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode == http.StatusNotFound {
		return "missing", nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("opensearch mapping probe returned %s: %s", response.Status, truncate(body, 200))
	}
	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", err
	}
	entry, _ := payload[openSearchIndex].(map[string]any)
	mappings, _ := entry["mappings"].(map[string]any)
	properties, _ := mappings["properties"].(map[string]any)
	id, _ := properties["ID"].(map[string]any)
	if id == nil {
		return "missing", nil
	}
	if id["type"] == "keyword" {
		return "exact", nil
	}
	if fields, ok := id["fields"].(map[string]any); ok {
		if _, ok := fields["keyword"].(map[string]any); ok {
			return "subfield", nil
		}
	}
	return "missing", nil
}

// normalizeOpenSearchIndex makes sure the index uses an explicit mapping where
// ID is an exact keyword field. Because a full rebuild rewrites every document,
// an index with an incompatible (dynamically mapped) shape is dropped instead of
// left in a state where prune queries silently match nothing.
func (s *Service) normalizeOpenSearchIndex(ctx context.Context) error {
	state, err := s.indexIDField(ctx)
	if err != nil {
		return err
	}
	if state == "exact" {
		return nil
	}
	deleteRequest, err := http.NewRequestWithContext(ctx, http.MethodDelete, s.openSearchURL+"/"+openSearchIndex, nil)
	if err != nil {
		return err
	}
	if s.openSearchUser != "" {
		deleteRequest.SetBasicAuth(s.openSearchUser, s.openSearchPassword)
	}
	deleteResponse, err := s.client.Do(deleteRequest)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, deleteResponse.Body)
	deleteResponse.Body.Close()
	if deleteResponse.StatusCode != http.StatusOK && deleteResponse.StatusCode != http.StatusNotFound {
		return fmt.Errorf("opensearch index delete returned %s", deleteResponse.Status)
	}
	return s.createOpenSearchIndex(ctx)
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
	return s.createOpenSearchIndex(ctx)
}

// createOpenSearchIndex creates the projection index with an explicit mapping.
func (s *Service) createOpenSearchIndex(ctx context.Context) error {
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
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, s.openSearchURL+"/"+openSearchIndex, bytes.NewReader(body))
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
	return s.sendOpenSearchBulk(ctx, payload.Bytes())
}

// sendOpenSearchBulk posts NDJSON to _bulk. Asset identifiers such as
// "k8s-pod/ops-qa/gateway-abc" contain slashes, so they travel inside the
// request body instead of the URL path.
func (s *Service) sendOpenSearchBulk(ctx context.Context, payload []byte) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.openSearchURL+"/_bulk?refresh=true", bytes.NewReader(payload))
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
