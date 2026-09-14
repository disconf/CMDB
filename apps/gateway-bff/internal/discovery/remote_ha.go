package discovery

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"strings"
	"time"
)

const defaultRemoteSessionLeaseTTL = 45 * time.Second

func gatewayInstanceID() string {
	for _, key := range []string{"GATEWAY_INSTANCE_ID", "POD_NAME", "HOSTNAME"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	data := make([]byte, 4)
	_, _ = rand.Read(data)
	return "gateway-" + hex.EncodeToString(data)
}

func remoteSessionLeaseTTL() time.Duration {
	value := strings.TrimSpace(os.Getenv("REMOTE_SESSION_LEASE_TTL"))
	if value == "" {
		return defaultRemoteSessionLeaseTTL
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed < 15*time.Second || parsed > 10*time.Minute {
		return defaultRemoteSessionLeaseTTL
	}
	return parsed
}

func (s *Service) RemoteSessionLeaseRenewalInterval() time.Duration {
	interval := s.remoteLeaseTTL / 3
	if interval < 5*time.Second {
		return 5 * time.Second
	}
	return interval
}

func (s *Service) RenewRemoteSessionLease(ctx context.Context, sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || s.instanceID == "" {
		return ErrRemoteValidation
	}
	now := time.Now()
	heartbeatAt := now.Format("2006-01-02 15:04:05")
	leaseExpiresAt := now.Add(s.remoteLeaseTTL).Format("2006-01-02 15:04:05")

	s.mu.RLock()
	found := false
	for i := range s.remoteSessions {
		if s.remoteSessions[i].ID != sessionID || s.remoteSessions[i].Status != "active" {
			continue
		}
		found = true
		owner := strings.TrimSpace(s.remoteSessions[i].OwnerNode)
		leaseUntil := parseRemoteTime(s.remoteSessions[i].LeaseExpiresAt)
		if owner != "" && owner != s.instanceID && leaseUntil.After(now) {
			s.mu.RUnlock()
			return ErrRemoteForbidden
		}
		break
	}
	s.mu.RUnlock()
	if !found {
		return ErrNotFound
	}

	if s.db != nil {
		result, err := s.db.ExecContext(ctx, `UPDATE remote_access_sessions SET owner_node=$2,lease_heartbeat_at=$3,lease_expires_at=$4,updated_at=now() WHERE id=$1 AND status='active' AND (owner_node='' OR owner_node=$2 OR NULLIF(lease_expires_at,'') IS NULL OR NULLIF(lease_expires_at,'')::timestamptz <= now())`, sessionID, s.instanceID, heartbeatAt, leaseExpiresAt)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return ErrRemoteForbidden
		}
	}

	s.mu.Lock()
	for i := range s.remoteSessions {
		if s.remoteSessions[i].ID != sessionID || s.remoteSessions[i].Status != "active" {
			continue
		}
		s.remoteSessions[i].OwnerNode = s.instanceID
		s.remoteSessions[i].LeaseHeartbeatAt = heartbeatAt
		s.remoteSessions[i].LeaseExpiresAt = leaseExpiresAt
		break
	}
	s.mu.Unlock()
	return nil
}

func (s *Service) RunRemoteSessionLeases(ctx context.Context) {
	interval := s.RemoteSessionLeaseRenewalInterval()
	maintain := func() {
		interrupted, err := s.ReconcileRemoteSessionLeases(ctx)
		if err != nil {
			slog.Error("reconcile remote session leases", "error", err)
			return
		}
		if interrupted > 0 {
			slog.Warn("interrupted remote sessions with expired leases", "count", interrupted)
		}
	}
	maintain()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			maintain()
		}
	}
}

func (s *Service) ReconcileRemoteSessionLeases(ctx context.Context) (int, error) {
	if s.db == nil {
		return 0, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM remote_access_sessions WHERE status='active' AND (owner_node='' OR NULLIF(lease_expires_at,'') IS NULL OR NULLIF(lease_expires_at,'')::timestamptz <= now()) ORDER BY updated_at LIMIT 200`)
	if err != nil {
		return 0, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err = rows.Close(); err != nil {
		return 0, err
	}
	if err = rows.Err(); err != nil {
		return 0, err
	}

	interrupted := 0
	for _, id := range ids {
		message := "会话租约已过期，控制节点失联，已自动中断"
		closedAt := time.Now().Format("2006-01-02 15:04:05")
		entry, _ := json.Marshal([]RemoteSessionLog{{Time: time.Now().Format("15:04:05"), Level: "error", Kind: "session", Message: message}})
		result, updateErr := s.db.ExecContext(ctx, `UPDATE remote_access_sessions SET status='interrupted',closed_at=$2,owner_node='',lease_heartbeat_at='',lease_expires_at='',logs=COALESCE(logs,'[]'::jsonb) || $3::jsonb,updated_at=now() WHERE id=$1 AND status='active' AND (owner_node='' OR NULLIF(lease_expires_at,'') IS NULL OR NULLIF(lease_expires_at,'')::timestamptz <= now())`, id, closedAt, entry)
		if updateErr != nil {
			return interrupted, updateErr
		}
		affected, updateErr := result.RowsAffected()
		if updateErr != nil {
			return interrupted, updateErr
		}
		if affected == 0 {
			continue
		}
		s.markRemoteSessionInterrupted(id, closedAt, message)
		interrupted++
	}
	return interrupted, nil
}

func (s *Service) markRemoteSessionInterrupted(sessionID, closedAt, message string) {
	s.mu.Lock()
	for i := range s.remoteSessions {
		if s.remoteSessions[i].ID != sessionID || s.remoteSessions[i].Status != "active" {
			continue
		}
		s.remoteSessions[i].Status = "interrupted"
		s.remoteSessions[i].ClosedAt = closedAt
		s.remoteSessions[i].OwnerNode = ""
		s.remoteSessions[i].LeaseHeartbeatAt = ""
		s.remoteSessions[i].LeaseExpiresAt = ""
		s.remoteSessions[i].Logs = append(s.remoteSessions[i].Logs, RemoteSessionLog{Time: time.Now().Format("15:04:05"), Level: "error", Kind: "session", Message: message})
		break
	}
	s.mu.Unlock()
	s.CloseRemoteTerminal(sessionID)
}

func (s *Service) ShutdownRemoteSessions() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.mu.RLock()
	ids := []string{}
	for i := range s.remoteSessions {
		if s.remoteSessions[i].Status == "active" && s.remoteSessions[i].OwnerNode == s.instanceID {
			ids = append(ids, s.remoteSessions[i].ID)
		}
	}
	s.mu.RUnlock()
	closedAt := time.Now().Format("2006-01-02 15:04:05")
	message := "网关实例正常退出，远程会话已中断"
	for _, id := range ids {
		if s.db != nil {
			result, err := s.db.ExecContext(ctx, `UPDATE remote_access_sessions SET status='interrupted',closed_at=CASE WHEN closed_at='' THEN $3 ELSE closed_at END,owner_node='',lease_heartbeat_at='',lease_expires_at='',updated_at=now() WHERE id=$1 AND status='active' AND owner_node=$2`, id, s.instanceID, closedAt)
			if err != nil {
				slog.Error("release remote session lease", "session", id, "error", err)
			} else if affected, rowsErr := result.RowsAffected(); rowsErr != nil {
				slog.Error("release remote session lease", "session", id, "error", rowsErr)
			} else if affected == 0 {
				continue
			}
		}
		s.markRemoteSessionInterrupted(id, closedAt, message)
	}
	if len(ids) == 0 && s.db != nil {
		if _, err := s.db.ExecContext(ctx, `UPDATE remote_access_sessions SET status='interrupted',closed_at=CASE WHEN closed_at='' THEN $2 ELSE closed_at END,owner_node='',lease_heartbeat_at='',lease_expires_at='',updated_at=now() WHERE status='active' AND owner_node=$1`, s.instanceID, closedAt); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("release remote sessions", "owner", s.instanceID, "error", err)
		}
	}
}
