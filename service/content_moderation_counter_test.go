package service

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMemoryViolationCounter_BasicIncrAndClear(t *testing.T) {
	c := NewMemoryViolationCounter()
	ctx := context.Background()

	// 第一个用户连续 incr 3 次
	for i := 0; i < 3; i++ {
		count, err := c.IncrAndCount(ctx, 1, 3600)
		require.NoError(t, err)
		require.Equal(t, int64(i+1), count)
	}

	// 不同用户互不干扰
	count, err := c.IncrAndCount(ctx, 2, 3600)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)

	// GetCount 只查不增
	count, err = c.GetCount(ctx, 1, 3600)
	require.NoError(t, err)
	require.Equal(t, int64(3), count)
	count, _ = c.GetCount(ctx, 1, 3600)
	require.Equal(t, int64(3), count)

	// Clear 清零
	require.NoError(t, c.Clear(ctx, 1))
	count, _ = c.GetCount(ctx, 1, 3600)
	require.Equal(t, int64(0), count)
	// user 2 仍在
	count, _ = c.GetCount(ctx, 2, 3600)
	require.Equal(t, int64(1), count)
}

func TestMemoryViolationCounter_ConcurrentIncr(t *testing.T) {
	c := NewMemoryViolationCounter()
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = c.IncrAndCount(ctx, 99, 3600)
		}()
	}
	wg.Wait()
	count, _ := c.GetCount(ctx, 99, 3600)
	require.Equal(t, int64(50), count)
}

func TestMemoryViolationCounter_InvalidArgs(t *testing.T) {
	c := NewMemoryViolationCounter()
	ctx := context.Background()
	count, err := c.IncrAndCount(ctx, 0, 3600)
	require.NoError(t, err)
	require.Equal(t, int64(0), count)
	count, err = c.GetCount(ctx, -1, 3600)
	require.NoError(t, err)
	require.Equal(t, int64(0), count)
	require.NoError(t, c.Clear(ctx, 0))
}

func TestMemoryViolationCounter_WindowFallback(t *testing.T) {
	// windowSeconds <= 0 应回退到 3600
	c := NewMemoryViolationCounter()
	ctx := context.Background()
	count, err := c.IncrAndCount(ctx, 5, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
}

func TestRedisViolationCounter_NilClient(t *testing.T) {
	c := NewRedisViolationCounter(nil)
	ctx := context.Background()
	_, err := c.IncrAndCount(ctx, 1, 3600)
	require.Error(t, err)
	_, err = c.GetCount(ctx, 1, 3600)
	require.Error(t, err)
	require.Error(t, c.Clear(ctx, 1))
}
