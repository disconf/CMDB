package toolbox

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func (s *Service) runDatabase(ctx context.Context, in RunInput) ([]Line, string, string, error) {
	if s.credentials == nil {
		return nil, "failed", "数据库凭据服务未配置", ErrNotConfigured
	}
	host, _, address, err := normalizeHostPort(in.Target, "5432")
	if err != nil {
		return nil, "failed", "数据库地址校验失败", err
	}
	if err = validateResolvedHost(ctx, host); err != nil {
		return nil, "failed", "数据库地址解析校验失败", err
	}
	material, err := s.credentials(strings.TrimSpace(in.CredentialID))
	if err != nil || strings.TrimSpace(material.Username) == "" || material.Secret == "" {
		return nil, "failed", "数据库凭据不可用", fmt.Errorf("解析数据库凭据失败")
	}
	databaseName := strings.TrimSpace(in.Query)
	dsn := url.URL{Scheme: "postgres", User: url.UserPassword(material.Username, material.Secret), Host: address, Path: "/" + databaseName, RawQuery: "sslmode=prefer&connect_timeout=5"}
	db, err := sql.Open("pgx", dsn.String())
	if err != nil {
		return nil, "failed", "数据库连接初始化失败", err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	start := time.Now()
	if err = db.PingContext(ctx); err != nil {
		return []Line{outputLine("error", err.Error())}, "failed", "数据库连接失败", nil
	}
	var version, currentDatabase, currentUser string
	if err = db.QueryRowContext(ctx, `SELECT version(), current_database(), current_user`).Scan(&version, &currentDatabase, &currentUser); err != nil {
		return []Line{outputLine("error", err.Error())}, "failed", "数据库只读查询失败", nil
	}
	lines := []Line{
		outputLine("success", fmt.Sprintf("连接地址：%s/%s", address, currentDatabase)),
		outputLine("info", fmt.Sprintf("当前用户：%s", currentUser)),
		outputLine("info", version),
	}
	return lines, "success", fmt.Sprintf("数据库连通，耗时 %s", time.Since(start).Round(time.Millisecond)), nil
}
