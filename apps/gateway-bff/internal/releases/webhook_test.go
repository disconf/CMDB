package releases

import "testing"

func TestParseHarborEvent(t *testing.T) {
	event, err := ParseProviderEvent("harbor", []byte(`{"type":"PUSH_ARTIFACT","operator":"ci","event_data":{"resources":[{"digest":"sha256:abc","tag":"v1.2.3","resource_url":"harbor.example.com/cmdb/gateway:v1.2.3"}],"repository":{"name":"gateway","namespace":"cmdb","repo_full_name":"cmdb/gateway"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if event.Source != "harbor" || event.Application != "cmdb/gateway" || event.Version != "v1.2.3" || event.Status != StatusSuccess || event.Stage != "artifact" {
		t.Fatalf("unexpected harbor event: %#v", event)
	}
}

func TestParseGitLabPipelineEvent(t *testing.T) {
	event, err := ParseProviderEvent("gitlab", []byte(`{"object_kind":"pipeline","object_attributes":{"id":42,"ref":"main","sha":"abc123","status":"success","url":"https://gitlab.example.com/pipeline/42","environment":"staging"},"project":{"name":"gateway","path_with_namespace":"cmdb/gateway"},"commit":{"id":"abc123"},"user":{"username":"ci"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if event.Source != "gitlab" || event.ExternalID != "gitlab:42" || event.Application != "cmdb/gateway" || event.Version != "main" || event.Status != StatusSuccess || event.Commit != "abc123" {
		t.Fatalf("unexpected gitlab event: %#v", event)
	}
}

func TestParseArgoCDEvent(t *testing.T) {
	event, err := ParseProviderEvent("argocd", []byte(`{"app":"gateway","environment":"ops-qa","syncStatus":"Synced","healthStatus":"Healthy","revision":"abc123","operator":"argocd"}`))
	if err != nil {
		t.Fatal(err)
	}
	if event.Source != "argocd" || event.Application != "gateway" || event.Version != "abc123" || event.Status != StatusSuccess || event.Stage != "verify" {
		t.Fatalf("unexpected argocd event: %#v", event)
	}
}
