package networkautomation

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/tickets"
)

func (s *Service) OnboardingRequests() []OnboardingRequest {
	if s.db != nil {
		if items, err := loadOnboardingRequests(context.Background(), s.db); err == nil {
			s.mu.Lock()
			s.onboarding = items
			s.mu.Unlock()
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]OnboardingRequest(nil), s.onboarding...)
}

func (s *Service) CreateOnboardingRequest(input OnboardingInput, operator string) (OnboardingRequest, error) {
	var anomaly cmdb.NetworkAnomaly
	found := false
	for _, item := range s.cmdb.NetworkAnomalies() {
		if item.ID == input.AnomalyID && item.Type == "unmanaged-access" {
			anomaly = item
			found = true
			break
		}
	}
	if !found {
		return OnboardingRequest{}, ErrNotFound
	}
	for _, item := range s.OnboardingRequests() {
		if item.AnomalyID == input.AnomalyID && item.Status != "rejected" {
			return item, nil
		}
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	request := OnboardingRequest{ID: fmt.Sprintf("onboard-%d", time.Now().UnixNano()), AnomalyID: anomaly.ID, Name: firstValue(input.Name, anomaly.MACAddress, anomaly.IP), IP: anomaly.IP, MACAddress: anomaly.MACAddress, VLAN: anomaly.VLAN, AssetType: firstValue(input.AssetType, "physical-server"), Environment: firstValue(input.Environment, anomaly.Environment), ProjectGroup: firstValue(input.ProjectGroup, anomaly.ProjectGroup), Owner: strings.TrimSpace(input.Owner), Location: firstValue(input.Location, anomaly.Location), Status: "pending", Note: strings.TrimSpace(input.Note), CreatedBy: operator, CreatedAt: now, UpdatedAt: now}
	if request.Name == "" || request.Owner == "" {
		return OnboardingRequest{}, ErrValidation
	}
	if s.db != nil {
		if err := upsertOnboardingRequest(context.Background(), s.db, request); err != nil {
			return OnboardingRequest{}, err
		}
	}
	s.mu.Lock()
	s.onboarding = append([]OnboardingRequest{request}, s.onboarding...)
	s.mu.Unlock()
	return request, nil
}

func (s *Service) ApproveOnboardingRequest(id, operator string) (OnboardingRequest, error) {
	request, err := s.onboardingByID(id)
	if err != nil {
		return OnboardingRequest{}, err
	}
	if request.Status == "onboarded" {
		return request, nil
	}
	assetID := onboardingAssetID(request)
	attrs := []cmdb.Attribute{{Name: "onboarding_source", Label: "入网来源", Value: "network-compliance"}}
	if request.MACAddress != "" || request.IP != "" {
		interfaces, _ := json.Marshal([]cmdb.NetworkInterface{{Name: "onboarded", MACAddress: request.MACAddress, IPv4: request.IP, AdminStatus: "up", OperStatus: "up", Type: "ethernet"}})
		attrs = append(attrs, cmdb.Attribute{Name: "network_interfaces", Label: "网卡清单", Value: string(interfaces)})
	}
	asset, err := s.cmdb.UpsertExternalAsset(cmdb.CreateAssetInput{ID: assetID, Name: request.Name, Type: request.AssetType, Status: "online", IP: request.IP, Environment: request.Environment, ProjectGroup: request.ProjectGroup, Owner: request.Owner, Location: request.Location, Source: "network-onboarding", Tags: []string{"新设备入网"}, Attributes: attrs}, operator)
	if err != nil {
		return OnboardingRequest{}, err
	}
	rule, err := s.CreateComplianceRule(ComplianceRuleInput{Name: "入网白名单-" + request.Name, Action: "allow", Type: "mac", Value: request.MACAddress, Environment: request.Environment, ProjectGroup: request.ProjectGroup, Location: request.Location, VLAN: request.VLAN, Description: "新设备入网审批自动生成"}, operator)
	if err != nil {
		return OnboardingRequest{}, err
	}
	rule, err = s.ApproveComplianceRule(rule.ID, operator)
	if err != nil {
		return OnboardingRequest{}, err
	}
	ticket, ticketErr := s.tickets.Create(tickets.CreateInput{Title: "新设备入网：" + request.Name, Type: "change", Priority: "medium", Applicant: operator, Description: request.Note, RelatedAsset: asset.ID, ProjectGroup: request.ProjectGroup})
	if ticketErr == nil {
		if approved, approveErr := s.tickets.Approve(ticket.ID, operator, "入网审批已通过"); approveErr == nil {
			if closed, closeErr := s.tickets.Close(approved.ID, operator, "新设备已纳管并写入白名单"); closeErr == nil {
				request.TicketID = closed.ID
			}
		}
	}
	request.Status = "onboarded"
	request.AssetID = asset.ID
	request.RuleID = rule.ID
	request.ApprovedBy = operator
	request.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
	if s.db != nil {
		if err := upsertOnboardingRequest(context.Background(), s.db, request); err != nil {
			return OnboardingRequest{}, err
		}
	}
	s.mu.Lock()
	for i := range s.onboarding {
		if s.onboarding[i].ID == id {
			s.onboarding[i] = request
		}
	}
	s.mu.Unlock()
	return request, nil
}

func (s *Service) RejectOnboardingRequest(id, operator, note string) (OnboardingRequest, error) {
	request, err := s.onboardingByID(id)
	if err != nil {
		return OnboardingRequest{}, err
	}
	request.Status = "rejected"
	request.Note = strings.TrimSpace(note)
	request.ApprovedBy = operator
	request.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
	if s.db != nil {
		if err := upsertOnboardingRequest(context.Background(), s.db, request); err != nil {
			return OnboardingRequest{}, err
		}
	}
	s.mu.Lock()
	for i := range s.onboarding {
		if s.onboarding[i].ID == id {
			s.onboarding[i] = request
		}
	}
	s.mu.Unlock()
	return request, nil
}

func (s *Service) onboardingByID(id string) (OnboardingRequest, error) {
	for _, item := range s.OnboardingRequests() {
		if item.ID == id {
			return item, nil
		}
	}
	return OnboardingRequest{}, ErrNotFound
}

func onboardingAssetID(request OnboardingRequest) string {
	sum := sha1.Sum([]byte(firstValue(request.MACAddress, request.IP, request.Name)))
	return "onboarded-" + hex.EncodeToString(sum[:6])
}
