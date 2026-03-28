package cluster

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const defaultTTL = 60 * time.Second

// AgentInfo holds agent metadata stored in Redis.
type AgentInfo struct {
	PodIP        string
	AgentVersion string
	StatusJSON   []byte
}

// Registry is a Redis-backed cluster registry that stores agent metadata.
// Each agent is a Redis hash: {prefix}{id} with fields pod_ip, version, status, nonce.
type Registry struct {
	client    *redis.Client
	podIP     string
	ttl       time.Duration
	keyPrefix string // e.g. "vrouter:agent:"
}

// NewRegistry creates a new cluster Registry.
func NewRegistry(redisAddr, podIP string) (*Registry, *redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, nil, fmt.Errorf("redis ping: %w", err)
	}

	return &Registry{
		client:    client,
		podIP:     podIP,
		ttl:       defaultTTL,
		keyPrefix: "vrouter:agent:",
	}, client, nil
}

func (r *Registry) key(agentID string) string {
	return r.keyPrefix + agentID
}

// Register publishes agent info to Redis with TTL.
// Returns a nonce that must be passed to Deregister.
func (r *Registry) Register(ctx context.Context, agentID, version string) (string, error) {
	nonce := uuid.New().String()
	key := r.key(agentID)
	pipe := r.client.Pipeline()
	pipe.HSet(ctx, key, map[string]any{
		"pod_ip":  r.podIP,
		"version": version,
		"status":  "",
		"nonce":   nonce,
	})
	pipe.Expire(ctx, key, r.ttl)
	_, err := pipe.Exec(ctx)
	return nonce, err
}

// Deregister removes the agent from Redis, only if the nonce matches.
func (r *Registry) Deregister(ctx context.Context, agentID, nonce string) error {
	key := r.key(agentID)
	storedNonce, err := r.client.HGet(ctx, key, "nonce").Result()
	if err == redis.Nil {
		return nil
	}
	if err != nil {
		return err
	}
	if storedNonce == nonce {
		return r.client.Del(ctx, key).Err()
	}
	return nil
}

// IsConnected checks if an agent is registered anywhere in the cluster.
func (r *Registry) IsConnected(ctx context.Context, agentID string) (bool, error) {
	exists, err := r.client.Exists(ctx, r.key(agentID)).Result()
	if err != nil {
		return false, err
	}
	return exists > 0, nil
}

// GetInfo returns agent metadata from Redis.
func (r *Registry) GetInfo(ctx context.Context, agentID string) (*AgentInfo, error) {
	vals, err := r.client.HGetAll(ctx, r.key(agentID)).Result()
	if err != nil {
		return nil, err
	}
	if len(vals) == 0 {
		return nil, nil
	}
	return &AgentInfo{
		PodIP:        vals["pod_ip"],
		AgentVersion: vals["version"],
		StatusJSON:   []byte(vals["status"]),
	}, nil
}

// UpdateStatus writes the agent's latest status to Redis.
func (r *Registry) UpdateStatus(ctx context.Context, agentID, version string, statusJSON []byte) error {
	key := r.key(agentID)
	pipe := r.client.Pipeline()
	pipe.HSet(ctx, key, map[string]any{
		"version": version,
		"status":  string(statusJSON),
	})
	pipe.Expire(ctx, key, r.ttl)
	_, err := pipe.Exec(ctx)
	return err
}

// Refresh re-sets the TTL for an agent.
func (r *Registry) Refresh(ctx context.Context, agentID string) error {
	return r.client.Expire(ctx, r.key(agentID), r.ttl).Err()
}

// TestKeyPrefix returns the key prefix (for test helpers to share the namespace).
func (r *Registry) TestKeyPrefix() string {
	return r.keyPrefix
}

// Close closes the Redis client.
func (r *Registry) Close() error {
	return r.client.Close()
}
