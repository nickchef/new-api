package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMemoryHashCache_SuccessPath(t *testing.T) {
	c := NewMemoryContentModerationHashCache()
	ctx := context.Background()

	ok, err := c.Has(ctx, "h1")
	require.NoError(t, err)
	require.False(t, ok)

	require.NoError(t, c.Record(ctx, "h1"))
	require.NoError(t, c.Record(ctx, "h2"))
	require.NoError(t, c.Record(ctx, "h1"))

	ok, err = c.Has(ctx, "h1")
	require.NoError(t, err)
	require.True(t, ok)

	count, err := c.Count(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(2), count)

	require.NoError(t, c.Delete(ctx, "h1"))
	ok, _ = c.Has(ctx, "h1")
	require.False(t, ok)
	count, _ = c.Count(ctx)
	require.Equal(t, int64(1), count)

	require.NoError(t, c.Clear(ctx))
	count, _ = c.Count(ctx)
	require.Equal(t, int64(0), count)
}

func TestMemoryHashCache_EmptyHashNoop(t *testing.T) {
	c := NewMemoryContentModerationHashCache()
	ctx := context.Background()
	require.NoError(t, c.Record(ctx, ""))
	ok, err := c.Has(ctx, "")
	require.NoError(t, err)
	require.False(t, ok)
	require.NoError(t, c.Delete(ctx, ""))
	count, _ := c.Count(ctx)
	require.Equal(t, int64(0), count)
}

func TestRedisHashCache_NilClientReturnsError(t *testing.T) {
	c := NewRedisContentModerationHashCache(nil)
	ctx := context.Background()
	_, err := c.Has(ctx, "x")
	require.Error(t, err)
	require.Error(t, c.Record(ctx, "x"))
	require.Error(t, c.Delete(ctx, "x"))
	require.Error(t, c.Clear(ctx))
	_, err = c.Count(ctx)
	require.Error(t, err)
}

func TestRedisHashCache_EmptyHashShortCircuits(t *testing.T) {
	// 即使没有真实 Redis，空 hash 都应直接返回 false / nil
	c := NewRedisContentModerationHashCache(nil)
	ctx := context.Background()
	ok, err := c.Has(ctx, "")
	require.NoError(t, err)
	require.False(t, ok)
	require.NoError(t, c.Record(ctx, ""))
	require.NoError(t, c.Delete(ctx, ""))
}
