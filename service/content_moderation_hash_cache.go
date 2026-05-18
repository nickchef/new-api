package service

import (
	"context"
	"errors"
	"sync"

	"github.com/QuantumNous/new-api/common"

	"github.com/go-redis/redis/v8"
)

// ContentModerationHashCache 是命中 hash 黑名单的 Set 缓存接口。
//
// 生产环境用 Redis 实现（CMRedisHashCache），测试 / Redis 不可用时回退到内存实现
// （CMMemoryHashCache）。
type ContentModerationHashCache interface {
	Has(ctx context.Context, hash string) (bool, error)
	Record(ctx context.Context, hash string) error
	Delete(ctx context.Context, hash string) error
	Clear(ctx context.Context) error
	Count(ctx context.Context) (int64, error)
}

// CMHashCache 是包级共享单例。由 InitContentModerationHashCache 注入。
var CMHashCache ContentModerationHashCache = NewMemoryContentModerationHashCache()

// InitContentModerationHashCache 在 main.go 启动时调用：
//   - 若 common.RedisEnabled 且 common.RDB != nil 用 RedisHashCache
//   - 否则维持 MemoryHashCache（仅供单实例部署兜底）
func InitContentModerationHashCache() {
	if common.RedisEnabled && common.RDB != nil {
		CMHashCache = NewRedisContentModerationHashCache(common.RDB)
	}
}

// ---------- Redis implementation ----------

type redisHashCache struct {
	rdb *redis.Client
}

// NewRedisContentModerationHashCache 构造基于 Redis Set 的实现。
func NewRedisContentModerationHashCache(rdb *redis.Client) ContentModerationHashCache {
	return &redisHashCache{rdb: rdb}
}

func (r *redisHashCache) Has(ctx context.Context, hash string) (bool, error) {
	if hash == "" {
		return false, nil
	}
	if r == nil || r.rdb == nil {
		return false, errors.New("redis client unavailable")
	}
	return r.rdb.SIsMember(ctx, common.CMRedisKeyFlaggedHashes, hash).Result()
}

func (r *redisHashCache) Record(ctx context.Context, hash string) error {
	if hash == "" {
		return nil
	}
	if r == nil || r.rdb == nil {
		return errors.New("redis client unavailable")
	}
	return r.rdb.SAdd(ctx, common.CMRedisKeyFlaggedHashes, hash).Err()
}

func (r *redisHashCache) Delete(ctx context.Context, hash string) error {
	if hash == "" {
		return nil
	}
	if r == nil || r.rdb == nil {
		return errors.New("redis client unavailable")
	}
	return r.rdb.SRem(ctx, common.CMRedisKeyFlaggedHashes, hash).Err()
}

func (r *redisHashCache) Clear(ctx context.Context) error {
	if r == nil || r.rdb == nil {
		return errors.New("redis client unavailable")
	}
	return r.rdb.Del(ctx, common.CMRedisKeyFlaggedHashes).Err()
}

func (r *redisHashCache) Count(ctx context.Context) (int64, error) {
	if r == nil || r.rdb == nil {
		return 0, errors.New("redis client unavailable")
	}
	return r.rdb.SCard(ctx, common.CMRedisKeyFlaggedHashes).Result()
}

// ---------- In-memory fallback ----------

type memoryHashCache struct {
	mu    sync.RWMutex
	store map[string]struct{}
}

// NewMemoryContentModerationHashCache 单进程内存实现。Redis 不可达时使用，
// 在多实例部署下不能跨节点同步。
func NewMemoryContentModerationHashCache() ContentModerationHashCache {
	return &memoryHashCache{store: map[string]struct{}{}}
}

func (m *memoryHashCache) Has(_ context.Context, hash string) (bool, error) {
	if hash == "" {
		return false, nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.store[hash]
	return ok, nil
}

func (m *memoryHashCache) Record(_ context.Context, hash string) error {
	if hash == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store[hash] = struct{}{}
	return nil
}

func (m *memoryHashCache) Delete(_ context.Context, hash string) error {
	if hash == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.store, hash)
	return nil
}

func (m *memoryHashCache) Clear(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store = map[string]struct{}{}
	return nil
}

func (m *memoryHashCache) Count(_ context.Context) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return int64(len(m.store)), nil
}
