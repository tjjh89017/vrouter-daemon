package cluster

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// TestRedisClient creates a Redis client for testing.
// Skips the test if Redis is not available at localhost:6379.
func TestRedisClient(t *testing.T) *redis.Client {
	t.Helper()

	addr := "localhost:6379"
	client := redis.NewClient(&redis.Options{Addr: addr})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis not available at %s: %v", addr, err)
	}

	t.Cleanup(func() { client.Close() })
	return client
}

// TestRegistry creates a cluster Registry with a unique key prefix for testing.
func TestRegistry(t *testing.T, podIP string) (*Registry, redis.UniversalClient) {
	t.Helper()
	client := TestRedisClient(t)
	prefix := fmt.Sprintf("test:%d:", rand.Int63())

	reg := &Registry{
		client:    client,
		podIP:     podIP,
		ttl:       defaultTTL,
		keyPrefix: prefix + "agent:",
	}

	t.Cleanup(func() {
		ctx := context.Background()
		iter := client.Scan(ctx, 0, prefix+"*", 1000).Iterator()
		for iter.Next(ctx) {
			client.Del(ctx, iter.Val())
		}
	})

	return reg, client
}

// TestBrokerWithPrefix creates a Broker with a unique key prefix for testing.
func TestBrokerWithPrefix(t *testing.T, client redis.UniversalClient, prefix string) *Broker {
	t.Helper()
	return &Broker{
		client:        client,
		pendingPrefix: prefix + "pending:",
		reqPrefix:     prefix + "req:",
		donePrefix:    prefix + "done:",
		resultPrefix:  prefix + "result:",
	}
}
