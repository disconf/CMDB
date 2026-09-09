package cmdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrNotFound = errors.New("asset not found")

var runtimeAutoAttrs = map[string]bool{
	"os": true, "kernel": true, "architecture": true, "cpu": true,
	"memory_bytes": true, "disk_bytes": true, "boot_time": true,
	"agent_version": true, "mac_addresses": true,
}
var ErrValidation = errors.New("validation failed")
var ErrConflict = errors.New("asset already exists")

type Service struct {
	mu      sync.RWMutex
	assets  []Asset
	models  []Model
	history map[string][]HistoryEntry
	db      *sql.DB
}

func NewService() *Service {
	assets := []Asset{
		asset("srv-prod-001", "prod-api-01", "physical-server", "物理服务器", "online", "10.20.1.11", "生产", "核心系统组", "张伟", "上海一号机房 / A03-12", "Agent", []string{"核心", "Linux", "API"}),
		asset("srv-prod-002", "prod-api-02", "physical-server", "物理服务器", "online", "10.20.1.12", "生产", "核心系统组", "张伟", "上海一号机房 / A03-13", "Agent", []string{"核心", "Linux", "API"}),
		asset("srv-test-001", "test-runner-01", "physical-server", "物理服务器", "offline", "10.30.2.8", "测试", "研发效能组", "李娜", "上海二号机房 / B01-04", "Agent", []string{"测试", "Runner"}),
		asset("vm-prod-041", "order-service-vm", "virtual-machine", "虚拟机", "online", "10.21.4.41", "生产", "电商业务组", "王强", "VMware / cluster-a", "vCenter", []string{"订单", "Java"}),
		asset("vm-prod-052", "payment-gateway-vm", "virtual-machine", "虚拟机", "warning", "10.21.5.52", "生产", "核心系统组", "周敏", "VMware / cluster-a", "vCenter", []string{"支付", "核心"}),
		asset("cloud-ecs-018", "analytics-worker", "cloud-host", "云主机", "online", "172.18.3.18", "生产", "数据平台组", "陈明", "阿里云 / 华东2 / 可用区B", "Cloud API", []string{"数据", "弹性"}),
		asset("k8s-node-01", "prod-k8s-worker-01", "k8s-node", "K8s 节点", "online", "10.22.1.21", "生产", "容器平台组", "赵峰", "prod-k8s / worker", "Kubernetes API", []string{"K8s", "Worker"}),
		asset("k8s-node-02", "prod-k8s-worker-02", "k8s-node", "K8s 节点", "warning", "10.22.1.22", "生产", "容器平台组", "赵峰", "prod-k8s / worker", "Kubernetes API", []string{"K8s", "Worker"}),
		asset("net-sw-001", "core-switch-01", "network-device", "网络设备", "online", "10.10.0.2", "生产", "基础设施组", "孙磊", "上海一号机房 / 核心区", "SNMP", []string{"核心交换", "Cisco"}),
		asset("db-prod-001", "order-mysql-primary", "database", "数据库", "warning", "10.23.2.31", "生产", "电商业务组", "吴涛", "DB Cluster / order-mysql", "Agent", []string{"MySQL", "主库", "订单"}),
		asset("redis-prod-01", "session-redis", "middleware", "中间件", "online", "10.23.3.41", "生产", "核心系统组", "吴涛", "Redis Cluster / session", "Agent", []string{"Redis", "会话"}),
		asset("lb-prod-01", "public-api-lb", "load-balancer", "负载均衡", "online", "10.20.0.10", "生产", "基础设施组", "孙磊", "上海一号机房 / 网络区", "API", []string{"入口", "HAProxy"}),
	}
	models := []Model{{Code: "physical-server", Name: "物理服务器", Category: "计算", Icon: "Server", Enabled: true}, {Code: "virtual-machine", Name: "虚拟机", Category: "计算", Icon: "Box", Enabled: true}, {Code: "cloud-host", Name: "云主机", Category: "计算", Icon: "Cloud", Enabled: true}, {Code: "k8s-node", Name: "K8s 节点", Category: "容器", Icon: "Container", Enabled: true}, {Code: "network-device", Name: "网络设备", Category: "网络", Icon: "Network", Enabled: true}, {Code: "database", Name: "数据库", Category: "数据", Icon: "Database", Enabled: true}, {Code: "middleware", Name: "中间件", Category: "数据", Icon: "Layers", Enabled: true}, {Code: "load-balancer", Name: "负载均衡", Category: "网络", Icon: "GitFork", Enabled: true}}
	models = applyModelFieldPresets(models)
	service := &Service{assets: assets, models: models, history: make(map[string][]HistoryEntry)}
	if databaseURL := os.Getenv("DATABASE_URL"); databaseURL != "" {
		db, persistedAssets, persistedHistory, err := openPostgres(databaseURL, assets)
		if err != nil {
			panic(fmt.Sprintf("initialize PostgreSQL CMDB repository: %v", err))
		}
		service.db = db
		persistedModels, modelErr := loadModels(context.Background(), db)
		if modelErr != nil {
			panic(fmt.Sprintf("load CMDB models: %v", modelErr))
		}
		if len(persistedModels) == 0 {
			for _, model := range models {
				if err := upsertModel(context.Background(), db, model); err != nil {
					panic(fmt.Sprintf("seed CMDB model: %v", err))
				}
			}
			persistedModels = models
		}
		if err := ensureModelFieldPresets(context.Background(), service.db, persistedModels); err != nil {
			panic(fmt.Sprintf("seed CMDB model fields: %v", err))
		}
		persistedModels, modelErr = loadModels(context.Background(), service.db)
		if modelErr != nil {
			panic(fmt.Sprintf("reload CMDB models: %v", modelErr))
		}
		service.models = persistedModels
		service.assets = persistedAssets
		service.history = persistedHistory
		for index := range service.models {
			service.models[index].Count = 0
			for _, item := range service.assets {
				if item.Type == service.models[index].Code {
					service.models[index].Count++
				}
			}
		}
	}
	return service
}

var modelFieldPresets = map[string][]ModelField{
	"physical-server": {
		{Name: "serial", Label: "???", Type: "text"},
		{Name: "vendor", Label: "??", Type: "text"},
		{Name: "model", Label: "??", Type: "text"},
		{Name: "bmc_ip", Label: "????IP(BMC)", Type: "text"},
		{Name: "idc_name", Label: "????", Type: "text"},
		{Name: "module_name", Label: "??/??", Type: "text"},
		{Name: "rack_no", Label: "???", Type: "text"},
		{Name: "u_position", Label: "U?", Type: "text"},
		{Name: "os_name", Label: "????", Type: "text"},
		{Name: "kernel", Label: "????", Type: "text"},
		{Name: "cpu", Label: "CPU", Type: "text"},
		{Name: "memory", Label: "??", Type: "text"},
		{Name: "disk", Label: "??", Type: "text"},
		{Name: "mac_addresses", Label: "MAC??", Type: "text"},
	},
	"network-device": {
		{Name: "serial", Label: "???", Type: "text"},
		{Name: "vendor", Label: "??", Type: "text"},
		{Name: "model", Label: "??", Type: "text"},
		{Name: "software_version", Label: "????", Type: "text"},
		{Name: "firmware", Label: "????", Type: "text"},
		{Name: "management_ip", Label: "??IP", Type: "text"},
		{Name: "snmp_community_ref", Label: "SNMP Community??", Type: "text"},
	},
}

func applyModelFieldPresets(models []Model) []Model {
	for index := range models {
		if fields, ok := modelFieldPresets[models[index].Code]; ok && len(models[index].Fields) == 0 {
			models[index].Fields = fields
		}
	}
	return models
}

func ensureModelFieldPresets(ctx context.Context, db *sql.DB, models []Model) error {
	for index := range models {
		if len(models[index].Fields) == 0 {
			if fields, ok := modelFieldPresets[models[index].Code]; ok {
				models[index].Fields = fields
				if err := upsertModel(ctx, db, models[index]); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func normalizeAttributes(input []Attribute, fields []ModelField) []Attribute {
	labelBy := make(map[string]string, len(fields))
	for _, f := range fields {
		labelBy[f.Name] = f.Label
	}
	out := make([]Attribute, 0, len(input))
	seen := make(map[string]bool, len(input))
	for _, a := range input {
		name := strings.TrimSpace(a.Name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		if strings.TrimSpace(a.Label) == "" {
			a.Label = labelBy[name]
		}
		if a.Label == "" {
			a.Label = name
		}
		out = append(out, a)
	}
	return out
}

func modelFieldsFor(models []Model, code string) []ModelField {
	for _, m := range models {
		if m.Code == code {
			return m.Fields
		}
	}
	return nil
}

func attributeDiff(before, after []Attribute) []Change {
	bm := make(map[string]string)
	am := make(map[string]string)
	labels := make(map[string]string)
	for _, a := range before {
		bm[a.Name] = a.Value
		labels[a.Name] = a.Label
	}
	for _, a := range after {
		am[a.Name] = a.Value
		if a.Label != "" {
			labels[a.Name] = a.Label
		}
	}
	var changes []Change
	for name, bv := range bm {
		av, ok := am[name]
		if !ok || av != bv {
			changes = append(changes, Change{Field: labels[name], Before: bv, After: av})
		}
	}
	for name, av := range am {
		if _, ok := bm[name]; !ok {
			changes = append(changes, Change{Field: labels[name], Before: "", After: av})
		}
	}
	return changes
}

func asset(id, name, typ, typeName, status, ip, env, group, owner, location, source string, tags []string) Asset {
	return Asset{ID: id, Name: name, Type: typ, TypeName: typeName, Status: status, IP: ip, Environment: env, ProjectGroup: group, Owner: owner, Location: location, Source: source, LastSeenAt: "2026-07-15 13:28:00", Tags: tags, Attributes: []Attribute{{"os", "操作系统", "Rocky Linux 9.4"}, {"cpu", "CPU", "16 Core"}, {"memory", "内存", "64 GB"}, {"importance", "重要级别", map[bool]string{true: "核心", false: "一般"}[env == "生产"]}}, Relations: []Relation{{"belongs-to", "app-order", "订单服务"}, {"located-in", "idc-sh-01", "上海一号机房"}}}
}

func (s *Service) Models() []Model {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Model(nil), s.models...)
}
func (s *Service) CreateModel(in ModelInput) (Model, error) {
	if err := validateModel(in); err != nil {
		return Model{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.models {
		if m.Code == in.Code {
			return Model{}, ErrConflict
		}
	}
	m := Model{Code: in.Code, Name: in.Name, Category: in.Category, Icon: in.Icon, Description: in.Description, Enabled: true, Fields: in.Fields}
	if m.Icon == "" {
		m.Icon = "Box"
	}
	if s.db != nil {
		if err := upsertModel(context.Background(), s.db, m); err != nil {
			return Model{}, err
		}
	}
	s.models = append(s.models, m)
	return m, nil
}
func (s *Service) UpdateModel(code string, in ModelInput) (Model, error) {
	if in.Code == "" {
		in.Code = code
	}
	if in.Code != code {
		return Model{}, ErrValidation
	}
	if err := validateModel(in); err != nil {
		return Model{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.models {
		if s.models[i].Code == code {
			count, enabled := s.models[i].Count, s.models[i].Enabled
			s.models[i] = Model{Code: code, Name: in.Name, Category: in.Category, Icon: in.Icon, Description: in.Description, Enabled: enabled, Fields: in.Fields, Count: count}
			if s.models[i].Icon == "" {
				s.models[i].Icon = "Box"
			}
			if s.db != nil {
				if err := upsertModel(context.Background(), s.db, s.models[i]); err != nil {
					return Model{}, err
				}
			}
			return s.models[i], nil
		}
	}
	return Model{}, ErrNotFound
}
func (s *Service) ToggleModel(code string) (Model, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.models {
		if s.models[i].Code == code {
			s.models[i].Enabled = !s.models[i].Enabled
			if s.db != nil {
				if err := upsertModel(context.Background(), s.db, s.models[i]); err != nil {
					return Model{}, err
				}
			}
			return s.models[i], nil
		}
	}
	return Model{}, ErrNotFound
}
func validateModel(in ModelInput) error {
	if strings.TrimSpace(in.Code) == "" || strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Category) == "" {
		return ErrValidation
	}
	seen := map[string]bool{}
	for _, f := range in.Fields {
		if f.Name == "" || f.Label == "" || (f.Type != "text" && f.Type != "number" && f.Type != "boolean" && f.Type != "date" && f.Type != "select") || seen[f.Name] {
			return ErrValidation
		}
		seen[f.Name] = true
	}
	return nil
}
func (s *Service) Summary() Summary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := Summary{Total: len(s.assets), Models: len(s.models)}
	groups := map[string]struct{}{}
	for _, a := range s.assets {
		groups[a.ProjectGroup] = struct{}{}
		switch a.Status {
		case "online":
			result.Online++
		case "warning":
			result.Warning++
		case "offline":
			result.Offline++
		}
	}
	result.ProjectGroups = len(groups)
	return result
}
func (s *Service) ListAssets(q AssetQuery) AssetPage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PageSize < 1 || q.PageSize > 100 {
		q.PageSize = 20
	}
	filtered := make([]Asset, 0)
	for _, a := range s.assets {
		search := strings.ToLower(q.Search)
		if search != "" && !strings.Contains(strings.ToLower(a.Name+" "+a.ID+" "+a.IP), search) {
			continue
		}
		if q.Type != "" && a.Type != q.Type {
			continue
		}
		if q.Status != "" && a.Status != q.Status {
			continue
		}
		if q.ProjectGroup != "" && a.ProjectGroup != q.ProjectGroup {
			continue
		}
		filtered = append(filtered, cloneAsset(a))
	}
	total := len(filtered)
	start := (q.Page - 1) * q.PageSize
	if start > total {
		start = total
	}
	end := start + q.PageSize
	if end > total {
		end = total
	}
	pages := (total + q.PageSize - 1) / q.PageSize
	return AssetPage{Data: filtered[start:end], Meta: PageMeta{Total: total, Page: q.Page, PageSize: q.PageSize, TotalPages: pages}}
}

// PrometheusTargetGroups returns online Linux Agent assets in Prometheus HTTP-SD format.
// A node_exporter is expected to listen on the supplied port on every managed host.
func (s *Service) PrometheusTargetGroups(port string) []PrometheusTargetGroup {
	if strings.TrimSpace(port) == "" {
		port = "9100"
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	groups := make([]PrometheusTargetGroup, 0)
	for _, a := range s.assets {
		if a.Status != "online" || a.Source != "linux-agent" || strings.TrimSpace(a.IP) == "" {
			continue
		}
		groups = append(groups, PrometheusTargetGroup{
			Targets: []string{a.IP + ":" + port},
			Labels: map[string]string{
				"asset_id":      a.ID,
				"asset_name":    a.Name,
				"environment":   a.Environment,
				"project_group": a.ProjectGroup,
				"service":       "cmdb-host",
			},
		})
	}
	return groups
}

func (s *Service) MonitoringAssets() []Asset {
	s.mu.RLock()
	defer s.mu.RUnlock()
	assets := make([]Asset, 0)
	for _, asset := range s.assets {
		if asset.Source == "linux-agent" {
			assets = append(assets, cloneAsset(asset))
		}
	}
	return assets
}
func (s *Service) FindHostByIP(ip string, exceptID string) string {
	if ip == "" {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, a := range s.assets {
		if a.IP == ip && a.ID != exceptID && (a.Type == "physical-server" || a.Type == "virtual-machine" || a.Type == "k8s-node") {
			return a.ID
		}
	}
	return ""
}

func (s *Service) MergeHostInventory(id, source string, attrs []Attribute) (Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.assets {
		if s.assets[index].ID != id {
			continue
		}
		before := s.assets[index]
		target := &s.assets[index]
		byName := make(map[string]int, len(target.Attributes))
		for i, a := range target.Attributes {
			byName[a.Name] = i
		}
		for _, a := range attrs {
			if !runtimeAutoAttrs[a.Name] {
				continue
			}
			if idx, ok := byName[a.Name]; ok {
				target.Attributes[idx].Value = a.Value
				if target.Attributes[idx].Label == "" {
					target.Attributes[idx].Label = a.Label
				}
			} else {
				target.Attributes = append(target.Attributes, Attribute{Name: a.Name, Label: a.Label, Value: a.Value})
				byName[a.Name] = len(target.Attributes) - 1
			}
		}
		target.Status = "online"
		changes := attributeDiff(before.Attributes, target.Attributes)
		entry := HistoryEntry{ID: fmt.Sprintf("hist-%d", time.Now().UnixNano()), AssetID: id, Action: "merged-by-" + source, Operator: "discovery", OccurredAt: time.Now().Format("2006-01-02 15:04:05"), Changes: changes}
		if s.db != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			tx, err := s.db.BeginTx(ctx, nil)
			if err != nil {
				return Asset{}, err
			}
			attrsJSON, _ := json.Marshal(target.Attributes)
			_, err = tx.ExecContext(ctx, `UPDATE cmdb_assets SET status='online',attributes=$2,updated_at=now() WHERE id=$1`, id, attrsJSON)
			if err == nil {
				err = insertHistory(ctx, tx, entry)
			}
			if err == nil {
				err = insertAssetEvents(ctx, tx, *target, entry)
			}
			if err != nil {
				_ = tx.Rollback()
				s.assets[index] = before
				return Asset{}, err
			}
			if err = tx.Commit(); err != nil {
				s.assets[index] = before
				return Asset{}, err
			}
		}
		s.history[id] = append(s.history[id], entry)
		return cloneAsset(*target), nil
	}
	return Asset{}, ErrNotFound
}

func (s *Service) HasAssetIP(ip string, exceptID string) bool {
	if ip == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, a := range s.assets {
		if a.IP == ip && a.ID != exceptID && (a.Type == "physical-server" || a.Type == "virtual-machine" || a.Type == "k8s-node") {
			return true
		}
	}
	return false
}

func (s *Service) GetAsset(id string) (Asset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, a := range s.assets {
		if a.ID == id {
			return cloneAsset(a), nil
		}
	}
	return Asset{}, ErrNotFound
}
func (s *Service) recordStatusEvent(asset Asset, action, operator string) error {
	now := time.Now().Format("2006-01-02 15:04:05")
	entry := HistoryEntry{ID: fmt.Sprintf("hist-%d", time.Now().UnixNano()), AssetID: asset.ID, Action: action, Operator: operator, OccurredAt: now}
	if s.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if err = insertHistory(ctx, tx, entry); err == nil {
			err = insertAssetEvents(ctx, tx, asset, entry)
		}
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	s.history[asset.ID] = append(s.history[asset.ID], entry)
	return nil
}

func (s *Service) UpsertAgentAsset(in AgentAssetInput) (Asset, error) {
	if in.ID == "" || in.Hostname == "" {
		return Asset{}, ErrValidation
	}
	attrs := []Attribute{{Name: "os", Label: "操作系统", Value: in.OS}, {Name: "kernel", Label: "内核", Value: in.Kernel}, {Name: "architecture", Label: "架构", Value: in.Architecture}, {Name: "cpu", Label: "CPU核心", Value: fmt.Sprint(in.CPUCount)}, {Name: "memory_bytes", Label: "内存字节", Value: fmt.Sprint(in.MemoryBytes)}, {Name: "disk_bytes", Label: "磁盘字节", Value: fmt.Sprint(in.DiskBytes)}, {Name: "boot_time", Label: "启动时间", Value: in.BootTime}, {Name: "agent_version", Label: "Agent版本", Value: in.AgentVersion}}
	assetType := strings.TrimSpace(in.Type)
	if assetType == "" {
		assetType = "physical-server"
	}
	typeName := assetType
	for _, m := range s.models {
		if m.Code == assetType && m.Enabled {
			typeName = m.Name
			break
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().Format("2006-01-02 15:04:05")
	for i := range s.assets {
		if s.assets[i].ID == in.ID {
			beforeStatus := s.assets[i].Status
			s.assets[i].Name = in.Hostname
			s.assets[i].IP = in.IP
			s.assets[i].Status = "online"
			s.assets[i].Source = "linux-agent"
			s.assets[i].LastSeenAt = now
			s.assets[i].Attributes = attrs
			if s.db != nil {
				payload, _ := json.Marshal(attrs)
				if _, err := s.db.Exec(`UPDATE cmdb_assets SET name=$2,ip=$3,status='online',source='linux-agent',last_seen_at=now(),attributes=$4,updated_at=now() WHERE id=$1`, in.ID, in.Hostname, in.IP, payload); err != nil {
					return Asset{}, err
				}
			}
			if beforeStatus != "online" {
				if err := s.recordStatusEvent(s.assets[i], "online", "agent"); err != nil {
					return Asset{}, err
				}
			}
			return cloneAsset(s.assets[i]), nil
		}
	}
	asset := Asset{ID: in.ID, Name: in.Hostname, Type: assetType, TypeName: typeName, Status: "online", IP: in.IP, Environment: "待确认", ProjectGroup: "自动发现", Owner: "待分配", Source: "linux-agent", LastSeenAt: now, Tags: []string{"Linux", "Agent"}, Attributes: attrs, Relations: []Relation{}}
	if s.db != nil {
		if err := insertAsset(context.Background(), s.db, asset); err != nil {
			return Asset{}, err
		}
	}
	s.assets = append(s.assets, asset)
	for i := range s.models {
		if s.models[i].Code == asset.Type {
			s.models[i].Count++
		}
	}
	if err := s.recordStatusEvent(asset, "agent-registered", "agent"); err != nil {
		s.assets = s.assets[:len(s.assets)-1]
		for i := range s.models {
			if s.models[i].Code == asset.Type {
				s.models[i].Count--
			}
		}
		return Asset{}, err
	}
	return cloneAsset(asset), nil
}

func (s *Service) MarkAgentAssetOffline(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.assets {
		if s.assets[i].ID == id && s.assets[i].Source == "linux-agent" {
			if s.assets[i].Status == "offline" {
				return nil
			}
			s.assets[i].Status = "offline"
			if s.db != nil {
				if _, err := s.db.Exec(`UPDATE cmdb_assets SET status='offline',updated_at=now() WHERE id=$1`, id); err != nil {
					return err
				}
			}
			return s.recordStatusEvent(s.assets[i], "offline", "agent-health")
		}
	}
	return ErrNotFound
}

func (s *Service) CreateAsset(input CreateAssetInput, operator string) (Asset, error) {
	if err := validateCreate(input); err != nil {
		return Asset{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.assets {
		if item.ID == input.ID {
			return Asset{}, ErrConflict
		}
	}
	typeName := ""
	modelIndex := -1
	for index := range s.models {
		if s.models[index].Code == input.Type && s.models[index].Enabled {
			typeName = s.models[index].Name
			modelIndex = index
			break
		}
	}
	if typeName == "" {
		return Asset{}, fmt.Errorf("%w: unknown type", ErrValidation)
	}
	attrs := normalizeAttributes(input.Attributes, modelFieldsFor(s.models, input.Type))
	created := Asset{ID: input.ID, Name: input.Name, Type: input.Type, TypeName: typeName, Status: input.Status, IP: input.IP, Environment: input.Environment, ProjectGroup: input.ProjectGroup, Owner: input.Owner, Location: input.Location, Source: input.Source, LastSeenAt: time.Now().Format("2006-01-02 15:04:05"), Tags: append([]string(nil), input.Tags...), Attributes: attrs, Relations: append([]Relation(nil), input.Relations...)}
	entry := HistoryEntry{ID: fmt.Sprintf("hist-%d", time.Now().UnixNano()), AssetID: input.ID, Action: "created", Operator: operator, OccurredAt: time.Now().Format("2006-01-02 15:04:05")}
	if s.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return Asset{}, err
		}
		if err = insertAsset(ctx, tx, created); err == nil {
			err = insertHistory(ctx, tx, entry)
		}
		if err == nil {
			err = insertAssetEvents(ctx, tx, created, entry)
		}
		if err != nil {
			_ = tx.Rollback()
			return Asset{}, err
		}
		if err = tx.Commit(); err != nil {
			return Asset{}, err
		}
	}
	s.models[modelIndex].Count++
	s.assets = append(s.assets, created)
	s.history[input.ID] = append(s.history[input.ID], entry)
	return cloneAsset(created), nil
}
func (s *Service) UpdateAsset(id string, input UpdateAssetInput, operator string) (Asset, error) {
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Owner) == "" {
		return Asset{}, ErrValidation
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.assets {
		if s.assets[index].ID != id {
			continue
		}
		before := s.assets[index]
		target := &s.assets[index]
		target.Name = input.Name
		target.Status = input.Status
		target.IP = input.IP
		target.Environment = input.Environment
		target.ProjectGroup = input.ProjectGroup
		target.Owner = input.Owner
		target.Location = input.Location
		target.Tags = append([]string(nil), input.Tags...)
		if input.Attributes != nil {
			target.Attributes = normalizeAttributes(input.Attributes, modelFieldsFor(s.models, target.Type))
		}
		if input.Relations != nil {
			target.Relations = append([]Relation(nil), input.Relations...)
		}
		changes := diffAsset(before, *target)
		entry := HistoryEntry{ID: fmt.Sprintf("hist-%d", time.Now().UnixNano()), AssetID: id, Action: "updated", Operator: operator, OccurredAt: time.Now().Format("2006-01-02 15:04:05"), Changes: changes}
		if s.db != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			tx, err := s.db.BeginTx(ctx, nil)
			if err != nil {
				s.assets[index] = before
				return Asset{}, err
			}
			tags, _ := json.Marshal(target.Tags)
			attrsJSON, _ := json.Marshal(target.Attributes)
			relsJSON, _ := json.Marshal(target.Relations)
			_, err = tx.ExecContext(ctx, `UPDATE cmdb_assets SET name=$2,status=$3,ip=$4,environment=$5,
				project_group=$6,owner=$7,location=$8,tags=$9,attributes=$10,relations=$11,updated_at=now() WHERE id=$1`,
				id, target.Name, target.Status, target.IP, target.Environment, target.ProjectGroup, target.Owner, target.Location, tags, attrsJSON, relsJSON)
			if err == nil {
				err = insertHistory(ctx, tx, entry)
			}
			if err == nil {
				err = insertAssetEvents(ctx, tx, *target, entry)
			}
			if err != nil {
				_ = tx.Rollback()
				s.assets[index] = before
				return Asset{}, err
			}
			if err = tx.Commit(); err != nil {
				s.assets[index] = before
				return Asset{}, err
			}
		}
		s.history[id] = append(s.history[id], entry)
		return cloneAsset(*target), nil
	}
	return Asset{}, ErrNotFound
}
func (s *Service) History(id string) ([]HistoryEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	found := false
	for _, a := range s.assets {
		if a.ID == id {
			found = true
			break
		}
	}
	if !found {
		return nil, ErrNotFound
	}
	entries := s.history[id]
	return append([]HistoryEntry(nil), entries...), nil
}
func hostAssetType(assetType string) bool {
	return assetType == "physical-server" || assetType == "virtual-machine" || assetType == "k8s-node"
}

func (s *Service) importRow(row CreateAssetInput, operator string) (string, error) {
	targetID := ""
	s.mu.RLock()
	for _, a := range s.assets {
		if a.ID == row.ID {
			targetID = a.ID
			break
		}
		if row.IP != "" && a.IP == row.IP && hostAssetType(a.Type) && a.ID != row.ID {
			targetID = a.ID
			break
		}
	}
	s.mu.RUnlock()
	if targetID != "" {
		if _, err := s.UpdateAsset(targetID, UpdateAssetInput{Name: row.Name, Status: row.Status, IP: row.IP, Environment: row.Environment, ProjectGroup: row.ProjectGroup, Owner: row.Owner, Location: row.Location, Tags: row.Tags, Attributes: row.Attributes, Relations: row.Relations}, operator+"-csv-upsert"); err != nil {
			return "", err
		}
		return "updated", nil
	}
	if _, err := s.CreateAsset(row, operator); err != nil {
		return "", err
	}
	return "created", nil
}

func (s *Service) ImportAssets(rows []CreateAssetInput, upsert bool, operator string) ImportResult {
	result := ImportResult{Total: len(rows), Errors: []ImportError{}}
	for index, row := range rows {
		if !upsert {
			if _, err := s.CreateAsset(row, operator); err != nil {
				result.Errors = append(result.Errors, ImportError{Row: index + 2, ID: row.ID, Message: err.Error()})
				continue
			}
			result.Created++
			continue
		}
		status, err := s.importRow(row, operator)
		if err != nil {
			result.Errors = append(result.Errors, ImportError{Row: index + 2, ID: row.ID, Message: err.Error()})
			continue
		}
		if status == "updated" {
			result.Updated++
		} else {
			result.Created++
		}
	}
	return result
}

// Analytics returns inventory reports (status/type/group/source/completeness).
func (s *Service) Analytics() Analytics {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := Analytics{Status: Summary{Total: len(s.assets), Models: len(s.models)}}
	typeCounts := map[string]int{}
	groupCounts := map[string]int{}
	sourceCounts := map[string]int{}
	for _, a := range s.assets {
		typeCounts[a.Type]++
		groupCounts[a.ProjectGroup]++
		sourceCounts[a.Source]++
		if a.Status == "online" {
			out.Status.Online++
		} else if a.Status == "warning" {
			out.Status.Warning++
		} else {
			out.Status.Offline++
		}
	}
	for _, m := range s.models {
		out.ByType = append(out.ByType, TypeCount{Code: m.Code, Name: m.Name, Count: typeCounts[m.Code]})
	}
	for _, g := range sortedKeys(groupCounts) {
		out.ByGroup = append(out.ByGroup, GroupCount{Group: g, Count: groupCounts[g]})
	}
	for _, src := range sortedKeys(sourceCounts) {
		out.BySource = append(out.BySource, SourceCount{Source: src, Count: sourceCounts[src]})
	}
	total := len(s.assets)
	if total > 0 {
		for _, spec := range [][2]string{{"owner", "负责人"}, {"location", "位置"}, {"ip", "IP地址"}, {"projectGroup", "项目组"}} {
			missing := 0
			for _, a := range s.assets {
				value := strings.TrimSpace(assetString(a, spec[0]))
				if value == "" || value == "待分配" || value == "自动发现" {
					missing++
				}
			}
			out.Completeness = append(out.Completeness, Completeness{Field: spec[0], Label: spec[1], Missing: missing, Total: total, Rate: float64(total-missing) / float64(total)})
		}
	}
	return out
}

func assetString(a Asset, field string) string {
	switch field {
	case "owner":
		return a.Owner
	case "location":
		return a.Location
	case "ip":
		return a.IP
	case "projectGroup":
		return a.ProjectGroup
	}
	return ""
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ModelTemplateCSV returns a CSV header line for a CI model (fixed columns + model fields).
func (s *Service) ModelTemplateCSV(code string) (string, error) {
	fields := []string{"id", "name", "type", "status", "ip", "environment", "projectGroup", "owner", "location", "source", "tags"}
	model := Model{}
	found := false
	for _, m := range s.models {
		if m.Code == code {
			model = m
			found = true
			break
		}
	}
	if !found {
		return "", ErrNotFound
	}
	seen := map[string]bool{}
	for _, name := range fields {
		seen[name] = true
	}
	for _, f := range model.Fields {
		if !seen[f.Name] {
			fields = append(fields, f.Name)
			seen[f.Name] = true
		}
	}
	return strings.Join(fields, ","), nil
}

func validateCreate(input CreateAssetInput) error {
	if strings.TrimSpace(input.ID) == "" || strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Type) == "" || strings.TrimSpace(input.Status) == "" || strings.TrimSpace(input.ProjectGroup) == "" || strings.TrimSpace(input.Owner) == "" {
		return ErrValidation
	}
	return nil
}
func diffAsset(a, b Asset) []Change {
	pairs := [][3]string{{"name", a.Name, b.Name}, {"status", a.Status, b.Status}, {"ip", a.IP, b.IP}, {"projectGroup", a.ProjectGroup, b.ProjectGroup}, {"owner", a.Owner, b.Owner}, {"location", a.Location, b.Location}, {"tags", strings.Join(a.Tags, "|"), strings.Join(b.Tags, "|")}}
	var result []Change
	for _, p := range pairs {
		if p[1] != p[2] {
			result = append(result, Change{Field: p[0], Before: p[1], After: p[2]})
		}
	}
	result = append(result, attributeDiff(a.Attributes, b.Attributes)...)
	return result
}
func cloneAsset(a Asset) Asset {
	a.Tags = append([]string(nil), a.Tags...)
	a.Attributes = append([]Attribute(nil), a.Attributes...)
	a.Relations = append([]Relation(nil), a.Relations...)
	return a
}
