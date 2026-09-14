package discovery

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"cmdb/gateway-bff/internal/cmdb"
)

type ExporterInstallInput struct {
	Hosts        []string `json:"hosts"`
	Port         int      `json:"port"`
	ExporterPort int      `json:"exporterPort"`
	GatewayURL   string   `json:"gatewayUrl"`
	CredentialID string   `json:"credentialId"`
}
type ExporterInstallResult struct {
	Host string `json:"host"`
	OK   bool   `json:"ok"`
	Info string `json:"info"`
	Port int    `json:"port"`
}

func NodeExporterVersion() string {
	if value := strings.TrimSpace(os.Getenv("CMDB_NODE_EXPORTER_VERSION")); value != "" {
		return value
	}
	return "1.11.1"
}

func (s *Service) ExporterBatchInstall(in ExporterInstallInput) ([]ExporterInstallResult, error) {
	if len(in.Hosts) == 0 {
		return nil, errors.New("hosts required")
	}
	sshPort := in.Port
	if sshPort == 0 {
		sshPort = 22
	}
	exporterPort := in.ExporterPort
	if exporterPort == 0 {
		exporterPort = 9100
	}
	if sshPort < 1 || sshPort > 65535 || exporterPort < 1 || exporterPort > 65535 {
		return nil, errors.New("invalid port")
	}
	username, password := os.Getenv("CMDB_SSH_USERNAME"), os.Getenv("CMDB_SSH_PASSWORD")
	if in.CredentialID != "" {
		u, secret, err := s.resolveCredential(in.CredentialID)
		if err != nil {
			return nil, fmt.Errorf("resolve SSH credential: %w", err)
		}
		if u == "" || secret == "" {
			return nil, errors.New("SSH credential is empty")
		}
		username, password = u, secret
	}
	if username == "" || password == "" {
		return nil, errors.New("SSH credentials are not configured")
	}
	gatewayURL := strings.TrimRight(in.GatewayURL, "/")
	if gatewayURL == "" {
		gatewayURL = strings.TrimRight(os.Getenv("CMDB_AGENT_GATEWAY_URL"), "/")
	}
	if gatewayURL == "" {
		gatewayURL = "http://172.28.69.161:30080"
	}
	token := strings.TrimSpace(os.Getenv("AGENT_SHARED_TOKEN"))
	if token == "" {
		return nil, errors.New("AGENT_SHARED_TOKEN is not configured")
	}
	results := []ExporterInstallResult{}
	for _, host := range in.Hosts {
		script := fmt.Sprintf("set -e; mkdir -p /opt/node-exporter; curl -fsSL --connect-timeout 5 -H 'Authorization: Bearer %s' '%s/api/v1/agent/install/node-exporter/linux-amd64' -o /opt/node-exporter/node_exporter.new; chmod 755 /opt/node-exporter/node_exporter.new; /opt/node-exporter/node_exporter.new --version >/dev/null; mv -f /opt/node-exporter/node_exporter.new /opt/node-exporter/node_exporter; cat > /etc/systemd/system/node_exporter.service <<'U'\n[Unit]\nDescription=Prometheus Node Exporter\nAfter=network-online.target\n[Service]\nExecStart=/opt/node-exporter/node_exporter --web.listen-address=:%d\nRestart=always\nRestartSec=3\n[Install]\nWantedBy=multi-user.target\nU\nsystemctl daemon-reload; systemctl enable node_exporter >/dev/null 2>&1 || true; systemctl restart node_exporter; sleep 2; echo ACTIVE=$(systemctl is-active node_exporter) PORT=%d", token, gatewayURL, exporterPort, exporterPort)
		out, err := runSSHRaw(host, sshPort, username, password, script, 45*time.Second)
		result := ExporterInstallResult{Host: host, Port: exporterPort}
		if err != nil {
			result.Info = err.Error()
		} else {
			result.OK = true
			result.Info = out
			s.updateExporterAsset(host, exporterPort, "active")
		}
		results = append(results, result)
	}
	return results, nil
}

func (s *Service) ExporterBatchUninstall(hosts []string, port int, credentialID string) ([]ExporterInstallResult, error) {
	if len(hosts) == 0 {
		return nil, errors.New("hosts required")
	}
	if port == 0 {
		port = 22
	}
	if port < 1 || port > 65535 {
		return nil, errors.New("invalid port")
	}
	username, password := os.Getenv("CMDB_SSH_USERNAME"), os.Getenv("CMDB_SSH_PASSWORD")
	if credentialID != "" {
		u, secret, err := s.resolveCredential(credentialID)
		if err != nil {
			return nil, fmt.Errorf("resolve SSH credential: %w", err)
		}
		if u == "" || secret == "" {
			return nil, errors.New("SSH credential is empty")
		}
		username, password = u, secret
	}
	if username == "" || password == "" {
		return nil, errors.New("SSH credentials are not configured")
	}
	script := "systemctl stop node_exporter 2>/dev/null; systemctl disable node_exporter 2>/dev/null; rm -f /etc/systemd/system/node_exporter.service /opt/node-exporter/node_exporter; rmdir /opt/node-exporter 2>/dev/null || true; systemctl daemon-reload; echo UNINSTALLED"
	results := []ExporterInstallResult{}
	for _, host := range hosts {
		out, err := runSSHRaw(host, port, username, password, script, 25*time.Second)
		result := ExporterInstallResult{Host: host}
		if err != nil {
			result.Info = err.Error()
		} else {
			result.OK = true
			result.Info = out
			s.updateExporterAsset(host, 0, "removed")
		}
		results = append(results, result)
	}
	return results, nil
}

func (s *Service) updateExporterAsset(host string, port int, status string) {
	if s.cmdb == nil {
		return
	}
	id := s.cmdb.FindHostByIP(host, "")
	if id == "" {
		return
	}
	asset, err := s.cmdb.GetAsset(id)
	if err != nil {
		return
	}
	attributes := append([]cmdb.Attribute(nil), asset.Attributes...)
	setAttribute := func(name, label, value string) {
		for i := range attributes {
			if attributes[i].Name == name {
				attributes[i].Value = value
				return
			}
		}
		attributes = append(attributes, cmdb.Attribute{Name: name, Label: label, Value: value})
	}
	setAttribute("node_exporter_status", "Exporter 状态", status)
	if port > 0 {
		setAttribute("node_exporter_port", "Exporter 端口", strconv.Itoa(port))
	}
	setAttribute("node_exporter_version", "Exporter 版本", NodeExporterVersion())
	tags := append([]string(nil), asset.Tags...)
	if status == "active" && !containsString(tags, "node-exporter") {
		tags = append(tags, "node-exporter")
	}
	_, _ = s.cmdb.UpdateAsset(id, cmdb.UpdateAssetInput{Name: asset.Name, Status: asset.Status, IP: asset.IP, Environment: asset.Environment, ProjectGroup: asset.ProjectGroup, Owner: asset.Owner, Location: asset.Location, Tags: tags, Attributes: attributes, Relations: asset.Relations}, "exporter-manager")
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
