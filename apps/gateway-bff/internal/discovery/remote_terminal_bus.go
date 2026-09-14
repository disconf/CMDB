package discovery

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	remoteTerminalPresenceTTL = 45 * time.Second
	remoteTerminalControlWait = 3 * time.Second
)

type remoteTerminalBusEvent struct {
	Kind      string `json:"kind"`
	Origin    string `json:"origin,omitempty"`
	Operator  string `json:"operator,omitempty"`
	Data      string `json:"data,omitempty"`
	Sequence  int64  `json:"sequence,omitempty"`
	Cols      int    `json:"cols,omitempty"`
	Rows      int    `json:"rows,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
}

type RemoteTerminalBus struct {
	client     redis.UniversalClient
	instanceID string
}

func newRemoteTerminalBusFromEnv(instanceID string) (*RemoteTerminalBus, error) {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("REMOTE_TERMINAL_BUS")))
	if mode == "disabled" || mode == "off" || mode == "none" {
		return nil, nil
	}
	rawURL := strings.TrimSpace(os.Getenv("REMOTE_TERMINAL_REDIS_URL"))
	if rawURL == "" {
		rawURL = strings.TrimSpace(os.Getenv("REDIS_URL"))
	}
	if rawURL == "" {
		return nil, nil
	}
	client, err := newRemoteTerminalRedisClient(rawURL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err = client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	return &RemoteTerminalBus{client: client, instanceID: instanceID}, nil
}

func newRemoteTerminalRedisClient(rawURL string) (redis.UniversalClient, error) {
	options, err := redis.ParseURL(rawURL)
	if err != nil {
		return nil, err
	}
	addrs := []string{options.Addr}
	if configured := strings.TrimSpace(os.Getenv("REDIS_CLUSTER_ADDRS")); configured != "" {
		addrs = splitList(configured)
	}
	if len(addrs) == 0 || strings.TrimSpace(addrs[0]) == "" {
		return nil, errors.New("redis address is empty")
	}
	clusterMode := strings.EqualFold(strings.TrimSpace(os.Getenv("REDIS_CLUSTER_MODE")), "true")
	if !clusterMode {
		clusterMode = strings.Contains(strings.ToLower(rawURL), "redis-cluster")
	}
	return redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs:                 addrs,
		Username:              options.Username,
		Password:              options.Password,
		DB:                    options.DB,
		TLSConfig:             options.TLSConfig,
		IsClusterMode:         clusterMode,
		DialTimeout:           3 * time.Second,
		ReadTimeout:           5 * time.Second,
		WriteTimeout:          3 * time.Second,
		MaxRetries:            2,
		MinRetryBackoff:       100 * time.Millisecond,
		MaxRetryBackoff:       time.Second,
		PoolSize:              8,
		MinIdleConns:          1,
		ConnMaxIdleTime:       5 * time.Minute,
		ContextTimeoutEnabled: true,
	}), nil
}

func remoteTerminalControlChannel(sessionID string) string {
	return "cmdb:remote:terminal:{" + strings.TrimSpace(sessionID) + "}:control"
}

func remoteTerminalOutputChannel(sessionID string) string {
	return "cmdb:remote:terminal:{" + strings.TrimSpace(sessionID) + "}:output"
}

func remoteTerminalPresenceKey(sessionID string) string {
	return "cmdb:remote:terminal:{" + strings.TrimSpace(sessionID) + "}:presence"
}

func remoteTerminalSequenceKey(sessionID string) string {
	return "cmdb:remote:terminal:{" + strings.TrimSpace(sessionID) + "}:sequence"
}

func (b *RemoteTerminalBus) PublishOutput(ctx context.Context, sessionID string, sequence int64, data []byte) error {
	return b.publish(ctx, remoteTerminalOutputChannel(sessionID), remoteTerminalBusEvent{Kind: "output", Sequence: sequence, Data: base64.StdEncoding.EncodeToString(data)})
}

func (b *RemoteTerminalBus) PublishInput(ctx context.Context, sessionID, operator string, data []byte) error {
	return b.publish(ctx, remoteTerminalControlChannel(sessionID), remoteTerminalBusEvent{Kind: "input", Operator: operator, Data: base64.StdEncoding.EncodeToString(data)})
}

func (b *RemoteTerminalBus) PublishResize(ctx context.Context, sessionID string, cols, rows int) error {
	return b.publish(ctx, remoteTerminalControlChannel(sessionID), remoteTerminalBusEvent{Kind: "resize", Cols: cols, Rows: rows})
}

func (b *RemoteTerminalBus) PublishClose(ctx context.Context, sessionID string) error {
	return b.publish(ctx, remoteTerminalControlChannel(sessionID), remoteTerminalBusEvent{Kind: "close"})
}

func (b *RemoteTerminalBus) PublishRelease(ctx context.Context, sessionID string) error {
	return b.publish(ctx, remoteTerminalControlChannel(sessionID), remoteTerminalBusEvent{Kind: "release"})
}
func (b *RemoteTerminalBus) PublishOwnerOffline(ctx context.Context, sessionID string) error {
	return b.publish(ctx, remoteTerminalOutputChannel(sessionID), remoteTerminalBusEvent{Kind: "owner_offline"})
}

func (b *RemoteTerminalBus) publish(ctx context.Context, channel string, event remoteTerminalBusEvent) error {
	event.Origin = b.instanceID
	if event.CreatedAt == "" {
		event.CreatedAt = time.Now().Format(time.RFC3339Nano)
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return b.client.Publish(ctx, channel, payload).Err()
}

func (b *RemoteTerminalBus) NextSequence(ctx context.Context, sessionID string) (int64, error) {
	return b.client.Incr(ctx, remoteTerminalSequenceKey(sessionID)).Result()
}

func (b *RemoteTerminalBus) SubscribeControl(ctx context.Context, sessionID string) (*redis.PubSub, error) {
	return b.subscribe(ctx, remoteTerminalControlChannel(sessionID))
}

func (b *RemoteTerminalBus) SubscribeOutput(ctx context.Context, sessionID string) (*redis.PubSub, error) {
	return b.subscribe(ctx, remoteTerminalOutputChannel(sessionID))
}

func (b *RemoteTerminalBus) subscribe(ctx context.Context, channel string) (*redis.PubSub, error) {
	subscription := b.client.Subscribe(ctx, channel)
	if _, err := subscription.Receive(ctx); err != nil {
		_ = subscription.Close()
		return nil, err
	}
	return subscription, nil
}

func (b *RemoteTerminalBus) HeartbeatPresence(ctx context.Context, sessionID, participantID, access string) error {
	key := remoteTerminalPresenceKey(sessionID)
	value := fmt.Sprintf("%s|%d", access, time.Now().Unix())
	pipe := b.client.Pipeline()
	pipe.HSet(ctx, key, participantID, value)
	pipe.Expire(ctx, key, remoteTerminalPresenceTTL)
	_, err := pipe.Exec(ctx)
	return err
}

func (b *RemoteTerminalBus) RemovePresence(ctx context.Context, sessionID, participantID string) error {
	return b.client.HDel(ctx, remoteTerminalPresenceKey(sessionID), participantID).Err()
}

func (b *RemoteTerminalBus) Presence(ctx context.Context, sessionID string) (int, bool) {
	values, err := b.client.HGetAll(ctx, remoteTerminalPresenceKey(sessionID)).Result()
	if err != nil {
		return 0, false
	}
	now := time.Now().Unix()
	count := 0
	controller := false
	for _, raw := range values {
		access, timestamp, ok := strings.Cut(raw, "|")
		if !ok {
			continue
		}
		var seen int64
		if _, err = fmt.Sscanf(timestamp, "%d", &seen); err != nil || now-seen > int64(remoteTerminalPresenceTTL/time.Second)+5 {
			continue
		}
		count++
		if access == "control" {
			controller = true
		}
	}
	return count, controller
}

func (b *RemoteTerminalBus) Close() error {
	if b == nil || b.client == nil {
		return nil
	}
	return b.client.Close()
}

func decodeRemoteTerminalBusEvent(payload string) (remoteTerminalBusEvent, error) {
	var event remoteTerminalBusEvent
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return remoteTerminalBusEvent{}, err
	}
	if strings.TrimSpace(event.Kind) == "" {
		return remoteTerminalBusEvent{}, errors.New("terminal bus event kind is empty")
	}
	return event, nil
}

func remoteTerminalEventData(event remoteTerminalBusEvent) ([]byte, error) {
	if event.Data == "" {
		return nil, nil
	}
	return base64.StdEncoding.DecodeString(event.Data)
}

func (b *RemoteTerminalBus) logError(action string, err error) {
	if err != nil && !errors.Is(err, context.Canceled) {
		slog.Warn("remote terminal bus "+action, "instance", b.instanceID, "error", err)
	}
}
