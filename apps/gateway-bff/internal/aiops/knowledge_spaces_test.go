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
	otherAdmin := KnowledgeActor{Username: "other-admin", Roles: []string{"platform-admin"}, Permissions: []string{"aiops:execute"}}

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
	if _, err = service.KnowledgeDocument(item.ID, otherAdmin, false); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("another administrator must not read private knowledge: %v", err)
	}
	for _, space := range service.KnowledgeSpaces(otherAdmin) {
		if space.ID == privateID {
			t.Fatal("another administrator must not see private space")
		}
	}
	if len(service.SearchKnowledgeForActor("私有知识", false, viewer)) != 0 {
		t.Fatal("viewer must not find private knowledge through legacy search")
	}
	if len(service.KnowledgeDocuments(privateID, "", "", "", false, viewer)) != 0 {
		t.Fatal("viewer must not list private knowledge")
	}
}

func TestKnowledgePrivateSpaceIgnoresSpoofedOwner(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	service := NewService()
	admin := KnowledgeActor{Username: "admin", Permissions: []string{"aiops:execute"}}
	viewer := KnowledgeActor{Username: "viewer", Permissions: []string{"aiops:view"}}
	privateID := privateKnowledgeSpaceID(admin.Username)
	tree, err := service.KnowledgeTree(privateID, admin)
	if err != nil || len(tree.Folders) == 0 {
		t.Fatalf("private tree: %#v %v", tree, err)
	}
	item, err := service.CreateKnowledge(KnowledgeInput{
		SpaceID: privateID, FolderID: tree.Folders[0].ID, Title: "负责人伪造测试",
		Category: "个人", Summary: "私有摘要", Content: "私有正文", Status: "published",
		Visibility: "private", Owner: viewer.Username,
	}, admin.Username)
	if err != nil {
		t.Fatalf("create private knowledge: %v", err)
	}
	if _, err = service.KnowledgeDocument(item.ID, viewer, false); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("spoofed owner must not bypass private isolation: %v", err)
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
	spaces := service.KnowledgeSpaces(bob)
	found := false
	for _, candidate := range spaces {
		if candidate.ID == space.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("bob should see project space after folder ACL: %#v", spaces)
	}
	tree, err := service.KnowledgeTree(space.ID, bob)
	if err != nil || len(tree.Folders) == 0 {
		t.Fatalf("bob should see authorized folder in space tree: %#v %v", tree, err)
	}
}

func TestKnowledgeACLInheritanceCanBeDisabled(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	service := NewService()
	admin := KnowledgeActor{Username: "admin", Permissions: []string{"aiops:execute"}}
	bob := KnowledgeActor{Username: "bob", Permissions: []string{"aiops:view"}}
	space, err := service.CreateKnowledgeSpace(KnowledgeSpaceInput{
		Name: "继承测试空间", Kind: "project", Visibility: "project", Owner: "team-b",
	}, admin)
	if err != nil {
		t.Fatalf("create project space: %v", err)
	}
	folder, err := service.CreateKnowledgeFolder(KnowledgeFolderInput{SpaceID: space.ID, Name: "非继承目录"}, admin)
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}
	if _, err = service.CreateKnowledge(KnowledgeInput{
		SpaceID: space.ID, FolderID: folder.ID, Title: "非继承文档", Category: folder.Name,
		Summary: "summary", Content: "content", Status: "published", Visibility: "project",
	}, admin.Username); err != nil {
		t.Fatalf("create document: %v", err)
	}
	if _, err = service.SetKnowledgeACL("folder", folder.ID, []KnowledgeACLInput{{
		SubjectType: "user", SubjectID: bob.Username, Permission: "view", Effect: "allow", Inherit: false,
	}}, admin); err != nil {
		t.Fatalf("grant folder ACL: %v", err)
	}
	tree, err := service.KnowledgeTree(space.ID, bob)
	if err != nil || len(tree.Folders) != 1 {
		t.Fatalf("bob should see the authorized folder itself: %#v %v", tree, err)
	}
	if len(service.KnowledgeDocuments(space.ID, "", "", "", true, bob)) != 0 {
		t.Fatal("non-inherited folder ACL must not expose child documents")
	}
}

func TestKnowledgeCreateWithActorCannotSpoofOwner(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	service := NewService()
	admin := KnowledgeActor{Username: "admin", Permissions: []string{"aiops:execute"}}
	editor := KnowledgeActor{Username: "editor", Permissions: []string{"knowledge:view", "knowledge:edit", "knowledge:publish"}}
	tree, err := service.KnowledgeTree("", editor)
	if err != nil || len(tree.Folders) == 0 {
		t.Fatalf("default tree: %#v %v", tree, err)
	}
	item, err := service.CreateKnowledgeWithActor(KnowledgeInput{
		SpaceID: tree.Selected, FolderID: tree.Folders[0].ID, Title: "负责人保护",
		Summary: "summary", Content: "content", Status: "published", Owner: admin.Username,
	}, editor)
	if err != nil {
		t.Fatalf("create knowledge: %v", err)
	}
	if item.Owner != editor.Username {
		t.Fatalf("non-admin must not spoof owner: %#v", item)
	}
	privateID := privateKnowledgeSpaceID(admin.Username)
	privateActor := KnowledgeActor{Username: admin.Username, Permissions: []string{"aiops:execute"}}
	privateTree, err := service.KnowledgeTree(privateID, privateActor)
	if err != nil || len(privateTree.Folders) == 0 {
		t.Fatalf("private tree: %#v %v", privateTree, err)
	}
	if _, err = service.CreateKnowledgeWithActor(KnowledgeInput{
		SpaceID: privateID, FolderID: privateTree.Folders[0].ID, Title: "越权私有文档",
		Summary: "summary", Content: "content", Status: "draft",
	}, editor); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("editor must not create in another user's private space: %v", err)
	}
}

func TestKnowledgeRAGFiltersPrivateDocuments(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	service := NewService()
	admin := KnowledgeActor{Username: "admin", Permissions: []string{"aiops:execute"}}
	viewer := KnowledgeActor{Username: "viewer", Permissions: []string{"aiops:view"}}
	privateID := privateKnowledgeSpaceID(admin.Username)
	tree, err := service.KnowledgeTree(privateID, admin)
	if err != nil || len(tree.Folders) == 0 {
		t.Fatalf("private tree: %#v %v", tree, err)
	}
	if _, err = service.CreateKnowledge(KnowledgeInput{
		SpaceID: privateID, FolderID: tree.Folders[0].ID, Title: "私有磁盘知识",
		Category: "个人", Summary: "私有磁盘排查", Content: "私有磁盘内容", Status: "published", Visibility: "private",
	}, admin.Username); err != nil {
		t.Fatalf("create private knowledge: %v", err)
	}
	answer, err := service.AskKnowledgeWithActor("私有磁盘知识", 5, viewer)
	if err != nil {
		t.Fatalf("ask as viewer: %v", err)
	}
	if len(answer.Citations) != 0 {
		t.Fatalf("viewer citations must not include private knowledge: %#v", answer.Citations)
	}
}
