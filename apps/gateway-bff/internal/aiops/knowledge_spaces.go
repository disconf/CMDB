package aiops

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	knowledgeDefaultSpaceID = "ks-default"
	knowledgeDefaultSpace   = "运维知识库"
	knowledgeRootFolderName = "未分类"
)

const (
	knowledgePermView    = "view"
	knowledgePermEdit    = "edit"
	knowledgePermPublish = "publish"
	knowledgePermManage  = "manage"
)

func (s *Service) ensureKnowledgeLayout(ctx context.Context) {
	spaces, err := s.repo.LoadKnowledgeSpaces(ctx)
	if err != nil {
		return
	}
	if len(spaces) == 0 {
		now := time.Now().Format("2006-01-02 15:04:05")
		_ = s.repo.SaveKnowledgeSpace(ctx, KnowledgeSpace{
			ID: knowledgeDefaultSpaceID, Name: knowledgeDefaultSpace, Code: "ops-knowledge",
			Description: "运维规范、故障复盘和 Runbook", Kind: "organization",
			Visibility: "organization", Owner: "system", CreatedBy: "system",
			CreatedAt: now, UpdatedAt: now,
		})
		spaces = []KnowledgeSpace{{ID: knowledgeDefaultSpaceID, Name: knowledgeDefaultSpace, Code: "ops-knowledge", Kind: "organization", Visibility: "organization", Owner: "system"}}
	}
	folders, _ := s.repo.LoadKnowledgeFolders(ctx, knowledgeDefaultSpaceID)
	byCategory := map[string]KnowledgeFolder{}
	for _, folder := range folders {
		if folder.ParentID == "" {
			byCategory[folder.Name] = folder
		}
	}
	for index := range s.knowledge {
		item := &s.knowledge[index]
		changed := false
		if item.SpaceID == "" {
			item.SpaceID = knowledgeDefaultSpaceID
			changed = true
		}
		if item.FolderID == "" {
			name := strings.TrimSpace(item.Category)
			if name == "" {
				name = knowledgeRootFolderName
			}
			folder, ok := byCategory[name]
			if !ok {
				now := time.Now().Format("2006-01-02 15:04:05")
				folder = KnowledgeFolder{ID: newID("kf"), SpaceID: knowledgeDefaultSpaceID, Name: name, Path: "/" + name, Owner: "system", CreatedAt: now, UpdatedAt: now}
				_ = s.repo.SaveKnowledgeFolder(ctx, folder)
				byCategory[name] = folder
			}
			item.FolderID = folder.ID
			changed = true
		}
		if item.ContentFormat == "" {
			item.ContentFormat = "markdown"
			changed = true
		}
		if item.Visibility == "" {
			item.Visibility = "organization"
			changed = true
		}
		if item.Owner == "" {
			item.Owner = item.Author
			changed = true
		}
		if item.Version <= 0 {
			item.Version = 1
			changed = true
		}
		if item.Status == "published" && item.PublishedVersion <= 0 {
			item.PublishedVersion = item.Version
			changed = true
		}
		if changed {
			_ = s.repo.SaveKnowledge(ctx, *item)
		}
	}
}

func (s *Service) ensurePrivateSpace(ctx context.Context, actor KnowledgeActor) {
	username := strings.TrimSpace(actor.Username)
	if username == "" {
		return
	}
	spaceID := privateKnowledgeSpaceID(username)
	spaces, err := s.repo.LoadKnowledgeSpaces(ctx)
	if err != nil {
		return
	}
	for _, space := range spaces {
		if space.ID == spaceID {
			return
		}
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	_ = s.repo.SaveKnowledgeSpace(ctx, KnowledgeSpace{
		ID: spaceID, Name: username + " 的文档", Code: spaceID, Description: "个人私有文档",
		Kind: "private", Visibility: "private", Owner: username, CreatedBy: username,
		CreatedAt: now, UpdatedAt: now,
	})
	_ = s.repo.SaveKnowledgeFolder(ctx, KnowledgeFolder{
		ID: spaceID + "-root", SpaceID: spaceID, Name: "我的文档", Path: "/我的文档",
		Owner: username, CreatedAt: now, UpdatedAt: now,
	})
}

func privateKnowledgeSpaceID(username string) string {
	replacer := strings.NewReplacer("@", "-", ".", "-", "_", "-", " ", "-")
	return "ks-private-" + strings.Trim(replacer.Replace(strings.ToLower(username)), "-")
}

func (s *Service) KnowledgeSpaces(actor KnowledgeActor) []KnowledgeSpace {
	ctx := context.Background()
	s.ensureKnowledgeLayout(ctx)
	s.ensurePrivateSpace(ctx, actor)
	spaces, _ := s.repo.LoadKnowledgeSpaces(ctx)
	folders, _ := s.repo.LoadKnowledgeFolders(ctx, "")
	knowledge, _ := s.repo.LoadKnowledge(ctx)
	result := make([]KnowledgeSpace, 0, len(spaces))
	for _, space := range spaces {
		if !s.canViewKnowledgeSpace(actor, space) {
			continue
		}
		for _, folder := range folders {
			if folder.SpaceID == space.ID && s.knowledgeFolderPermission(actor, folder) > 0 {
				space.FolderCount++
			}
		}
		for _, item := range knowledge {
			if item.SpaceID == space.ID && item.Status != "archived" && s.canViewKnowledgeDocument(actor, item) {
				space.DocumentCount++
			}
		}
		space.Private = space.Kind == "private"
		result = append(result, space)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Private != result[j].Private {
			return result[j].Private
		}
		return result[i].Name < result[j].Name
	})
	return result
}

func (s *Service) KnowledgeTree(spaceID string, actor KnowledgeActor) (KnowledgeTree, error) {
	spaces := s.KnowledgeSpaces(actor)
	if spaceID == "" && len(spaces) > 0 {
		spaceID = spaces[0].ID
	}
	var selected KnowledgeSpace
	found := false
	for _, space := range spaces {
		if space.ID == spaceID {
			selected, found = space, true
			break
		}
	}
	if !found {
		return KnowledgeTree{Spaces: spaces}, ErrNotFound
	}
	folders, err := s.repo.LoadKnowledgeFolders(context.Background(), spaceID)
	if err != nil {
		return KnowledgeTree{}, err
	}
	knowledge, _ := s.repo.LoadKnowledge(context.Background())
	counts := map[string]int{}
	visible := map[string]bool{}
	for _, item := range knowledge {
		if item.SpaceID == spaceID && item.Status != "archived" && s.canViewKnowledgeDocument(actor, item) {
			counts[item.FolderID]++
			visible[item.FolderID] = true
		}
	}
	byID := make(map[string]KnowledgeFolder, len(folders))
	for _, folder := range folders {
		byID[folder.ID] = folder
		if s.knowledgeFolderPermission(actor, folder) > 0 {
			visible[folder.ID] = true
		}
	}
	for id := range visible {
		for current := byID[id]; current.ParentID != ""; {
			visible[current.ParentID] = true
			parent, ok := byID[current.ParentID]
			if !ok {
				break
			}
			current = parent
		}
	}
	visibleFolders := make([]KnowledgeFolder, 0, len(folders))
	for _, folder := range folders {
		if !visible[folder.ID] {
			continue
		}
		folder.DocumentCount = counts[folder.ID]
		visibleFolders = append(visibleFolders, folder)
	}
	return KnowledgeTree{Spaces: spaces, Folders: buildKnowledgeFolderTree(visibleFolders), Selected: selected.ID}, nil
}

func buildKnowledgeFolderTree(folders []KnowledgeFolder) []KnowledgeFolder {
	byParent := map[string][]KnowledgeFolder{}
	for _, folder := range folders {
		byParent[folder.ParentID] = append(byParent[folder.ParentID], folder)
	}
	var build func(parent string) []KnowledgeFolder
	build = func(parent string) []KnowledgeFolder {
		items := byParent[parent]
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].SortOrder != items[j].SortOrder {
				return items[i].SortOrder < items[j].SortOrder
			}
			return items[i].Name < items[j].Name
		})
		for index := range items {
			items[index].Children = build(items[index].ID)
		}
		return items
	}
	return build("")
}

func (s *Service) CreateKnowledgeSpace(input KnowledgeSpaceInput, actor KnowledgeActor) (KnowledgeSpace, error) {
	if !s.actorCan(actor, knowledgePermManage) {
		return KnowledgeSpace{}, ErrUnauthorized
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return KnowledgeSpace{}, ErrValidation
	}
	kind := strings.ToLower(strings.TrimSpace(input.Kind))
	if kind == "" {
		kind = "organization"
	}
	if kind != "organization" && kind != "project" && kind != "private" {
		return KnowledgeSpace{}, ErrValidation
	}
	visibility := strings.ToLower(strings.TrimSpace(input.Visibility))
	if visibility == "" {
		visibility = "organization"
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	code := strings.TrimSpace(input.Code)
	if code == "" {
		code = newID("ks")
	}
	space := KnowledgeSpace{
		ID: newID("ks"), Name: name, Code: code, Description: strings.TrimSpace(input.Description),
		Kind: kind, Visibility: visibility, Owner: strings.TrimSpace(input.Owner),
		CreatedBy: actor.Username, CreatedAt: now, UpdatedAt: now,
	}
	if space.Owner == "" {
		space.Owner = actor.Username
	}
	if err := s.repo.SaveKnowledgeSpace(context.Background(), space); err != nil {
		return KnowledgeSpace{}, err
	}
	return space, nil
}

func (s *Service) UpdateKnowledgeSpace(id string, input KnowledgeSpaceInput, actor KnowledgeActor) (KnowledgeSpace, error) {
	if !s.actorCan(actor, knowledgePermManage) {
		return KnowledgeSpace{}, ErrUnauthorized
	}
	spaces, err := s.repo.LoadKnowledgeSpaces(context.Background())
	if err != nil {
		return KnowledgeSpace{}, err
	}
	for _, space := range spaces {
		if space.ID != id {
			continue
		}
		if name := strings.TrimSpace(input.Name); name != "" {
			space.Name = name
		}
		if input.Description != "" {
			space.Description = strings.TrimSpace(input.Description)
		}
		if input.Visibility != "" {
			space.Visibility = strings.ToLower(strings.TrimSpace(input.Visibility))
		}
		if input.Owner != "" {
			space.Owner = strings.TrimSpace(input.Owner)
		}
		space.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
		if err = s.repo.SaveKnowledgeSpace(context.Background(), space); err != nil {
			return KnowledgeSpace{}, err
		}
		return space, nil
	}
	return KnowledgeSpace{}, ErrNotFound
}

func (s *Service) DeleteKnowledgeSpace(id string, actor KnowledgeActor) error {
	if !s.actorCan(actor, knowledgePermManage) {
		return ErrUnauthorized
	}
	folders, _ := s.repo.LoadKnowledgeFolders(context.Background(), id)
	knowledge, _ := s.repo.LoadKnowledge(context.Background())
	if len(folders) > 0 {
		return ErrConflict
	}
	for _, item := range knowledge {
		if item.SpaceID == id {
			return ErrConflict
		}
	}
	return s.repo.DeleteKnowledgeSpace(context.Background(), id)
}

func (s *Service) CreateKnowledgeFolder(input KnowledgeFolderInput, actor KnowledgeActor) (KnowledgeFolder, error) {
	if !s.actorCan(actor, knowledgePermEdit) {
		return KnowledgeFolder{}, ErrUnauthorized
	}
	spaceID := strings.TrimSpace(input.SpaceID)
	if spaceID == "" || strings.TrimSpace(input.Name) == "" {
		return KnowledgeFolder{}, ErrValidation
	}
	spaces := s.KnowledgeSpaces(actor)
	allowed := false
	for _, space := range spaces {
		if space.ID == spaceID {
			allowed = true
			break
		}
	}
	if !allowed {
		return KnowledgeFolder{}, ErrUnauthorized
	}
	path := "/" + strings.TrimSpace(input.Name)
	if parentID := strings.TrimSpace(input.ParentID); parentID != "" {
		parent, err := s.knowledgeFolder(parentID)
		if err != nil || parent.SpaceID != spaceID {
			return KnowledgeFolder{}, ErrValidation
		}
		if s.knowledgeFolderPermission(actor, parent) < permissionRank(knowledgePermEdit) {
			return KnowledgeFolder{}, ErrUnauthorized
		}
		path = strings.TrimRight(parent.Path, "/") + "/" + strings.TrimSpace(input.Name)
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	folder := KnowledgeFolder{
		ID: newID("kf"), SpaceID: spaceID, ParentID: strings.TrimSpace(input.ParentID),
		Name: strings.TrimSpace(input.Name), Path: path, Description: strings.TrimSpace(input.Description),
		Owner: strings.TrimSpace(input.Owner), SortOrder: input.SortOrder, CreatedAt: now, UpdatedAt: now,
	}
	if folder.Owner == "" {
		folder.Owner = actor.Username
	}
	if err := s.repo.SaveKnowledgeFolder(context.Background(), folder); err != nil {
		return KnowledgeFolder{}, err
	}
	return folder, nil
}

func (s *Service) UpdateKnowledgeFolder(id string, input KnowledgeFolderInput, actor KnowledgeActor) (KnowledgeFolder, error) {
	folder, err := s.knowledgeFolder(id)
	if err != nil {
		return KnowledgeFolder{}, err
	}
	if !s.canEditKnowledgeFolder(actor, folder) {
		return KnowledgeFolder{}, ErrUnauthorized
	}
	if name := strings.TrimSpace(input.Name); name != "" {
		folder.Name = name
	}
	if input.Description != "" {
		folder.Description = strings.TrimSpace(input.Description)
	}
	if input.Owner != "" {
		folder.Owner = strings.TrimSpace(input.Owner)
	}
	folder.SortOrder = input.SortOrder
	folder.Path = s.folderPath(folder.ParentID, folder.Name)
	folder.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
	if err = s.repo.SaveKnowledgeFolder(context.Background(), folder); err != nil {
		return KnowledgeFolder{}, err
	}
	return folder, nil
}

func (s *Service) MoveKnowledgeFolder(id, parentID string, actor KnowledgeActor) (KnowledgeFolder, error) {
	folder, err := s.knowledgeFolder(id)
	if err != nil {
		return KnowledgeFolder{}, err
	}
	if !s.canEditKnowledgeFolder(actor, folder) {
		return KnowledgeFolder{}, ErrUnauthorized
	}
	if parentID == id {
		return KnowledgeFolder{}, ErrValidation
	}
	if parentID != "" {
		parent, err := s.knowledgeFolder(parentID)
		if err != nil || parent.SpaceID != folder.SpaceID {
			return KnowledgeFolder{}, ErrValidation
		}
		for current := parent; current.ID != ""; {
			if current.ID == id {
				return KnowledgeFolder{}, ErrValidation
			}
			current, _ = s.knowledgeFolder(current.ParentID)
		}
	}
	folder.ParentID = parentID
	folder.Path = s.folderPath(parentID, folder.Name)
	folder.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
	if err = s.repo.SaveKnowledgeFolder(context.Background(), folder); err != nil {
		return KnowledgeFolder{}, err
	}
	return folder, nil
}

func (s *Service) DeleteKnowledgeFolder(id string, actor KnowledgeActor) error {
	folder, err := s.knowledgeFolder(id)
	if err != nil {
		return err
	}
	if !s.canEditKnowledgeFolder(actor, folder) {
		return ErrUnauthorized
	}
	folders, _ := s.repo.LoadKnowledgeFolders(context.Background(), folder.SpaceID)
	for _, item := range folders {
		if item.ParentID == id {
			return ErrConflict
		}
	}
	knowledge, _ := s.repo.LoadKnowledge(context.Background())
	for _, item := range knowledge {
		if item.FolderID == id && item.Status != "archived" {
			return ErrConflict
		}
	}
	return s.repo.DeleteKnowledgeFolder(context.Background(), id)
}

func (s *Service) KnowledgeDocuments(spaceID, folderID, query, status string, includeSubfolders bool, actor KnowledgeActor) []Knowledge {
	ctx := context.Background()
	s.ensureKnowledgeLayout(ctx)
	knowledge, _ := s.repo.LoadKnowledge(ctx)
	folders, _ := s.repo.LoadKnowledgeFolders(ctx, spaceID)
	folderSet := map[string]bool{}
	if folderID != "" {
		folderSet[folderID] = true
		if includeSubfolders {
			queue := []string{folderID}
			for len(queue) > 0 {
				parent := queue[0]
				queue = queue[1:]
				for _, folder := range folders {
					if folder.ParentID == parent && !folderSet[folder.ID] {
						folderSet[folder.ID] = true
						queue = append(queue, folder.ID)
					}
				}
			}
		}
	}
	query = strings.ToLower(strings.TrimSpace(query))
	result := []Knowledge{}
	for _, item := range knowledge {
		if spaceID != "" && item.SpaceID != spaceID {
			continue
		}
		if folderID != "" && !folderSet[item.FolderID] {
			continue
		}
		if status != "" && status != "all" && item.Status != status {
			continue
		}
		if status == "" && item.Status == "archived" {
			continue
		}
		if item.Status == "draft" && !s.canEditKnowledgeDocument(actor, item) {
			continue
		}
		if !s.canViewKnowledgeDocument(actor, item) {
			continue
		}
		if query != "" && knowledgeSearchScore(item, query) == 0 {
			continue
		}
		result = append(result, item)
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].UpdatedAt > result[j].UpdatedAt })
	if len(result) > 500 {
		result = result[:500]
	}
	return result
}

func (s *Service) KnowledgeDocument(id string, actor KnowledgeActor, incrementView bool) (Knowledge, error) {
	s.ensureKnowledgeLayout(context.Background())
	knowledge, _ := s.repo.LoadKnowledge(context.Background())
	for _, item := range knowledge {
		if item.ID != id {
			continue
		}
		if !s.canViewKnowledgeDocument(actor, item) {
			return Knowledge{}, ErrUnauthorized
		}
		if incrementView {
			item.Views++
			_ = s.repo.SaveKnowledge(context.Background(), item)
		}
		return item, nil
	}
	return Knowledge{}, ErrNotFound
}

func (s *Service) CreateKnowledgeWithActor(input KnowledgeInput, actor KnowledgeActor) (Knowledge, error) {
	if actor.Username == "" || !s.actorCan(actor, knowledgePermEdit) {
		return Knowledge{}, ErrUnauthorized
	}
	s.ensureKnowledgeLayout(context.Background())
	s.ensurePrivateSpace(context.Background(), actor)
	folder, space, err := s.knowledgeTargetFolder(input.SpaceID, input.FolderID, input.Category, actor)
	if err != nil {
		return Knowledge{}, err
	}
	input.SpaceID = space.ID
	input.FolderID = folder.ID
	input.Category = folder.Name
	if space.Kind == "private" {
		input.Visibility = "private"
		input.Owner = actor.Username
	} else if !s.actorCan(actor, knowledgePermManage) {
		input.Owner = actor.Username
	}
	if input.Status == "" {
		input.Status = "published"
	}
	if (input.Status == "published" || input.Status == "archived") && !s.actorCan(actor, knowledgePermPublish) {
		if input.Status == "published" {
			input.Status = "draft"
		} else {
			return Knowledge{}, ErrUnauthorized
		}
	}
	return s.CreateKnowledge(input, actor.Username)
}

func (s *Service) UpdateKnowledgeWithActor(id string, input KnowledgeInput, actor KnowledgeActor) (Knowledge, error) {
	existing, ok := s.knowledgeExists(id)
	if !ok {
		return Knowledge{}, ErrNotFound
	}
	if !s.canEditKnowledgeDocument(actor, existing) {
		return Knowledge{}, ErrUnauthorized
	}
	targetSpaceID := strings.TrimSpace(input.SpaceID)
	if targetSpaceID == "" {
		targetSpaceID = existing.SpaceID
	}
	targetFolderID := strings.TrimSpace(input.FolderID)
	if targetFolderID == "" {
		targetFolderID = existing.FolderID
	}
	if targetSpaceID != existing.SpaceID || targetFolderID != existing.FolderID {
		folder, _, err := s.knowledgeTargetFolder(targetSpaceID, targetFolderID, input.Category, actor)
		if err != nil {
			return Knowledge{}, err
		}
		input.SpaceID, input.FolderID, input.Category = folder.SpaceID, folder.ID, folder.Name
	} else {
		input.SpaceID, input.FolderID = existing.SpaceID, existing.FolderID
	}
	if !s.actorCan(actor, knowledgePermManage) {
		input.Owner = existing.Owner
	}
	if input.Status == "" {
		input.Status = existing.Status
	}
	if (input.Status == "published" || input.Status == "archived") && !s.canPublishKnowledgeDocument(actor, existing) {
		return Knowledge{}, ErrUnauthorized
	}
	return s.UpdateKnowledge(id, input, actor.Username)
}

func (s *Service) knowledgeTargetFolder(spaceID, folderID, category string, actor KnowledgeActor) (KnowledgeFolder, KnowledgeSpace, error) {
	spaceID = strings.TrimSpace(spaceID)
	if spaceID == "" {
		spaceID = knowledgeDefaultSpaceID
	}
	space, err := s.knowledgeSpace(spaceID)
	if err != nil {
		return KnowledgeFolder{}, KnowledgeSpace{}, ErrValidation
	}
	if !s.canViewKnowledgeSpace(actor, space) {
		return KnowledgeFolder{}, KnowledgeSpace{}, ErrUnauthorized
	}
	var folder KnowledgeFolder
	if folderID = strings.TrimSpace(folderID); folderID == "" {
		folder = s.knowledgeFolderForCategoryInSpace(spaceID, category)
	} else {
		folder, err = s.knowledgeFolder(folderID)
		if err != nil || folder.SpaceID != spaceID {
			return KnowledgeFolder{}, KnowledgeSpace{}, ErrValidation
		}
	}
	if s.knowledgeFolderPermission(actor, folder) < permissionRank(knowledgePermEdit) {
		return KnowledgeFolder{}, KnowledgeSpace{}, ErrUnauthorized
	}
	return folder, space, nil
}

func (s *Service) knowledgeFolderForCategoryInSpace(spaceID, category string) KnowledgeFolder {
	name := strings.TrimSpace(category)
	if name == "" {
		name = knowledgeRootFolderName
	}
	folders, _ := s.repo.LoadKnowledgeFolders(context.Background(), spaceID)
	for _, folder := range folders {
		if folder.ParentID == "" && strings.EqualFold(folder.Name, name) {
			return folder
		}
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	folder := KnowledgeFolder{
		ID: newID("kf"), SpaceID: spaceID, Name: name, Path: "/" + name,
		Owner: "system", CreatedAt: now, UpdatedAt: now,
	}
	_ = s.repo.SaveKnowledgeFolder(context.Background(), folder)
	return folder
}

func (s *Service) CanAccessKnowledge(id string, actor KnowledgeActor, permission string) bool {
	knowledge, _ := s.repo.LoadKnowledge(context.Background())
	for _, item := range knowledge {
		if item.ID != id {
			continue
		}
		switch permission {
		case knowledgePermEdit:
			return s.canEditKnowledgeDocument(actor, item)
		case knowledgePermPublish:
			return s.canPublishKnowledgeDocument(actor, item)
		default:
			return s.canViewKnowledgeDocument(actor, item)
		}
	}
	return false
}

func (s *Service) CanAccessKnowledgeFolder(id string, actor KnowledgeActor, permission string) bool {
	folder, err := s.knowledgeFolder(id)
	if err != nil {
		return false
	}
	return s.knowledgeFolderPermission(actor, folder) >= permissionRank(permission)
}

func (s *Service) MoveKnowledgeDocument(id, spaceID, folderID string, actor KnowledgeActor) (Knowledge, error) {
	knowledge, _ := s.repo.LoadKnowledge(context.Background())
	for _, item := range knowledge {
		if item.ID != id {
			continue
		}
		if !s.canEditKnowledgeDocument(actor, item) {
			return Knowledge{}, ErrUnauthorized
		}
		if spaceID == "" {
			spaceID = item.SpaceID
		}
		folder, err := s.knowledgeFolder(folderID)
		if err != nil || folder.SpaceID != spaceID {
			return Knowledge{}, ErrValidation
		}
		if s.knowledgeFolderPermission(actor, folder) < permissionRank(knowledgePermEdit) {
			return Knowledge{}, ErrUnauthorized
		}
		item.SpaceID, item.FolderID = spaceID, folderID
		item.Version++
		item.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
		if err = s.repo.SaveKnowledge(context.Background(), item); err != nil {
			return Knowledge{}, err
		}
		_ = s.reloadKnowledge()
		return item, nil
	}
	return Knowledge{}, ErrNotFound
}

func (s *Service) PublishKnowledge(id string, actor KnowledgeActor) (Knowledge, error) {
	knowledge, _ := s.repo.LoadKnowledge(context.Background())
	for _, item := range knowledge {
		if item.ID != id {
			continue
		}
		if !s.canPublishKnowledgeDocument(actor, item) {
			return Knowledge{}, ErrUnauthorized
		}
		item.Status = "published"
		item.Version++
		item.PublishedVersion = item.Version
		item.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
		if err := s.repo.SaveKnowledge(context.Background(), item); err != nil {
			return Knowledge{}, err
		}
		_ = s.reloadKnowledge()
		_ = s.indexKnowledge(item)
		_, _ = s.saveKnowledgeVersion(item, actor.Username, "发布知识")
		return item, nil
	}
	return Knowledge{}, ErrNotFound
}

func (s *Service) ArchiveKnowledge(id string, actor KnowledgeActor) (Knowledge, error) {
	knowledge, _ := s.repo.LoadKnowledge(context.Background())
	for _, item := range knowledge {
		if item.ID != id {
			continue
		}
		if !s.canPublishKnowledgeDocument(actor, item) {
			return Knowledge{}, ErrUnauthorized
		}
		item.Status = "archived"
		item.Version++
		item.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
		if err := s.repo.SaveKnowledge(context.Background(), item); err != nil {
			return Knowledge{}, err
		}
		_ = s.reloadKnowledge()
		_, _ = s.saveKnowledgeVersion(item, actor.Username, "归档知识")
		return item, nil
	}
	return Knowledge{}, ErrNotFound
}

func (s *Service) KnowledgeACL(resourceType, resourceID string, actor KnowledgeActor) []KnowledgeACL {
	if actor.Username == "" {
		return []KnowledgeACL{}
	}
	if resourceType != "folder" && resourceType != "document" {
		return []KnowledgeACL{}
	}
	items, _ := s.repo.LoadKnowledgeACL(context.Background(), resourceType, resourceID)
	return items
}

func (s *Service) SetKnowledgeACL(resourceType, resourceID string, entries []KnowledgeACLInput, actor KnowledgeActor) ([]KnowledgeACL, error) {
	if !s.actorCan(actor, knowledgePermManage) {
		return nil, ErrUnauthorized
	}
	if resourceType != "folder" && resourceType != "document" {
		return nil, ErrValidation
	}
	existing, _ := s.repo.LoadKnowledgeACL(context.Background(), resourceType, resourceID)
	for _, item := range existing {
		_ = s.repo.DeleteKnowledgeACL(context.Background(), item.ID)
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	result := []KnowledgeACL{}
	for _, input := range entries {
		subjectType := strings.ToLower(strings.TrimSpace(input.SubjectType))
		subjectID := strings.TrimSpace(input.SubjectID)
		permission := strings.ToLower(strings.TrimSpace(input.Permission))
		effect := strings.ToLower(strings.TrimSpace(input.Effect))
		if subjectID == "" || (subjectType != "user" && subjectType != "role" && subjectType != "project_group") {
			continue
		}
		if permission != knowledgePermView && permission != knowledgePermEdit && permission != knowledgePermPublish && permission != knowledgePermManage {
			continue
		}
		if effect == "" {
			effect = "allow"
		}
		item := KnowledgeACL{
			ID: newID("kacl"), ResourceType: resourceType, ResourceID: resourceID,
			SubjectType: subjectType, SubjectID: subjectID, Permission: permission,
			Effect: effect, Inherit: input.Inherit, CreatedBy: actor.Username, CreatedAt: now,
		}
		if err := s.repo.SaveKnowledgeACL(context.Background(), item); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

func (s *Service) knowledgeFolder(id string) (KnowledgeFolder, error) {
	if strings.TrimSpace(id) == "" {
		return KnowledgeFolder{}, ErrValidation
	}
	folders, _ := s.repo.LoadKnowledgeFolders(context.Background(), "")
	for _, folder := range folders {
		if folder.ID == id {
			return folder, nil
		}
	}
	return KnowledgeFolder{}, ErrNotFound
}

func (s *Service) knowledgeFolderForCategory(category string) KnowledgeFolder {
	name := strings.TrimSpace(category)
	if name == "" {
		name = knowledgeRootFolderName
	}
	folders, _ := s.repo.LoadKnowledgeFolders(context.Background(), knowledgeDefaultSpaceID)
	for _, folder := range folders {
		if folder.ParentID == "" && strings.EqualFold(folder.Name, name) {
			return folder
		}
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	folder := KnowledgeFolder{
		ID: newID("kf"), SpaceID: knowledgeDefaultSpaceID, Name: name,
		Path: "/" + name, Owner: "system", CreatedAt: now, UpdatedAt: now,
	}
	_ = s.repo.SaveKnowledgeFolder(context.Background(), folder)
	return folder
}

func (s *Service) folderPath(parentID, name string) string {
	path := "/" + strings.TrimSpace(name)
	if parentID == "" {
		return path
	}
	if parent, err := s.knowledgeFolder(parentID); err == nil {
		return strings.TrimRight(parent.Path, "/") + path
	}
	return path
}

func (s *Service) canViewKnowledgeSpace(actor KnowledgeActor, space KnowledgeSpace) bool {
	if space.Kind == "private" {
		return space.Owner == actor.Username
	}
	if s.actorCan(actor, knowledgePermManage) {
		return true
	}
	switch space.Kind {
	case "project":
		if space.Owner == actor.Username || containsString(actor.ProjectGroups, space.Owner) {
			return true
		}
		if s.aclAllows(actor, "space", space.ID, knowledgePermView) {
			return true
		}
	default:
		if s.actorCan(actor, knowledgePermView) {
			return true
		}
	}
	ctx := context.Background()
	folders, _ := s.repo.LoadKnowledgeFolders(ctx, space.ID)
	for _, folder := range folders {
		if s.knowledgeFolderPermission(actor, folder) > 0 {
			return true
		}
	}
	knowledge, _ := s.repo.LoadKnowledge(ctx)
	for _, item := range knowledge {
		if item.SpaceID == space.ID && s.canViewKnowledgeDocument(actor, item) {
			return true
		}
	}
	return false
}

func (s *Service) canViewKnowledgeDocument(actor KnowledgeActor, item Knowledge) bool {
	return s.knowledgeDocumentPermission(actor, item) >= 1
}

func (s *Service) canEditKnowledgeDocument(actor KnowledgeActor, item Knowledge) bool {
	return s.knowledgeDocumentPermission(actor, item) >= 2
}

func (s *Service) canPublishKnowledgeDocument(actor KnowledgeActor, item Knowledge) bool {
	return s.knowledgeDocumentPermission(actor, item) >= 3
}

func (s *Service) canEditKnowledgeFolder(actor KnowledgeActor, folder KnowledgeFolder) bool {
	return s.knowledgeFolderPermission(actor, folder) >= permissionRank(knowledgePermEdit)
}

func (s *Service) knowledgeDocumentPermission(actor KnowledgeActor, item Knowledge) int {
	if actor.Username == "" {
		return 0
	}
	space, err := s.knowledgeSpace(item.SpaceID)
	if err != nil {
		return 0
	}
	if space.Kind == "private" {
		if space.Owner == actor.Username {
			return 4
		}
		return 0
	}
	if s.actorCan(actor, knowledgePermManage) {
		return 4
	}
	rank := s.knowledgeBasePermission(actor, space)
	if item.Owner == actor.Username && item.Owner != "" && rank >= permissionRank(knowledgePermEdit) {
		rank = maxKnowledgeRank(rank, permissionRank(knowledgePermPublish))
	}
	if folder, err := s.knowledgeFolder(item.FolderID); err == nil {
		for _, ancestor := range s.folderAncestors(folder) {
			denied, allowed := s.aclPermissionWithInheritance(actor, "folder", ancestor.ID, true)
			if denied {
				return 0
			}
			rank = maxKnowledgeRank(rank, allowed)
		}
	}
	denied, allowed := s.aclPermission(actor, "document", item.ID)
	if denied {
		return 0
	}
	rank = maxKnowledgeRank(rank, allowed)
	if item.Status == "draft" && rank < 2 {
		return 0
	}
	if item.Status == "archived" && rank < 4 {
		return 0
	}
	return rank
}

func (s *Service) knowledgeFolderPermission(actor KnowledgeActor, folder KnowledgeFolder) int {
	if actor.Username == "" {
		return 0
	}
	space, err := s.knowledgeSpace(folder.SpaceID)
	if err != nil {
		return 0
	}
	if space.Kind == "private" {
		if space.Owner == actor.Username {
			return 4
		}
		return 0
	}
	if s.actorCan(actor, knowledgePermManage) {
		return 4
	}
	rank := s.knowledgeBasePermission(actor, space)
	if folder.Owner == actor.Username && folder.Owner != "" && rank >= permissionRank(knowledgePermEdit) {
		rank = maxKnowledgeRank(rank, permissionRank(knowledgePermPublish))
	}
	ancestors := s.folderAncestors(folder)
	for index, ancestor := range ancestors {
		denied, allowed := s.aclPermissionWithInheritance(actor, "folder", ancestor.ID, index < len(ancestors)-1)
		if denied {
			return 0
		}
		rank = maxKnowledgeRank(rank, allowed)
	}
	return rank
}

func (s *Service) knowledgeBasePermission(actor KnowledgeActor, space KnowledgeSpace) int {
	rank := 0
	if s.actorCan(actor, knowledgePermView) {
		rank = 1
	}
	if s.actorCan(actor, knowledgePermEdit) {
		rank = 2
	}
	if s.actorCan(actor, knowledgePermPublish) {
		rank = 3
	}
	if space.Kind == "project" && space.Owner != actor.Username && !containsString(actor.ProjectGroups, space.Owner) && !s.aclAllows(actor, "space", space.ID, knowledgePermView) {
		return 0
	}
	return rank
}

func maxKnowledgeRank(left, right int) int {
	if right > left {
		return right
	}
	return left
}

func (s *Service) knowledgeSpace(id string) (KnowledgeSpace, error) {
	spaces, _ := s.repo.LoadKnowledgeSpaces(context.Background())
	for _, space := range spaces {
		if space.ID == id {
			return space, nil
		}
	}
	return KnowledgeSpace{}, ErrNotFound
}

func (s *Service) folderAncestors(folder KnowledgeFolder) []KnowledgeFolder {
	result := []KnowledgeFolder{folder}
	current := folder
	for current.ParentID != "" {
		parent, err := s.knowledgeFolder(current.ParentID)
		if err != nil {
			break
		}
		result = append([]KnowledgeFolder{parent}, result...)
		current = parent
	}
	return result
}

func (s *Service) aclAllows(actor KnowledgeActor, resourceType, resourceID, permission string) bool {
	denied, allowed := s.aclPermission(actor, resourceType, resourceID)
	return !denied && allowed >= permissionRank(permission)
}

func (s *Service) aclPermission(actor KnowledgeActor, resourceType, resourceID string) (bool, int) {
	return s.aclPermissionWithInheritance(actor, resourceType, resourceID, false)
}

func (s *Service) aclPermissionWithInheritance(actor KnowledgeActor, resourceType, resourceID string, inheritedOnly bool) (bool, int) {
	items, _ := s.repo.LoadKnowledgeACL(context.Background(), resourceType, resourceID)
	allowed := 0
	for _, item := range items {
		if inheritedOnly && !item.Inherit {
			continue
		}
		if !knowledgeSubjectMatches(actor, item) {
			continue
		}
		if item.Effect == "deny" {
			return true, 0
		}
		if rank := permissionRank(item.Permission); rank > allowed {
			allowed = rank
		}
	}
	return false, allowed
}

func knowledgeSubjectMatches(actor KnowledgeActor, item KnowledgeACL) bool {
	switch item.SubjectType {
	case "user":
		return item.SubjectID == actor.Username
	case "role":
		return containsString(actor.Roles, item.SubjectID)
	case "project_group":
		return containsString(actor.ProjectGroups, item.SubjectID)
	}
	return false
}

func permissionRank(permission string) int {
	switch permission {
	case knowledgePermView:
		return 1
	case knowledgePermEdit:
		return 2
	case knowledgePermPublish:
		return 3
	case knowledgePermManage:
		return 4
	}
	return 0
}

func (s *Service) actorCan(actor KnowledgeActor, permission string) bool {
	target := permissionRank(permission)
	for _, value := range actor.Permissions {
		switch value {
		case "aiops:execute", "knowledge:manage", "knowledge:" + permission:
			return true
		case "aiops:view":
			if target <= permissionRank(knowledgePermView) {
				return true
			}
		case "knowledge:view":
			if target <= permissionRank(knowledgePermView) {
				return true
			}
		case "knowledge:create", "knowledge:edit", "knowledge:manage_folder":
			if target <= permissionRank(knowledgePermEdit) {
				return true
			}
		case "knowledge:publish":
			if target <= permissionRank(knowledgePermPublish) {
				return true
			}
		}
	}
	for _, role := range actor.Roles {
		if role == "platform-admin" {
			return true
		}
	}
	return false
}

func (s *Service) knowledgePlainText(item Knowledge) string {
	if item.ContentFormat == "html" {
		return stripHTML(item.Content)
	}
	return item.Content
}

func stripHTML(value string) string {
	var builder strings.Builder
	inTag := false
	for _, char := range value {
		switch char {
		case '<':
			inTag = true
		case '>':
			inTag = false
			builder.WriteByte(' ')
		default:
			if !inTag {
				builder.WriteRune(char)
			}
		}
	}
	return strings.Join(strings.Fields(builder.String()), " ")
}

func (s *Service) knowledgeFolderName(id string) string {
	if folder, err := s.knowledgeFolder(id); err == nil {
		return folder.Name
	}
	return "未分类"
}

func (s *Service) knowledgeSpaceName(id string) string {
	if space, err := s.knowledgeSpace(id); err == nil {
		return space.Name
	}
	return "知识库"
}

func knowledgeDocumentTitle(item Knowledge) string {
	if strings.TrimSpace(item.Title) != "" {
		return item.Title
	}
	return fmt.Sprintf("知识 %s", item.ID)
}
