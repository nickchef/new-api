package service

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/go-redis/redis/v8"
)

// ContentModerationViolationCounter 是用户违规滑窗计数器接口。
//   - IncrAndCount：原子地"加一次违规 + 滑窗清理 + 返回窗口内总数"
//   - GetCount：只查不增
//   - Clear：解封用户时清零
type ContentModerationViolationCounter interface {
	IncrAndCount(ctx context.Context, userID int, windowSeconds int) (int64, error)
	GetCount(ctx context.Context, userID int, windowSeconds int) (int64, error)
	Clear(ctx context.Context, userID int) error
}

// CMViolationCounter 是包级共享单例。InitContentModerationViolationCounter
// 根据 Redis 可用性切换实现。
var CMViolationCounter ContentModerationViolationCounter = NewMemoryViolationCounter()

// InitContentModerationViolationCounter 在 main.go 启动时调用。
func InitContentModerationViolationCounter() {
	if common.RedisEnabled && common.RDB != nil {
		CMViolationCounter = NewRedisViolationCounter(common.RDB)
	}
}

// ---------- Redis (ZSET) implementation ----------

type redisViolationCounter struct {
	rdb *redis.Client
}

// NewRedisViolationCounter 构造基于 Redis ZSET 滑窗的实现。
func NewRedisViolationCounter(rdb *redis.Client) ContentModerationViolationCounter {
	return &redisViolationCounter{rdb: rdb}
}

func (r *redisViolationCounter) IncrAndCount(ctx context.Context, userID, windowSeconds int) (int64, error) {
	if r == nil || r.rdb == nil {
		return 0, errors.New("redis client unavailable")
	}
	if userID <= 0 {
		return 0, nil
	}
	if windowSeconds <= 0 {
		windowSeconds = 3600
	}
	key := common.ContentModerationUserViolationsKey(userID)
	now := time.Now().Unix()
	cutoff := now - int64(windowSeconds)
	member := strconv.FormatInt(now, 10) + ":" + strconv.FormatInt(time.Now().UnixNano(), 10)

	pipe := r.rdb.Pipeline()
	pipe.ZAdd(ctx, key, &redis.Z{Score: float64(now), Member: member})
	pipe.ZRemRangeByScore(ctx, key, "0", "("+strconv.FormatInt(cutoff, 10))
	countCmd := pipe.ZCard(ctx, key)
	pipe.Expire(ctx, key, time.Duration(windowSeconds*2)*time.Second)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}
	return countCmd.Val(), nil
}

func (r *redisViolationCounter) GetCount(ctx context.Context, userID, windowSeconds int) (int64, error) {
	if r == nil || r.rdb == nil {
		return 0, errors.New("redis client unavailable")
	}
	if userID <= 0 {
		return 0, nil
	}
	if windowSeconds <= 0 {
		windowSeconds = 3600
	}
	key := common.ContentModerationUserViolationsKey(userID)
	now := time.Now().Unix()
	cutoff := now - int64(windowSeconds)
	pipe := r.rdb.Pipeline()
	pipe.ZRemRangeByScore(ctx, key, "0", "("+strconv.FormatInt(cutoff, 10))
	countCmd := pipe.ZCard(ctx, key)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}
	return countCmd.Val(), nil
}

func (r *redisViolationCounter) Clear(ctx context.Context, userID int) error {
	if r == nil || r.rdb == nil {
		return errors.New("redis client unavailable")
	}
	if userID <= 0 {
		return nil
	}
	return r.rdb.Del(ctx, common.ContentModerationUserViolationsKey(userID)).Err()
}

// ---------- In-memory fallback (single-process) ----------

type memoryViolationCounter struct {
	mu     sync.Mutex
	events map[int][]int64
}

// NewMemoryViolationCounter 单进程内存实现。
func NewMemoryViolationCounter() ContentModerationViolationCounter {
	return &memoryViolationCounter{events: map[int][]int64{}}
}

func (m *memoryViolationCounter) IncrAndCount(_ context.Context, userID, windowSeconds int) (int64, error) {
	if userID <= 0 {
		return 0, nil
	}
	if windowSeconds <= 0 {
		windowSeconds = 3600
	}
	now := time.Now().Unix()
	cutoff := now - int64(windowSeconds)

	m.mu.Lock()
	defer m.mu.Unlock()
	events := append(m.events[userID], now)
	pruned := events[:0]
	for _, ts := range events {
		if ts >= cutoff {
			pruned = append(pruned, ts)
		}
	}
	m.events[userID] = pruned
	return int64(len(pruned)), nil
}

func (m *memoryViolationCounter) GetCount(_ context.Context, userID, windowSeconds int) (int64, error) {
	if userID <= 0 {
		return 0, nil
	}
	if windowSeconds <= 0 {
		windowSeconds = 3600
	}
	now := time.Now().Unix()
	cutoff := now - int64(windowSeconds)

	m.mu.Lock()
	defer m.mu.Unlock()
	events := m.events[userID]
	pruned := events[:0]
	for _, ts := range events {
		if ts >= cutoff {
			pruned = append(pruned, ts)
		}
	}
	m.events[userID] = pruned
	return int64(len(pruned)), nil
}

func (m *memoryViolationCounter) Clear(_ context.Context, userID int) error {
	if userID <= 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.events, userID)
	return nil
}
