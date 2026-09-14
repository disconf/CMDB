package discovery

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

func (s *Service) ArchiveRemoteSessionRecording(ctx context.Context, sessionID string) error {
	if s.db == nil || s.objectStore == nil {
		return errors.New("object storage is not configured")
	}
	s.mu.RLock()
	var current RemoteSession
	for _, item := range s.remoteSessions {
		if item.ID == sessionID {
			current = cloneRemoteSession(item)
			break
		}
	}
	s.mu.RUnlock()
	if current.ID == "" {
		return ErrNotFound
	}
	if current.Status == "active" {
		return fmt.Errorf("%w: active sessions cannot be archived", ErrRemoteValidation)
	}
	if current.ArchiveStatus == "archived" && current.ArchiveKey != "" {
		return nil
	}
	if err := s.markSessionArchiveState(sessionID, "archiving", "", ""); err != nil {
		return err
	}
	recording, err := s.terminalRecordingSnapshot(ctx, sessionID)
	if err != nil {
		_ = s.markSessionArchiveState(sessionID, "failed", "", err.Error())
		return err
	}
	if len(recording.Events) == 0 {
		err = errors.New("terminal recording has no events")
		_ = s.markSessionArchiveState(sessionID, "failed", "", err.Error())
		return err
	}
	content, err := encodeRecordingArchive(recording)
	if err != nil {
		_ = s.markSessionArchiveState(sessionID, "failed", "", err.Error())
		return err
	}
	key := "terminal-recordings/" + strings.TrimSpace(sessionID) + ".jsonl.gz"
	if _, err = s.objectStore.Put(ctx, key, content, "application/gzip"); err != nil {
		_ = s.markSessionArchiveState(sessionID, "failed", "", err.Error())
		return err
	}
	sum := sha256.Sum256(content)
	s.mu.Lock()
	for i := range s.remoteSessions {
		if s.remoteSessions[i].ID != sessionID {
			continue
		}
		s.remoteSessions[i].ArchiveStatus = "archived"
		s.remoteSessions[i].ArchiveBucket = s.objectStore.Bucket()
		s.remoteSessions[i].ArchiveKey = key
		s.remoteSessions[i].ArchiveSHA256 = hex.EncodeToString(sum[:])
		s.remoteSessions[i].ArchiveSize = int64(len(content))
		s.remoteSessions[i].ArchivedAt = time.Now().Format("2006-01-02 15:04:05")
		s.remoteSessions[i].ArchiveError = ""
		updated := cloneRemoteSession(s.remoteSessions[i])
		s.mu.Unlock()
		return s.persistRemoteSession(updated)
	}
	s.mu.Unlock()
	return ErrNotFound
}

func (s *Service) markSessionArchiveState(sessionID, status, key, message string) error {
	var updated RemoteSession
	s.mu.Lock()
	for i := range s.remoteSessions {
		if s.remoteSessions[i].ID != sessionID {
			continue
		}
		s.remoteSessions[i].ArchiveStatus = status
		s.remoteSessions[i].ArchiveError = truncateArchiveError(message)
		if status == "archiving" {
			s.remoteSessions[i].ArchiveError = ""
		}
		if key != "" {
			s.remoteSessions[i].ArchiveKey = key
			s.remoteSessions[i].ArchiveBucket = s.objectStore.Bucket()
		}
		updated = cloneRemoteSession(s.remoteSessions[i])
		break
	}
	s.mu.Unlock()
	if updated.ID == "" {
		return ErrNotFound
	}
	return s.persistRemoteSession(updated)
}

func truncateArchiveError(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 1000 {
		return value[:1000]
	}
	return value
}

func encodeRecordingArchive(recording TerminalRecording) ([]byte, error) {
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	encoder := json.NewEncoder(writer)
	for _, event := range recording.Events {
		if err := encoder.Encode(event); err != nil {
			_ = writer.Close()
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func decodeRecordingArchive(content []byte) (TerminalRecording, error) {
	reader, err := gzip.NewReader(bytes.NewReader(content))
	if err != nil {
		return TerminalRecording{}, err
	}
	defer reader.Close()
	decoder := json.NewDecoder(reader)
	result := TerminalRecording{Events: []RemoteTerminalEvent{}}
	for decoder.More() {
		var event RemoteTerminalEvent
		if err = decoder.Decode(&event); err != nil {
			return TerminalRecording{}, err
		}
		result.Events = append(result.Events, event)
	}
	return result, nil
}

func (s *Service) terminalRecordingSnapshot(ctx context.Context, sessionID string) (TerminalRecording, error) {
	result := TerminalRecording{Events: []RemoteTerminalEvent{}}
	rows, err := s.db.QueryContext(ctx, `SELECT sequence,direction,data,created_at FROM remote_terminal_events WHERE session_id=$1 ORDER BY sequence`, sessionID)
	if err != nil {
		return TerminalRecording{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var item RemoteTerminalEvent
		var created time.Time
		if err = rows.Scan(&item.Sequence, &item.Direction, &item.Data, &created); err != nil {
			return TerminalRecording{}, err
		}
		item.CreatedAt = created.Local().Format("2006-01-02 15:04:05.000")
		result.Events = append(result.Events, item)
	}
	return result, rows.Err()
}

func (s *Service) ArchivePendingRemoteSessions(ctx context.Context) (int, error) {
	if s.db == nil || s.objectStore == nil {
		return 0, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM remote_access_sessions WHERE status<>'active' AND archive_status IN ('','failed') AND EXISTS(SELECT 1 FROM remote_terminal_events e WHERE e.session_id=remote_access_sessions.id) ORDER BY updated_at LIMIT 50`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return 0, err
		}
		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		return 0, err
	}
	archived := 0
	for _, id := range ids {
		if err = s.ArchiveRemoteSessionRecording(ctx, id); err != nil {
			continue
		}
		archived++
	}
	return archived, nil
}

func (s *Service) scheduleTerminalArchive(sessionID string) {
	if s.objectStore == nil || s.db == nil {
		return
	}
	go func() {
		time.Sleep(2 * time.Second)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := s.ArchiveRemoteSessionRecording(ctx, sessionID); err != nil {
			slog.Error("archive terminal recording after session close", "session", sessionID, "error", err)
		}
	}()
}
