package aiops

import (
	"errors"
	"testing"
)

func TestKnowledgeSpacesAndPrivateIsolation(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	service := NewService()
	admin := KnowledgeActor{Username: "admin", Permissions: []string{"aiops:execute"}}
	viewer := KnowledgeActor{Username: "viewer", Permissions: []string{"aiops:view"}}

	spaces := service.KnowledgeSpaces(admin)
	if len(spaces) < 2 {
		t.Fatalf("expected default and private spaces, got %#v", spaces)
	}
	privateID := privateKnowledgeSpaceID(admin.Username)
	tree, err := service.KnowledgeTree(privateID, admin)
	if err != nil || len(tree.Folders) == 0 {
		t.Fatalf("unexpected private tree: %#v %v", tree, err)
	}
	folderID := tree.Folders[0].ID
	item, err := service.CreateKnowledge(KnowledgeInput{
		SpaceID: privateID, FolderID: folderID, Title: "私有知识", Category: "个人",
		Summary: "summary", Content: "private content", Status: "published", Visibility: "private",
	}, admin.Username)
	if err != nil {
		t.Fatalf("create private knowledge: %v", err)
	}
	if _, err = service.KnowledgeDocument(item.ID, viewer, false); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("viewer must not read private knowledge: %v", err)
	}
	if len(service.KnowledgeDocuments(privateID, "", "", "", false, viewer)) != 0 {
		t.Fatal("viewer must not list private knowledge")
	}
}

func TestKnowledgeFolderACL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	service := NewService()
	admin := KnowledgeActor{Username: "admin", Permissions: []string{"aiops:execute"}}
	bob := KnowledgeActor{Username: "bob", Permissions: []string{"aiops:view"}}

	space, err := service.CreateKnowledgeSpace(KnowledgeSpaceInput{
		Name: "测试项目空间", Kind: "project", Visibility: "project", Owner: "team-a",
	}, admin)
	if err != nil {
		t.Fatalf("create project space: %v", err)
	}
	folder, err := service.CreateKnowledgeFolder(KnowledgeFolderInput{
		SpaceID: space.ID, Name: "数据库",
	}, admin)
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}
	item, err := service.CreateKnowledge(KnowledgeInput{
		SpaceID: space.ID, FolderID: folder.ID, Title: "MySQL 巡检", Category: "数据库",
		Summary: "summary", Content: "content", Status: "published", Visibility: "project",
	}, admin.Username)
	if err != nil {
		t.Fatalf("create document: %v", err)
	}
	if len(service.KnowledgeDocuments(space.ID, "", "", "", true, bob)) != 0 {
		t.Fatal("bob must not see project space before ACL")
	}
	if _, err = service.SetKnowledgeACL("folder", folder.ID, []KnowledgeACLInput{{
		SubjectType: "user", SubjectID: "bob", Permission: "view", Effect: "allow", Inherit: true,
	}}, admin); err != nil {
		t.Fatalf("grant ACL: %v", err)
	}
	items := service.KnowledgeDocuments(space.ID, "", "", "", true, bob)
	if len(items) != 1 || items[0].ID != item.ID {
		t.Fatalf("bob should see document after ACL: %#v", items)
	}
}
