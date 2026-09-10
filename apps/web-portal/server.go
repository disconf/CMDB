package main

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
)

//go:embed all:dist
var assets embed.FS

type runtimeConfiguration struct {
	KeycloakURL      string `json:"keycloakUrl"`
	KeycloakRealm    string `json:"keycloakRealm"`
	KeycloakClientID string `json:"keycloakClientId"`
}

func main() {
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	gateway := os.Getenv("GATEWAY_URL")
	if gateway == "" {
		gateway = "http://gateway:8080"
	}
	target, err := url.Parse(gateway)
	if err != nil {
		panic(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, e error) {
		slog.Error("gateway proxy error", "error", e)
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"code":"GATEWAY_UNAVAILABLE"}`))
	}

	sub, err := fs.Sub(assets, "dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))

	mux := http.NewServeMux()
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		proxy.ServeHTTP(w, r)
	})
	mux.HandleFunc("/config.js", func(w http.ResponseWriter, r *http.Request) {
		config := runtimeConfiguration{
			KeycloakURL:      firstNonEmpty(os.Getenv("KEYCLOAK_URL"), inferKeycloakURL(r)),
			KeycloakRealm:    firstNonEmpty(os.Getenv("KEYCLOAK_REALM"), "cmdb"),
			KeycloakClientID: firstNonEmpty(os.Getenv("KEYCLOAK_CLIENT_ID"), "cmdb-web"),
		}
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		payload, _ := json.Marshal(config)
		_, _ = w.Write([]byte("window.__CMDB_CONFIG__="))
		_, _ = w.Write(payload)
		_, _ = w.Write([]byte(";"))
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if f, err := sub.Open(path); err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		index, err := assets.ReadFile("dist/index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	})
	slog.Info("web-static listening", "address", addr, "gateway", gateway)
	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("serve", "error", err)
		os.Exit(1)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func inferKeycloakURL(r *http.Request) string {
	host := firstNonEmpty(r.Header.Get("X-Forwarded-Host"), r.Host)
	scheme := firstNonEmpty(r.Header.Get("X-Forwarded-Proto"), "http")
	if r.TLS != nil {
		scheme = "https"
	}
	name, port := host, ""
	if index := strings.LastIndex(host, ":"); index > 0 && !strings.Contains(host[index+1:], "]") {
		name, port = host[:index], host[index+1:]
	}
	if !strings.HasPrefix(name, "cmdb.") {
		return ""
	}
	result := scheme + "://keycloak." + strings.TrimPrefix(name, "cmdb.")
	if port != "" {
		result += ":" + port
	}
	return result
}
