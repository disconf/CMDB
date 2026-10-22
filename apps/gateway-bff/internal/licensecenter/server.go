package licensecenter

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"cmdb/gateway-bff/internal/license"
)

//go:embed web/*
var assets embed.FS

type session struct {
	Username  string
	ExpiresAt time.Time
}

type Server struct {
	handler      http.Handler
	privateKey   ed25519.PrivateKey
	store        *Store
	adminUser    string
	adminHash    [32]byte
	sessionMu    sync.Mutex
	sessions     map[string]session
	issuer       string
	sessionHours int
}

type issueInput struct {
	Customer     string   `json:"customer"`
	DeploymentID string   `json:"deploymentId"`
	Edition      string   `json:"edition"`
	MaxAssets    int      `json:"maxAssets"`
	ValidityDays int      `json:"validityDays"`
	Features     []string `json:"features"`
	Notes        string   `json:"notes"`
}

func New() (*Server, error) {
	adminUser := strings.TrimSpace(os.Getenv("LICENSE_CENTER_ADMIN_USERNAME"))
	adminPassword := os.Getenv("LICENSE_CENTER_ADMIN_PASSWORD")
	if adminUser == "" || adminPassword == "" {
		return nil, errors.New("LICENSE_CENTER_ADMIN_USERNAME and LICENSE_CENTER_ADMIN_PASSWORD are required")
	}
	privateValue := strings.TrimSpace(os.Getenv("LICENSE_SIGNING_KEY"))
	keyFile := strings.TrimSpace(os.Getenv("LICENSE_SIGNING_KEY_FILE"))
	if privateValue == "" && keyFile != "" {
		data, err := os.ReadFile(keyFile)
		if err != nil {
			return nil, fmt.Errorf("read signing key: %w", err)
		}
		privateValue = string(data)
	}
	privateKey, err := license.ParsePrivateKey(privateValue)
	if err != nil {
		return nil, fmt.Errorf("parse signing key: %w", err)
	}
	dataFile := strings.TrimSpace(os.Getenv("LICENSE_CENTER_DATA_FILE"))
	if dataFile == "" {
		dataFile = ".license-center-data/licenses.json"
	}
	store, err := NewStore(dataFile)
	if err != nil {
		return nil, err
	}
	service := &Server{
		privateKey: privateKey, store: store, adminUser: adminUser, adminHash: sha256.Sum256([]byte(adminPassword)),
		sessions: map[string]session{}, issuer: firstNonEmpty(os.Getenv("LICENSE_CENTER_ISSUER"), "CMDB Licensing Authority"),
		sessionHours: 12,
	}
	if raw := strings.TrimSpace(os.Getenv("LICENSE_CENTER_SESSION_HOURS")); raw != "" {
		if parsed, parseErr := strconv.Atoi(raw); parseErr == nil && parsed >= 1 && parsed <= 72 {
			service.sessionHours = parsed
		}
	}
	service.handler = service.routes()
	return service, nil
}

func (s *Server) Handler() http.Handler { return s.handler }

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("POST /api/logout", s.requireSession(s.logout))
	mux.HandleFunc("GET /api/session", s.sessionInfo)
	mux.HandleFunc("GET /api/licenses", s.requireSession(s.listLicenses))
	mux.HandleFunc("POST /api/licenses", s.requireSession(s.issueLicense))
	mux.HandleFunc("GET /api/licenses/{id}/download", s.requireSession(s.downloadLicense))
	mux.HandleFunc("GET /api/stats", s.requireSession(s.stats))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"code": "NOT_FOUND", "message": "接口不存在"})
	})
	web, err := fs.Sub(assets, "web")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(web))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if file, openErr := web.Open(path); openErr == nil {
			_ = file.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		index, readErr := assets.ReadFile("web/index.html")
		if readErr != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	})
	return requestLogger(mux)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if decodeJSON(r, &input) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST", "message": "登录信息格式不正确"})
		return
	}
	time.Sleep(250 * time.Millisecond)
	hash := sha256.Sum256([]byte(input.Password))
	userOK := subtle.ConstantTimeCompare([]byte(strings.TrimSpace(input.Username)), []byte(s.adminUser)) == 1
	passwordOK := subtle.ConstantTimeCompare(hash[:], s.adminHash[:]) == 1
	if !userOK || !passwordOK {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "INVALID_CREDENTIALS", "message": "管理员账号或密码不正确"})
		return
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "SESSION_FAILED"})
		return
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	expiresAt := time.Now().Add(time.Duration(s.sessionHours) * time.Hour)
	s.sessionMu.Lock()
	s.sessions[token] = session{Username: s.adminUser, ExpiresAt: expiresAt}
	s.sessionMu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name: "license_center_session", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode,
		Secure: requestSecure(r), Expires: expiresAt, MaxAge: s.sessionHours * 3600,
	})
	writeJSON(w, http.StatusOK, map[string]any{"username": s.adminUser, "expiresAt": expiresAt.Format(time.RFC3339)})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("license_center_session"); err == nil {
		s.sessionMu.Lock()
		delete(s.sessions, cookie.Value)
		s.sessionMu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: "license_center_session", Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: requestSecure(r), MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) sessionInfo(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("license_center_session")
	if err != nil || cookie.Value == "" {
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}
	s.sessionMu.Lock()
	current, ok := s.sessions[cookie.Value]
	if !ok || time.Now().After(current.ExpiresAt) {
		delete(s.sessions, cookie.Value)
		ok = false
	}
	s.sessionMu.Unlock()
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "username": current.Username})
}

func (s *Server) listLicenses(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.store.List())
}

func (s *Server) stats(w http.ResponseWriter, _ *http.Request) {
	records := s.store.List()
	now := time.Now()
	totalAssets := 0
	active := 0
	expiring := 0
	for _, item := range records {
		totalAssets += item.MaxAssets
		expires, hasExpiry := license.Expiry(claimsForRecord(item))
		if !hasExpiry || now.Before(expires) {
			active++
		}
		if hasExpiry && expires.After(now) && expires.Before(now.Add(30*24*time.Hour)) {
			expiring++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"totalLicenses": len(records), "activeLicenses": active, "expiringSoon": expiring,
		"issuedCapacity": totalAssets, "generatedAt": now.UTC().Format(time.RFC3339),
	})
}

func (s *Server) issueLicense(w http.ResponseWriter, r *http.Request) {
	var input issueInput
	if decodeJSON(r, &input) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST", "message": "授权信息格式不正确"})
		return
	}
	input.Customer = strings.TrimSpace(input.Customer)
	input.DeploymentID = strings.TrimSpace(input.DeploymentID)
	input.Edition = firstNonEmpty(input.Edition, license.EditionEnterprise)
	input.Notes = strings.TrimSpace(input.Notes)
	if input.Customer == "" || input.DeploymentID == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "VALIDATION_ERROR", "message": "客户名称和部署实例 ID 不能为空"})
		return
	}
	if len(input.DeploymentID) < 8 || len(input.DeploymentID) > 160 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "VALIDATION_ERROR", "message": "部署实例 ID 长度应为 8 到 160 个字符"})
		return
	}
	if input.MaxAssets < license.FreeAssetLimit+1 || input.MaxAssets > 10_000_000 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "VALIDATION_ERROR", "message": "商业授权资产额度必须大于 200 且不超过 10,000,000"})
		return
	}
	if input.ValidityDays < 0 || input.ValidityDays > 3650 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "VALIDATION_ERROR", "message": "有效期必须为 0 到 3650 天"})
		return
	}
	now := time.Now().UTC()
	record := Record{
		ID: newLicenseID(now), Customer: input.Customer, DeploymentID: input.DeploymentID,
		Edition: input.Edition, MaxAssets: input.MaxAssets, IssuedAt: now.Format(time.RFC3339),
		Features: normalizeFeatures(input.Features), Notes: input.Notes, IssuedBy: sessionFromContext(r.Context()).Username,
	}
	if input.ValidityDays > 0 {
		record.ExpiresAt = now.Add(time.Duration(input.ValidityDays) * 24 * time.Hour).Format(time.RFC3339)
	}
	claims := claimsForRecord(record)
	claims.IssuedBy = s.issuer + " / " + record.IssuedBy
	token, err := license.Sign(s.privateKey, claims)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "SIGN_FAILED", "message": err.Error()})
		return
	}
	record.LicenseKey = token
	if err := s.store.Add(record); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "STORE_FAILED", "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, record)
}

func (s *Server) downloadLicense(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	for _, record := range s.store.List() {
		if record.ID != id {
			continue
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.license"`, record.ID))
		_, _ = w.Write([]byte(record.LicenseKey + "\n"))
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"code": "LICENSE_NOT_FOUND", "message": "授权记录不存在"})
}

type contextKey string

const sessionContextKey contextKey = "license-center-session"

func (s *Server) requireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("license_center_session")
		if err != nil || cookie.Value == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "UNAUTHORIZED", "message": "请先登录授权中心"})
			return
		}
		s.sessionMu.Lock()
		current, ok := s.sessions[cookie.Value]
		if !ok || time.Now().After(current.ExpiresAt) {
			delete(s.sessions, cookie.Value)
			ok = false
		}
		s.sessionMu.Unlock()
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "UNAUTHORIZED", "message": "登录状态已失效"})
			return
		}
		next(w, contextWithSession(r, current))
	}
}

func contextWithSession(r *http.Request, current session) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), sessionContextKey, current))
}

func sessionFromContext(ctx context.Context) session {
	value, _ := ctx.Value(sessionContextKey).(session)
	return value
}

func requestSecure(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func decodeJSON(r *http.Request, value any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, 2<<20))
	decoder.DisallowUnknownFields()
	return decoder.Decode(value)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("write license-center response", "error", err)
	}
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		slog.Info("license-center request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(started))
	})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
