package toolbox

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

func outputLine(level, text string) Line {
	return Line{Time: time.Now().Format("15:04:05"), Level: level, Text: text}
}

func safeHTTPClient(timeout time.Duration) *http.Client {
	transport := &http.Transport{Proxy: nil, DialContext: safeDialContext, ForceAttemptHTTP2: true, TLSHandshakeTimeout: timeout, ResponseHeaderTimeout: timeout, ExpectContinueTimeout: time.Second}
	return &http.Client{Transport: transport, Timeout: timeout}
}

func safeDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if err := validateHost(host); err != nil {
		return nil, err
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	var lastErr error
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	for _, item := range addresses {
		if isBlockedIP(item.IP) {
			lastErr = fmt.Errorf("目标地址 %s 不允许访问", item.IP)
			continue
		}
		conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(item.IP.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("目标地址无法解析")
	}
	return nil, lastErr
}

func validateResolvedHost(ctx context.Context, host string) error {
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return err
	}
	for _, item := range addresses {
		if isBlockedIP(item.IP) {
			return fmt.Errorf("目标地址 %s 不允许访问", item.IP)
		}
	}
	return nil
}
func isBlockedIP(ip net.IP) bool {
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	if ip.IsLoopback() && strings.EqualFold(strings.TrimSpace(os.Getenv("TOOLBOX_ALLOW_LOOPBACK")), "true") {
		return false
	}
	return ip.IsLoopback()
}

func validateHost(host string) error {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" || strings.ContainsAny(host, " /\\") {
		return fmt.Errorf("目标主机不合法")
	}
	if ip := net.ParseIP(host); ip != nil && isBlockedIP(ip) {
		return fmt.Errorf("目标地址 %s 不允许访问", ip)
	}
	return nil
}

func normalizeHostPort(raw string, defaultPort string) (host string, port string, address string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", "", fmt.Errorf("目标地址不能为空")
	}
	if strings.Contains(raw, "://") {
		parsed, parseErr := url.Parse(raw)
		if parseErr != nil || parsed.Host == "" {
			return "", "", "", fmt.Errorf("目标地址格式不合法")
		}
		raw = parsed.Host
	}
	if strings.Contains(raw, ":") {
		host, port, err = net.SplitHostPort(raw)
		if err != nil {
			return "", "", "", fmt.Errorf("目标地址应使用 host:port 格式")
		}
	} else {
		host, port = raw, defaultPort
	}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if err = validateHost(host); err != nil {
		return "", "", "", err
	}
	portNumber, parseErr := strconv.Atoi(port)
	if parseErr != nil || portNumber < 1 || portNumber > 65535 {
		return "", "", "", fmt.Errorf("端口必须是 1-65535")
	}
	return host, port, net.JoinHostPort(host, port), nil
}

func (s *Service) runDNS(ctx context.Context, in RunInput) ([]Line, string, string, error) {
	host := strings.TrimSpace(in.Target)
	if strings.Contains(host, "://") {
		if parsed, err := url.Parse(host); err == nil && parsed.Hostname() != "" {
			host = parsed.Hostname()
		}
	}
	if err := validateHost(host); err != nil {
		return nil, "failed", "DNS 目标校验失败", err
	}
	start := time.Now()
	ips, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		return []Line{outputLine("error", err.Error())}, "failed", "DNS 解析失败", nil
	}
	lines := []Line{outputLine("info", fmt.Sprintf("域名：%s", host))}
	for _, ip := range ips {
		lines = append(lines, outputLine("success", fmt.Sprintf("解析地址：%s", ip)))
	}
	return lines, "success", fmt.Sprintf("解析完成，共 %d 个地址，耗时 %s", len(ips), time.Since(start).Round(time.Millisecond)), nil
}

func (s *Service) runTCP(ctx context.Context, in RunInput) ([]Line, string, string, error) {
	_, _, address, err := normalizeHostPort(in.Target, "80")
	if err != nil {
		return nil, "failed", "TCP 目标校验失败", err
	}
	start := time.Now()
	conn, err := safeDialContext(ctx, "tcp", address)
	if err != nil {
		return []Line{outputLine("error", err.Error())}, "failed", "TCP 连接失败", nil
	}
	_ = conn.Close()
	return []Line{outputLine("info", "TCP 三次握手完成"), outputLine("success", fmt.Sprintf("连接地址：%s", address))}, "success", fmt.Sprintf("端口可达，握手耗时 %s", time.Since(start).Round(time.Millisecond)), nil
}

func (s *Service) runHTTP(ctx context.Context, in RunInput) ([]Line, string, string, error) {
	target := strings.TrimSpace(in.Target)
	parsed, err := url.Parse(target)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, "failed", "HTTP 目标校验失败", fmt.Errorf("URL 必须是 http/https 地址")
	}
	if err = validateHost(parsed.Hostname()); err != nil {
		return nil, "failed", "HTTP 目标校验失败", err
	}
	start := time.Now()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, "failed", "HTTP 请求创建失败", err
	}
	request.Header.Set("User-Agent", "cmdb-diagnostic/1.0")
	response, err := safeHTTPClient(10 * time.Second).Do(request)
	if err != nil {
		return []Line{outputLine("error", err.Error())}, "failed", "HTTP 请求失败", nil
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 32*1024))
	status, summary := "success", fmt.Sprintf("HTTP %d，耗时 %s", response.StatusCode, time.Since(start).Round(time.Millisecond))
	lines := []Line{outputLine("info", fmt.Sprintf("请求：GET %s", target)), outputLine("info", fmt.Sprintf("状态码：%d %s", response.StatusCode, response.Status))}
	if response.StatusCode >= 400 {
		status = "warning"
		summary = fmt.Sprintf("HTTP %d，服务返回错误状态，耗时 %s", response.StatusCode, time.Since(start).Round(time.Millisecond))
	}
	if text := strings.TrimSpace(string(body)); text != "" {
		lines = append(lines, outputLine("info", truncateText(text, 16*1024)))
	}
	return lines, status, summary, nil
}
func (s *Service) runTLS(ctx context.Context, in RunInput) ([]Line, string, string, error) {
	host, _, address, err := normalizeHostPort(in.Target, "443")
	if err != nil {
		return nil, "failed", "TLS 目标校验失败", err
	}
	conn, err := safeDialContext(ctx, "tcp", address)
	if err != nil {
		return []Line{outputLine("error", err.Error())}, "failed", "TLS 连接失败", nil
	}
	defer conn.Close()
	tlsConn := tls.Client(conn, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	if err = tlsConn.HandshakeContext(ctx); err != nil {
		return []Line{outputLine("error", err.Error())}, "failed", "TLS 握手失败", nil
	}
	state := tlsConn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return nil, "failed", "TLS 证书缺失", fmt.Errorf("TLS 握手成功但未返回证书")
	}
	cert := state.PeerCertificates[0]
	remaining := int(time.Until(cert.NotAfter).Hours() / 24)
	status, summary := "success", fmt.Sprintf("TLS 握手成功，证书剩余 %d 天", remaining)
	if remaining < 30 {
		status = "warning"
		summary = fmt.Sprintf("TLS 证书将在 %d 天后过期", remaining)
	}
	lines := []Line{
		outputLine("success", fmt.Sprintf("协议版本：%s", tlsVersion(state.Version))),
		outputLine("info", fmt.Sprintf("证书主体：%s", cert.Subject.String())),
		outputLine("info", fmt.Sprintf("签发者：%s", cert.Issuer.String())),
		outputLine("info", fmt.Sprintf("有效期：%s 至 %s", cert.NotBefore.Format(time.RFC3339), cert.NotAfter.Format(time.RFC3339))),
	}
	return lines, status, summary, nil
}

func tlsVersion(version uint16) string {
	switch version {
	case tls.VersionTLS13:
		return "TLS 1.3"
	case tls.VersionTLS12:
		return "TLS 1.2"
	default:
		return fmt.Sprintf("TLS 0x%x", version)
	}
}

func truncateText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "\n...（输出已截断）"
}
func (s *Service) runPrometheus(ctx context.Context, in RunInput) ([]Line, string, string, error) {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("PROMETHEUS_URL")), "/")
	if base == "" {
		return nil, "failed", "Prometheus 未配置", ErrNotConfigured
	}
	if err := validateHost(baseURLHost(base)); err != nil {
		return nil, "failed", "Prometheus 配置校验失败", err
	}
	values := url.Values{"query": {strings.TrimSpace(in.Query)}}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/query?"+values.Encode(), nil)
	if err != nil {
		return nil, "failed", "Prometheus 请求创建失败", err
	}
	response, err := safeHTTPClient(10 * time.Second).Do(request)
	if err != nil {
		return []Line{outputLine("error", err.Error())}, "failed", "Prometheus 请求失败", nil
	}
	defer response.Body.Close()
	var payload struct {
		Status string `json:"status"`
		Error  string `json:"error"`
		Data   struct {
			ResultType string           `json:"resultType"`
			Result     []map[string]any `json:"result"`
		} `json:"data"`
	}
	if err = json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, "failed", "Prometheus 响应解析失败", err
	}
	if response.StatusCode != http.StatusOK || payload.Status != "success" {
		return []Line{outputLine("error", payload.Error)}, "failed", "Prometheus 查询失败", nil
	}
	lines := []Line{outputLine("info", fmt.Sprintf("查询：%s", in.Query)), outputLine("info", fmt.Sprintf("结果类型：%s，样本数：%d", payload.Data.ResultType, len(payload.Data.Result)))}
	for index, item := range payload.Data.Result {
		if index >= 20 {
			break
		}
		encoded, _ := json.Marshal(item)
		lines = append(lines, outputLine("info", string(encoded)))
	}
	return lines, "success", fmt.Sprintf("Prometheus 查询成功，返回 %d 个样本", len(payload.Data.Result)), nil
}

func baseURLHost(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

func (s *Service) runLoki(ctx context.Context, in RunInput) ([]Line, string, string, error) {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("LOKI_URL")), "/")
	if base == "" {
		return nil, "failed", "Loki 未配置", ErrNotConfigured
	}
	if err := validateHost(baseURLHost(base)); err != nil {
		return nil, "failed", "Loki 配置校验失败", err
	}
	end := time.Now()
	values := url.Values{"query": {strings.TrimSpace(in.Query)}, "start": {end.Add(-30 * time.Minute).Format(time.RFC3339Nano)}, "end": {end.Format(time.RFC3339Nano)}, "limit": {"100"}}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/loki/api/v1/query_range?"+values.Encode(), nil)
	if err != nil {
		return nil, "failed", "Loki 请求创建失败", err
	}
	response, err := safeHTTPClient(10 * time.Second).Do(request)
	if err != nil {
		return []Line{outputLine("error", err.Error())}, "failed", "Loki 请求失败", nil
	}
	defer response.Body.Close()
	var payload struct {
		Status string `json:"status"`
		Error  string `json:"error"`
		Data   struct {
			Result []struct {
				Stream map[string]string `json:"stream"`
				Values [][]string        `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if err = json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, "failed", "Loki 响应解析失败", err
	}
	if response.StatusCode != http.StatusOK || payload.Status != "success" {
		return []Line{outputLine("error", payload.Error)}, "failed", "Loki 查询失败", nil
	}
	lines := []Line{outputLine("info", fmt.Sprintf("查询：%s", in.Query)), outputLine("info", fmt.Sprintf("日志流：%d", len(payload.Data.Result)))}
	count := 0
	for _, stream := range payload.Data.Result {
		for _, value := range stream.Values {
			if count >= 50 || len(value) < 2 {
				break
			}
			lines = append(lines, outputLine("info", fmt.Sprintf("%s %s", value[0], value[1])))
			count++
		}
	}
	return lines, "success", fmt.Sprintf("Loki 查询成功，返回 %d 条日志", count), nil
}
