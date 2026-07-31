package auth

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestTokenCacheKeyDoesNotContainRawToken(t *testing.T) {
	token := "sensitive-access-token"
	key := tokenCacheKey(token)
	if strings.Contains(key, token) {
		t.Fatal("cache key must not contain the raw access token")
	}
	if key != tokenCacheKey(token) {
		t.Fatal("cache key must be deterministic")
	}
}

func TestRedisCachesAndEvictsUser(t *testing.T) {
	redisURL := os.Getenv("CMDB_INTEGRATION_REDIS_URL")
	if redisURL == "" {
		t.Skip("CMDB_INTEGRATION_REDIS_URL is not configured")
	}
	client, err := newRedisClient(redisURL)
	if err != nil {
		t.Fatalf("create redis client: %v", err)
	}
	service := &Service{redis: client, cacheTTL: 30 * time.Second}
	token := "integration-token-that-is-never-sent-to-keycloak"
	key := tokenCacheKey(token)
	t.Cleanup(func() {
		_ = client.Del(context.Background(), key).Err()
		_ = client.Close()
	})
	user := User{ID: "integration", Username: "redis-check", Roles: []string{"viewer"}, Permissions: []string{"cmdb:view"}}
	service.cacheUser(token, user)
	cached, ok := service.cachedUser(token)
	if !ok || cached.Username != user.Username || len(cached.Permissions) != 1 {
		t.Fatalf("unexpected cached user: ok=%v user=%+v", ok, cached)
	}
	ttl, err := client.TTL(context.Background(), key).Result()
	if err != nil || ttl <= 0 || ttl > service.cacheTTL {
		t.Fatalf("unexpected cache TTL %v: %v", ttl, err)
	}
	service.evictCachedUser(token)
	if _, ok = service.cachedUser(token); ok {
		t.Fatal("expected cached user to be evicted")
	}
}
