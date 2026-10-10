package aiops

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"cmdb/gateway-bff/internal/monitor"
	"cmdb/gateway-bff/internal/toolbox"
)

var allowedEvidenceTools = map[string]bool{
	"dns": true, "tcp": true, "http": true, "tls": true, "prometheus": true, "k8s-pod": true,
}

func (s *Service) CollectEvidence(ctx context.Context, analysisID string, requested []string) (Analysis, error) {
	if s.tools == nil {
		return Analysis{}, ErrNotReady
	}
	item, err := s.Get(analysisID)
	if err != nil {
		return Analysis{}, err
	}
	alert, hasAlert := s.findAlert(item.AlertID)
	requests := s.evidenceRequests(item, alert, hasAlert, requested)
	if len(requests) == 0 {
		return Analysis{}, ErrValidation
	}
	added := 0
	for _, input := range requests {
		result, runErr := s.tools.Run(ctx, input)
		if runErr != nil {
			continue
		}
		source := "toolbox/" + result.ToolID
		if hasEvidenceSource(item.Evidence, source) {
			continue
		}
		detail := strings.TrimSpace(result.Summary)
		if result.Error != "" {
			detail += "；错误：" + result.Error
		}
		if detail == "" {
			detail = "工具执行完成"
		}
		addEvidence(&item, "tool", source, result.ToolName, detail, 8)
		addSource(&item, source)
		item.Timeline = append(item.Timeline, TimelineEvent{Time: result.FinishedAt, Type: "tool", Source: source, Message: result.Summary})
		added++
	}
	if added == 0 {
		return Analysis{}, ErrNoEvidence
	}
	item.Confidence = confidenceFromEvidence(item.Evidence)
	item.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
	item.Summary = fmt.Sprintf("%s 已补充 %d 项只读工具证据。", strings.TrimSpace(item.Summary), added)
	if err = s.repo.SaveAnalysis(ctx, item); err != nil {
		return Analysis{}, err
	}
	s.mu.Lock()
	for i := range s.analyses {
		if s.analyses[i].ID == item.ID {
			s.analyses[i] = item
			break
		}
	}
	s.mu.Unlock()
	return item, nil
}

func (s *Service) evidenceRequests(item Analysis, alert monitor.Alert, hasAlert bool, requested []string) []toolbox.RunInput {
	inputs := []toolbox.RunInput{}
	assetID, assetName, assetIP := "", "", ""
	if len(item.RelatedAssets) > 0 {
		assetID = item.RelatedAssets[0].ID
		assetName = item.RelatedAssets[0].Name
		assetIP = item.RelatedAssets[0].IP
	}
	title := strings.ToLower(item.AlertTitle)
	targetName := assetName
	targetIP := assetIP
	if targetName == "" && hasAlert {
		targetName = alert.Target
	}
	if targetIP == "" && hasAlert && net.ParseIP(alert.Target) != nil {
		targetIP = alert.Target
	}
	selected := requested
	if len(selected) == 0 {
		selected = []string{"dns", "prometheus"}
		if assetID == "" {
			selected = []string{"dns"}
		}
		if strings.Contains(title, "pod") || strings.Contains(title, "kubernetes") {
			selected = append(selected, "k8s-pod")
		}
	}
	for _, toolID := range selected {
		toolID = strings.TrimSpace(toolID)
		if !allowedEvidenceTools[toolID] {
			continue
		}
		switch toolID {
		case "dns":
			if targetName != "" {
				inputs = append(inputs, toolbox.RunInput{ToolID: "dns", Target: targetName, Operator: "aiops:" + item.ID})
			}
		case "tcp":
			if targetIP != "" {
				inputs = append(inputs, toolbox.RunInput{ToolID: "tcp", Target: net.JoinHostPort(targetIP, "22"), Operator: "aiops:" + item.ID})
			}
		case "http":
			if targetName != "" {
				target := targetName
				if !strings.Contains(target, "://") {
					target = "http://" + target
				}
				inputs = append(inputs, toolbox.RunInput{ToolID: "http", Target: target, Operator: "aiops:" + item.ID})
			}
		case "tls":
			if targetName != "" {
				inputs = append(inputs, toolbox.RunInput{ToolID: "tls", Target: net.JoinHostPort(targetName, "443"), Operator: "aiops:" + item.ID})
			}
		case "prometheus":
			if assetID != "" {
				inputs = append(inputs, toolbox.RunInput{ToolID: "prometheus", Query: `up{cmdb_asset_id="` + assetID + `"}`, Operator: "aiops:" + item.ID})
			}
		case "k8s-pod":
			if namespace, name, ok := splitNamespacePod(item.Target); ok {
				inputs = append(inputs, toolbox.RunInput{ToolID: "k8s-pod", Namespace: namespace, Name: name, Operator: "aiops:" + item.ID})
			}
		}
	}
	return inputs
}

func splitNamespacePod(value string) (string, string, bool) {
	parts := strings.Split(strings.TrimSpace(value), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func hasEvidenceSource(items []Evidence, source string) bool {
	for _, item := range items {
		if item.Source == source {
			return true
		}
	}
	return false
}
