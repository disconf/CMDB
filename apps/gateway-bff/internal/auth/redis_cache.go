package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

const authCachePrefix = "cmdb:auth:token:"

func newRedisClient(redisURL string) (*redis.Client, error) {
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	return redis.NewClient(options), nil
}

func tokenCacheKey(token string) string {
	digest := sha256.Sum256([]byte(token))
	return authCachePrefix + hex.EncodeToString(digest[:])
}

func (s *Service) cachedUser(token string) (User, bool) {
	if s.redis == nil || token == "" {
		return User{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	value, err := s.redis.Get(ctx, tokenCacheKey(token)).Bytes()
	if err != nil {
		return User{}, false
	}
	var user User
	if json.Unmarshal(value, &user) != nil {
		return User{}, false
	}
	return user, true
}

func (s *Service) cacheUser(token string, user User) {
	if s.redis == nil || token == "" {
		return
	}
	value, err := json.Marshal(user)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_ = s.redis.Set(ctx, tokenCacheKey(token), value, s.cacheTTL).Err()
}

func (s *Service) evictCachedUser(token string) {
	if s.redis == nil || token == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_ = s.redis.Del(ctx, tokenCacheKey(token)).Err()
}
