package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newAPIKeyConcurrencyTestCache(t *testing.T) (*concurrencyCache, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache, ok := NewConcurrencyCache(client, 15, 900).(*concurrencyCache)
	require.True(t, ok, "NewConcurrencyCache must return the Redis concurrency cache")
	return cache, server
}

func TestAPIKeyConcurrencyLimit_IndependentKeysAndRelease(t *testing.T) {
	cache, _ := newAPIKeyConcurrencyTestCache(t)
	ctx := context.Background()
	acquired, err := cache.AcquireAPIKeySlot(ctx, 10, 1, "first")
	require.NoError(t, err)
	require.True(t, acquired)
	acquired, err = cache.AcquireAPIKeySlot(ctx, 10, 1, "second")
	require.NoError(t, err)
	require.False(t, acquired)
	acquired, err = cache.AcquireAPIKeySlot(ctx, 20, 1, "other-key")
	require.NoError(t, err)
	require.True(t, acquired)
	// Retrying the same reservation refreshes it without consuming another slot.
	acquired, err = cache.AcquireAPIKeySlot(ctx, 10, 1, "first")
	require.NoError(t, err)
	require.True(t, acquired)
	counts, err := cache.GetAPIKeyConcurrencyBatch(ctx, []int64{10, 20})
	require.NoError(t, err)
	require.Equal(t, map[int64]int{10: 1, 20: 1}, counts)
	require.NoError(t, cache.ReleaseAPIKeySlot(ctx, 10, "first"))
	acquired, err = cache.AcquireAPIKeySlot(ctx, 10, 1, "second")
	require.NoError(t, err)
	require.True(t, acquired)
}

func TestAPIKeyConcurrencyLimit_AtomicUnderContention(t *testing.T) {
	cache, _ := newAPIKeyConcurrencyTestCache(t)
	ctx := context.Background()
	const attempts, limit = 32, 3
	var wg sync.WaitGroup
	results := make(chan bool, attempts)
	errors := make(chan error, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			acquired, err := cache.AcquireAPIKeySlot(ctx, 10, limit, fmt.Sprintf("request-%d", i))
			results <- acquired
			errors <- err
		}(i)
	}
	wg.Wait()
	close(results)
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	count := 0
	for acquired := range results {
		if acquired {
			count++
		}
	}
	require.Equal(t, limit, count)
	counts, err := cache.GetAPIKeyConcurrencyBatch(ctx, []int64{10})
	require.NoError(t, err)
	require.Equal(t, limit, counts[10])
}

func TestAPIKeyConcurrencyLimit_ExpiredSlotsRecover(t *testing.T) {
	cache, server := newAPIKeyConcurrencyTestCache(t)
	ctx := context.Background()
	acquired, err := cache.AcquireAPIKeySlot(ctx, 10, 1, "crashed-request")
	require.NoError(t, err)
	require.True(t, acquired)
	server.FastForward(16 * time.Minute)
	acquired, err = cache.AcquireAPIKeySlot(ctx, 10, 1, "new-request")
	require.NoError(t, err)
	require.True(t, acquired)
}

func TestAPIKeyConcurrencyLimit_SharedWithLiveCalls(t *testing.T) {
	cache, _ := newAPIKeyConcurrencyTestCache(t)
	ctx := context.Background()
	acquired, err := cache.AcquireAPIKeySlot(ctx, 30, 1, "regular")
	require.NoError(t, err)
	require.True(t, acquired)
	acquired, err = cache.AcquireLiveLeaseWithAPIKeyLimit(ctx, 10, 10, 20, 10, 30, 1, "live", false)
	require.NoError(t, err)
	require.False(t, acquired, "ordinary requests must count toward the Live key limit")
	require.NoError(t, cache.ReleaseAPIKeySlot(ctx, 30, "regular"))
	acquired, err = cache.AcquireLiveLeaseWithAPIKeyLimit(ctx, 10, 10, 20, 10, 30, 1, "live", false)
	require.NoError(t, err)
	require.True(t, acquired)
	acquired, err = cache.AcquireAPIKeySlot(ctx, 30, 1, "ordinary-during-live")
	require.NoError(t, err)
	require.False(t, acquired, "Live calls must count toward the ordinary key limit")
	acquired, err = cache.AcquireLiveLeaseWithAPIKeyLimit(ctx, 11, 10, 20, 10, 30, 1, "second-live", true)
	require.NoError(t, err)
	require.False(t, acquired, "temporary user slots must not grant an extra key slot")
	require.NoError(t, cache.ReleaseLiveLease(ctx, 10, 20, 30, "live"))
	acquired, err = cache.AcquireAPIKeySlot(ctx, 30, 1, "after-live")
	require.NoError(t, err)
	require.True(t, acquired)
}
