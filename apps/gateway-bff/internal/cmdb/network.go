package cmdb

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// NetworkARPEntry links an IP address to a MAC address on a switch port or host interface.
type NetworkARPEntry struct {
	IP             string `json:"ip"`
	MACAddress     string `json:"macAddress,omitempty"`
	VLAN           string `json:"vlan,omitempty"`
	InterfaceIndex string `json:"interfaceIndex,omitempty"`
	AssetID        string `json:"assetId,omitempty"`
	AssetName      string `json:"assetName,omitempty"`
	State          string `json:"state,omitempty"`
	LastSeenAt     string `json:"lastSeenAt,omitempty"`
}

// NetworkPort is a logical or physical port discovered on a network device.
type NetworkPort struct {
	ID              string            `json:"id"`
	AssetID         string            `json:"assetId"`
	AssetName       string            `json:"assetName"`
	IfIndex         string            `json:"ifIndex"`
	Name            string            `json:"name"`
	Description     string            `json:"description,omitempty"`
	Alias           string            `json:"alias,omitempty"`
	AdminStatus     string            `json:"adminStatus"`
	OperStatus      string            `json:"operStatus"`
	SpeedMbps       string            `json:"speedMbps,omitempty"`
	MACAddress      string            `json:"macAddress,omitempty"`
	MACAddresses    []string          `json:"macAddresses,omitempty"`
	ARPEntries      []NetworkARPEntry `json:"arpEntries,omitempty"`
	VLAN            string            `json:"vlan,omitempty"`
	NeighborName    string            `json:"neighborName,omitempty"`
	NeighborPort    string            `json:"neighborPort,omitempty"`
	NeighborChassis string            `json:"neighborChassis,omitempty"`
	LastSeenAt      string            `json:"lastSeenAt,omitempty"`
}

// NetworkInterface is a host NIC discovered by SSH or node_exporter inventory.
type NetworkInterface struct {
	ID          string            `json:"id"`
	AssetID     string            `json:"assetId"`
	AssetName   string            `json:"assetName"`
	Name        string            `json:"name"`
	MACAddress  string            `json:"macAddress,omitempty"`
	IPv4        string            `json:"ipv4,omitempty"`
	MTU         string            `json:"mtu,omitempty"`
	SpeedMbps   string            `json:"speedMbps,omitempty"`
	AdminStatus string            `json:"adminStatus"`
	OperStatus  string            `json:"operStatus"`
	Type        string            `json:"type,omitempty"`
	LastSeenAt  string            `json:"lastSeenAt,omitempty"`
	Neighbors   []NetworkARPEntry `json:"neighbors,omitempty"`
	Link        *NetworkLink      `json:"link,omitempty"`
}

// NetworkLink is a manually maintained or LLDP-discovered physical connection.
type NetworkLink struct {
	ID              string   `json:"id"`
	SourceAssetID   string   `json:"sourceAssetId"`
	SourceAssetName string   `json:"sourceAssetName"`
	SourceInterface string   `json:"sourceInterface"`
	TargetAssetID   string   `json:"targetAssetId"`
	TargetAssetName string   `json:"targetAssetName"`
	TargetPort      string   `json:"targetPort"`
	Status          string   `json:"status"`
	Source          string   `json:"source"`
	Verification    string   `json:"verification"`
	Confidence      int      `json:"confidence"`
	Evidence        []string `json:"evidence,omitempty"`
	LastVerifiedAt  string   `json:"lastVerifiedAt,omitempty"`
	LastSeenAt      string   `json:"lastSeenAt,omitempty"`
}

type NetworkDeviceInventory struct {
	Asset      Asset         `json:"asset"`
	Ports      []NetworkPort `json:"ports"`
	TotalPorts int           `json:"totalPorts"`
	UpPorts    int           `json:"upPorts"`
}

type CreateNetworkLinkInput struct {
	SourceAssetID   string `json:"sourceAssetId"`
	SourceInterface string `json:"sourceInterface"`
	TargetAssetID   string `json:"targetAssetId"`
	TargetPort      string `json:"targetPort"`
}

func (s *Service) NetworkDevices() []NetworkDeviceInventory {
	s.mu.RLock()
	assets := make([]Asset, 0)
	for _, asset := range s.assets {
		if asset.Type == "network-device" || asset.Type == "load-balancer" {
			assets = append(assets, cloneAsset(asset))
		}
	}
	allAssets := make([]Asset, len(s.assets))
	for index := range s.assets {
		allAssets[index] = cloneAsset(s.assets[index])
	}
	s.mu.RUnlock()

	byName := make(map[string]Asset, len(allAssets))
	byIP := make(map[string]Asset, len(allAssets))
	for _, asset := range allAssets {
		byName[normalizeNetworkName(asset.Name)] = asset
		if asset.IP != "" {
			byIP[asset.IP] = asset
		}
	}
	result := make([]NetworkDeviceInventory, 0, len(assets))
	for _, asset := range assets {
		ports := parseNetworkPorts(asset)
		for index := range ports {
			addPortNeighborAsset(&ports[index], byName)
			addPortARPAssets(&ports[index], byIP)
		}
		inventory := NetworkDeviceInventory{Asset: asset, Ports: ports, TotalPorts: len(ports)}
		for _, port := range ports {
			if port.OperStatus == "up" {
				inventory.UpPorts++
			}
		}
		result = append(result, inventory)
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i].Asset.Name) < strings.ToLower(result[j].Asset.Name)
	})
	return result
}

func (s *Service) NetworkPorts(assetID string) ([]NetworkPort, error) {
	asset, err := s.GetAsset(assetID)
	if err != nil {
		return nil, err
	}
	ports := parseNetworkPorts(asset)
	byName := make(map[string]Asset)
	byIP := make(map[string]Asset)
	for _, candidate := range s.InventoryAssets() {
		byName[normalizeNetworkName(candidate.Name)] = candidate
		if candidate.IP != "" {
			byIP[candidate.IP] = candidate
		}
	}
	for index := range ports {
		addPortNeighborAsset(&ports[index], byName)
		addPortARPAssets(&ports[index], byIP)
	}
	return ports, nil
}

func (s *Service) NetworkInterfaces(assetID string) ([]NetworkInterface, error) {
	asset, err := s.GetAsset(assetID)
	if err != nil {
		return nil, err
	}
	interfaces := parseNetworkInterfaces(asset)
	allAssets := s.InventoryAssets()
	byIP := make(map[string]Asset, len(allAssets))
	for _, candidate := range allAssets {
		if candidate.IP != "" {
			byIP[candidate.IP] = candidate
		}
	}
	links := s.NetworkLinks()
	for index := range interfaces {
		interfaces[index].AssetID = asset.ID
		interfaces[index].AssetName = asset.Name
		addInterfaceARPAssets(&interfaces[index], byIP)
		for linkIndex := range links {
			link := links[linkIndex]
			if link.SourceAssetID == asset.ID && strings.EqualFold(link.SourceInterface, interfaces[index].Name) {
				copyLink := link
				interfaces[index].Link = &copyLink
				break
			}
		}
	}
	return interfaces, nil
}

func (s *Service) NetworkLinks() []NetworkLink {
	s.mu.RLock()
	assets := make([]Asset, len(s.assets))
	for index := range s.assets {
		assets[index] = cloneAsset(s.assets[index])
	}
	s.mu.RUnlock()

	byID := make(map[string]Asset, len(assets))
	byName := make(map[string]Asset, len(assets))
	for _, asset := range assets {
		byID[asset.ID] = asset
		byName[normalizeNetworkName(asset.Name)] = asset
	}
	links := make([]NetworkLink, 0)
	seen := make(map[string]bool)
	resolve := func(id, name string) (string, string) {
		if id != "" {
			if target, ok := byID[id]; ok {
				return target.ID, target.Name
			}
		}
		if name != "" {
			if target, ok := byName[normalizeNetworkName(name)]; ok {
				return target.ID, target.Name
			}
		}
		return id, name
	}
	for _, source := range assets {
		for _, relation := range source.Relations {
			if relation.Type != "connects-to" {
				continue
			}
			targetID, targetName := resolve(relation.TargetID, relation.TargetName)
			link := NetworkLink{
				ID: relation.ID, SourceAssetID: source.ID, SourceAssetName: source.Name,
				SourceInterface: relation.SourcePort, TargetAssetID: targetID, TargetAssetName: targetName,
				TargetPort: relation.TargetPort, Status: relation.Status, Source: relation.Source,
				LastSeenAt: relation.LastSeenAt,
			}
			if link.ID == "" {
				link.ID = networkLinkID(link)
			}
			if link.Status == "" {
				link.Status = "active"
			}
			if link.Source == "" {
				link.Source = "manual"
			}
			if !seen[link.ID] {
				seen[link.ID] = true
				links = append(links, link)
			}
		}
	}
	for _, asset := range assets {
		if asset.Type != "network-device" && asset.Type != "load-balancer" {
			continue
		}
		for _, port := range parseNetworkPorts(asset) {
			if port.NeighborName == "" {
				continue
			}
			targetID, targetName := resolve("", port.NeighborName)
			link := NetworkLink{
				ID:            networkLinkID(NetworkLink{SourceAssetID: asset.ID, SourceInterface: port.Name, TargetAssetID: targetID, TargetAssetName: targetName, TargetPort: port.NeighborPort}),
				SourceAssetID: asset.ID, SourceAssetName: asset.Name, SourceInterface: port.Name,
				TargetAssetID: targetID, TargetAssetName: targetName, TargetPort: port.NeighborPort,
				Status: "active", Source: "lldp", LastSeenAt: port.LastSeenAt,
			}
			if !seen[link.ID] {
				seen[link.ID] = true
				links = append(links, link)
			}
		}
	}
	sort.Slice(links, func(i, j int) bool {
		if links[i].SourceAssetName == links[j].SourceAssetName {
			return links[i].SourceInterface < links[j].SourceInterface
		}
		return strings.ToLower(links[i].SourceAssetName) < strings.ToLower(links[j].SourceAssetName)
	})
	return enrichNetworkLinks(links, assets)
}

func (s *Service) CreateNetworkLink(input CreateNetworkLinkInput, operator string) (NetworkLink, error) {
	input.SourceAssetID = strings.TrimSpace(input.SourceAssetID)
	input.SourceInterface = strings.TrimSpace(input.SourceInterface)
	input.TargetAssetID = strings.TrimSpace(input.TargetAssetID)
	input.TargetPort = strings.TrimSpace(input.TargetPort)
	if input.SourceAssetID == "" || input.SourceInterface == "" || input.TargetAssetID == "" || input.TargetPort == "" {
		return NetworkLink{}, fmt.Errorf("%w: source and target are required", ErrValidation)
	}
	source, err := s.GetAsset(input.SourceAssetID)
	if err != nil {
		return NetworkLink{}, err
	}
	target, err := s.GetAsset(input.TargetAssetID)
	if err != nil {
		return NetworkLink{}, err
	}
	if target.Type != "network-device" && target.Type != "load-balancer" {
		return NetworkLink{}, fmt.Errorf("%w: target must be a network device", ErrValidation)
	}
	if !isHostAssetType(source.Type) {
		return NetworkLink{}, fmt.Errorf("%w: source must be a server", ErrValidation)
	}
	if interfaces, err := s.NetworkInterfaces(source.ID); err == nil && len(interfaces) > 0 && !hasNetworkInterface(interfaces, input.SourceInterface) {
		return NetworkLink{}, fmt.Errorf("%w: source interface not found", ErrValidation)
	}
	if ports, err := s.NetworkPorts(target.ID); err == nil && len(ports) > 0 && !hasNetworkPort(ports, input.TargetPort) {
		return NetworkLink{}, fmt.Errorf("%w: target port not found", ErrValidation)
	}
	for _, link := range s.NetworkLinks() {
		if link.Source == "lldp" {
			continue
		}
		if link.SourceAssetID == source.ID && strings.EqualFold(link.SourceInterface, input.SourceInterface) {
			return NetworkLink{}, fmt.Errorf("%w: source interface is already linked", ErrConflict)
		}
		if link.TargetAssetID == target.ID && strings.EqualFold(link.TargetPort, input.TargetPort) {
			return NetworkLink{}, fmt.Errorf("%w: target port is already linked", ErrConflict)
		}
	}
	link := NetworkLink{
		SourceAssetID: source.ID, SourceAssetName: source.Name, SourceInterface: input.SourceInterface,
		TargetAssetID: target.ID, TargetAssetName: target.Name, TargetPort: input.TargetPort,
		Status: "active", Source: "manual", LastSeenAt: time.Now().Format("2006-01-02 15:04:05"),
	}
	link.ID = networkLinkID(link)
	relations := append([]Relation(nil), source.Relations...)
	relations = append(relations, Relation{
		ID: link.ID, Type: "connects-to", TargetID: target.ID, TargetName: target.Name,
		SourcePort: input.SourceInterface, TargetPort: input.TargetPort,
		Status: "active", Source: "manual", LastSeenAt: link.LastSeenAt,
	})
	if _, err := s.UpdateAsset(source.ID, assetUpdateInput(source, relations), operator); err != nil {
		return NetworkLink{}, err
	}
	return link, nil
}

func (s *Service) DeleteNetworkLink(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return ErrNotFound
	}
	for _, source := range s.InventoryAssets() {
		relations := make([]Relation, 0, len(source.Relations))
		found := false
		for _, relation := range source.Relations {
			if relation.Type != "connects-to" {
				relations = append(relations, relation)
				continue
			}
			relationID := relation.ID
			if relationID == "" {
				targetID, targetName := relation.TargetID, relation.TargetName
				relationID = networkLinkID(NetworkLink{SourceAssetID: source.ID, SourceInterface: relation.SourcePort, TargetAssetID: targetID, TargetAssetName: targetName, TargetPort: relation.TargetPort})
			}
			if relationID == id {
				found = true
				continue
			}
			relations = append(relations, relation)
		}
		if found {
			_, err := s.UpdateAsset(source.ID, assetUpdateInput(source, relations), "network-link-delete")
			return err
		}
	}
	return ErrNotFound
}

func assetUpdateInput(asset Asset, relations []Relation) UpdateAssetInput {
	return UpdateAssetInput{
		Name: asset.Name, Type: asset.Type, Status: asset.Status, IP: asset.IP, Environment: asset.Environment,
		ProjectGroup: asset.ProjectGroup, Owner: asset.Owner, Location: asset.Location,
		Tags: append([]string(nil), asset.Tags...), Attributes: append([]Attribute(nil), asset.Attributes...),
		Relations: relations,
	}
}

func parseNetworkPorts(asset Asset) []NetworkPort {
	raw := networkAttributeValue(asset.Attributes, "network_ports")
	var ports []NetworkPort
	if raw != "" && json.Unmarshal([]byte(raw), &ports) == nil && len(ports) > 0 {
		for index := range ports {
			if ports[index].AssetID == "" {
				ports[index].AssetID = asset.ID
			}
			if ports[index].AssetName == "" {
				ports[index].AssetName = asset.Name
			}
			if ports[index].ID == "" {
				ports[index].ID = portID(asset.ID, ports[index])
			}
		}
		sort.Slice(ports, func(i, j int) bool { return ports[i].IfIndex < ports[j].IfIndex })
		return ports
	}
	var neighbors []struct {
		LocalPort      string `json:"localPort"`
		RemoteSysName  string `json:"remoteSysName"`
		RemoteChassis  string `json:"remoteChassisId"`
		RemotePortID   string `json:"remotePortId"`
		RemotePortDesc string `json:"remotePortDesc"`
	}
	raw = networkAttributeValue(asset.Attributes, "lldp_neighbors")
	if raw != "" && json.Unmarshal([]byte(raw), &neighbors) == nil {
		for index, neighbor := range neighbors {
			name := strings.TrimSpace(neighbor.LocalPort)
			if name == "" {
				name = fmt.Sprintf("port-%d", index+1)
			}
			port := NetworkPort{AssetID: asset.ID, AssetName: asset.Name, IfIndex: neighbor.LocalPort, Name: name, AdminStatus: "unknown", OperStatus: "up", NeighborName: neighbor.RemoteSysName, NeighborPort: neighbor.RemotePortDesc, NeighborChassis: neighbor.RemoteChassis}
			port.ID = portID(asset.ID, port)
			ports = append(ports, port)
		}
	}
	return ports
}

func parseNetworkInterfaces(asset Asset) []NetworkInterface {
	raw := networkAttributeValue(asset.Attributes, "network_interfaces")
	if raw == "" {
		return []NetworkInterface{}
	}
	var interfaces []NetworkInterface
	if json.Unmarshal([]byte(raw), &interfaces) != nil {
		return []NetworkInterface{}
	}
	for index := range interfaces {
		if interfaces[index].AssetID == "" {
			interfaces[index].AssetID = asset.ID
		}
		if interfaces[index].AssetName == "" {
			interfaces[index].AssetName = asset.Name
		}
		if interfaces[index].ID == "" {
			interfaces[index].ID = fmt.Sprintf("%s:if:%s", asset.ID, interfaces[index].Name)
		}
		if interfaces[index].AdminStatus == "" {
			interfaces[index].AdminStatus = "unknown"
		}
		if interfaces[index].OperStatus == "" {
			interfaces[index].OperStatus = "unknown"
		}
	}
	sort.Slice(interfaces, func(i, j int) bool { return interfaces[i].Name < interfaces[j].Name })
	return interfaces
}

func addPortNeighborAsset(port *NetworkPort, byName map[string]Asset) {
	if port == nil || port.NeighborName == "" {
		return
	}
	if asset, ok := byName[normalizeNetworkName(port.NeighborName)]; ok {
		port.NeighborName = asset.Name
	}
}

func networkAttributeValue(attributes []Attribute, name string) string {
	for _, attribute := range attributes {
		if attribute.Name == name {
			return attribute.Value
		}
	}
	return ""
}

func addPortARPAssets(port *NetworkPort, byIP map[string]Asset) {
	if port == nil {
		return
	}
	for index := range port.ARPEntries {
		if asset, ok := byIP[port.ARPEntries[index].IP]; ok {
			port.ARPEntries[index].AssetID = asset.ID
			port.ARPEntries[index].AssetName = asset.Name
		}
	}
}

func addInterfaceARPAssets(item *NetworkInterface, byIP map[string]Asset) {
	if item == nil {
		return
	}
	for index := range item.Neighbors {
		if asset, ok := byIP[item.Neighbors[index].IP]; ok {
			item.Neighbors[index].AssetID = asset.ID
			item.Neighbors[index].AssetName = asset.Name
		}
	}
}
func normalizeNetworkName(value string) string { return strings.ToLower(strings.TrimSpace(value)) }

func portID(assetID string, port NetworkPort) string {
	return assetID + ":port:" + firstNonEmpty(port.Name, port.IfIndex)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func networkLinkID(link NetworkLink) string {
	key := strings.Join([]string{link.SourceAssetID, link.SourceInterface, link.TargetAssetID, link.TargetAssetName, link.TargetPort}, "|")
	sum := sha1.Sum([]byte(key))
	return "netlink-" + hex.EncodeToString(sum[:8])
}

func hasNetworkInterface(interfaces []NetworkInterface, name string) bool {
	for _, item := range interfaces {
		if strings.EqualFold(item.Name, name) {
			return true
		}
	}
	return false
}

func hasNetworkPort(ports []NetworkPort, name string) bool {
	for _, item := range ports {
		if strings.EqualFold(item.Name, name) || strings.EqualFold(item.IfIndex, name) {
			return true
		}
	}
	return false
}

func enrichNetworkLinks(links []NetworkLink, assets []Asset) []NetworkLink {
	byID := make(map[string]Asset, len(assets))
	for _, asset := range assets {
		byID[asset.ID] = asset
	}
	manualIndex := make(map[string]int)
	seenLLDP := make(map[string]bool)
	result := make([]NetworkLink, 0, len(links))
	for _, link := range links {
		if link.Source == "lldp" {
			continue
		}
		key := physicalLinkKey(link)
		if _, exists := manualIndex[key]; exists {
			continue
		}
		manualIndex[key] = len(result)
		result = append(result, link)
	}
	for _, link := range links {
		if link.Source != "lldp" {
			continue
		}
		key := physicalLinkKey(link)
		if index, exists := manualIndex[key]; exists {
			result[index].Evidence = append(result[index].Evidence, fmt.Sprintf("LLDP 邻居 %s / %s", link.SourceInterface, firstNonEmpty(link.TargetPort, link.TargetAssetName)))
			continue
		}
		if seenLLDP[key] {
			continue
		}
		seenLLDP[key] = true
		result = append(result, link)
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	for index := range result {
		if result[index].Source == "lldp" {
			enrichLLDPLink(&result[index], byID)
		} else {
			enrichManualLink(&result[index], byID)
		}
		if result[index].LastVerifiedAt == "" {
			result[index].LastVerifiedAt = now
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].SourceAssetName == result[j].SourceAssetName {
			return result[i].SourceInterface < result[j].SourceInterface
		}
		return strings.ToLower(result[i].SourceAssetName) < strings.ToLower(result[j].SourceAssetName)
	})
	return result
}

func enrichManualLink(link *NetworkLink, byID map[string]Asset) {
	link.Verification = "unverified"
	link.Confidence = 55
	sourceAsset, sourceOK := byID[link.SourceAssetID]
	targetAsset, targetOK := byID[link.TargetAssetID]
	if !sourceOK || !targetOK {
		link.Evidence = append(link.Evidence, "链路端点资产不完整")
		return
	}
	interfaceMAC := ""
	interfaceIPv4 := ""
	for _, item := range parseNetworkInterfaces(sourceAsset) {
		if strings.EqualFold(item.Name, link.SourceInterface) {
			interfaceMAC = normalizeMACForCompare(item.MACAddress)
			interfaceIPv4 = item.IPv4
			break
		}
	}
	portMACs := []string{}
	arpIPs := []string{}
	portFound := false
	for _, port := range parseNetworkPorts(targetAsset) {
		if strings.EqualFold(port.Name, link.TargetPort) || strings.EqualFold(port.IfIndex, link.TargetPort) {
			portFound = true
			if port.MACAddress != "" {
				portMACs = append(portMACs, normalizeMACForCompare(port.MACAddress))
			}
			for _, mac := range port.MACAddresses {
				portMACs = append(portMACs, normalizeMACForCompare(mac))
			}
			for _, entry := range port.ARPEntries {
				if entry.MACAddress != "" {
					portMACs = append(portMACs, normalizeMACForCompare(entry.MACAddress))
				}
				if entry.IP != "" {
					arpIPs = append(arpIPs, entry.IP)
				}
			}
			break
		}
	}
	if interfaceMAC == "" {
		link.Evidence = append(link.Evidence, "服务器网卡 MAC 尚未采集")
	} else {
		link.Evidence = append(link.Evidence, "服务器网卡 MAC "+interfaceMAC)
	}
	if !portFound {
		link.Evidence = append(link.Evidence, "交换机端口清单尚未采集")
		return
	}
	if interfaceMAC != "" && containsNormalizedMAC(portMACs, interfaceMAC) {
		link.Verification = "confirmed"
		link.Confidence = 100
		link.Evidence = append(link.Evidence, "交换机 FDB/ARP 表匹配服务器网卡 MAC，链路已确认")
		return
	}
	if interfaceIPv4 != "" && containsString(arpIPs, interfaceIPv4) {
		link.Verification = "confirmed"
		link.Confidence = 90
		link.Evidence = append(link.Evidence, "交换机 ARP 表匹配服务器 IPv4，链路已确认")
		return
	}
	unique := uniqueNormalizedMACs(portMACs)
	if len(unique) > 0 {
		link.Verification = "conflict"
		link.Confidence = 35
		link.Evidence = append(link.Evidence, fmt.Sprintf("交换机端口学习到 %d 个其他 MAC，需人工复核", len(unique)))
		return
	}
	link.Evidence = append(link.Evidence, "交换机端口暂未学习到 MAC/ARP，保留待验证")
}
func enrichLLDPLink(link *NetworkLink, byID map[string]Asset) {
	link.Verification = "candidate"
	link.Confidence = 70
	link.Evidence = []string{"LLDP 邻居声明"}
	if _, ok := byID[link.TargetAssetID]; ok {
		link.Verification = "lldp-confirmed"
		link.Confidence = 90
	}
	detail := strings.TrimSpace(link.TargetAssetName + " / " + link.TargetPort)
	if detail != "" && detail != "/" {
		link.Evidence = append(link.Evidence, "对端 "+detail)
	}
}

func physicalLinkKey(link NetworkLink) string {
	left := link.SourceAssetID + "|" + strings.ToLower(strings.TrimSpace(link.SourceInterface))
	right := link.TargetAssetID + "|" + strings.ToLower(strings.TrimSpace(link.TargetPort))
	if right < left {
		left, right = right, left
	}
	return left + "::" + right
}

func normalizeMACForCompare(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	replacer := strings.NewReplacer(":", "", "-", "", ".", "", " ", "")
	return replacer.Replace(value)
}

func containsNormalizedMAC(values []string, target string) bool {
	for _, value := range values {
		if normalizeMACForCompare(value) == target {
			return true
		}
	}
	return false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == strings.TrimSpace(target) {
			return true
		}
	}
	return false
}
func uniqueNormalizedMACs(values []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = normalizeMACForCompare(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

type NetworkLinkHistoryEntry struct {
	ID         string   `json:"id"`
	AssetID    string   `json:"assetId"`
	AssetName  string   `json:"assetName"`
	Action     string   `json:"action"`
	Operator   string   `json:"operator"`
	OccurredAt string   `json:"occurredAt"`
	Changes    []Change `json:"changes"`
}

func (s *Service) NetworkLinkHistory(limit int) []NetworkLinkHistoryEntry {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	s.mu.RLock()
	names := make(map[string]string, len(s.assets))
	for _, asset := range s.assets {
		names[asset.ID] = asset.Name
	}
	entries := make([]NetworkLinkHistoryEntry, 0)
	for assetID, history := range s.history {
		for _, entry := range history {
			linkChanges := make([]Change, 0)
			for _, change := range entry.Changes {
				if change.Field == "物理链路" {
					linkChanges = append(linkChanges, change)
				}
			}
			if len(linkChanges) == 0 {
				continue
			}
			entries = append(entries, NetworkLinkHistoryEntry{ID: entry.ID, AssetID: assetID, AssetName: names[assetID], Action: entry.Action, Operator: entry.Operator, OccurredAt: entry.OccurredAt, Changes: linkChanges})
		}
	}
	s.mu.RUnlock()
	sort.Slice(entries, func(i, j int) bool { return entries[i].OccurredAt > entries[j].OccurredAt })
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries
}
