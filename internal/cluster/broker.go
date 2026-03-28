package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const requestTTL = 120 * time.Second

// Request is a config apply request stored in Redis.
type Request struct {
	ID            string `json:"id"`
	AgentID       string `json:"agent_id"`
	ConfigPayload []byte `json:"config_payload"`
}

// Result is a config apply result stored in Redis.
type Result struct {
	Success      bool   `json:"success"`
	ExitCode     int    `json:"exit_code"`
	Stdout       string `json:"stdout"`
	Stderr       string `json:"stderr"`
	ErrorMessage string `json:"error,omitempty"`
}

// Broker handles request-response correlation through Redis.
type Broker struct {
	client        *redis.Client
	pendingPrefix string // "vrouter:pending:"
	reqPrefix     string // "vrouter:req:"
	donePrefix    string // "vrouter:done:"
	resultPrefix  string // "vrouter:result:"
}

// NewBroker creates a new Broker.
func NewBroker(client *redis.Client) *Broker {
	return &Broker{
		client:        client,
		pendingPrefix: "vrouter:pending:",
		reqPrefix:     "vrouter:req:",
		donePrefix:    "vrouter:done:",
		resultPrefix:  "vrouter:result:",
	}
}

// Submit puts a request into the agent's pending queue and blocks until
// the result is ready or the context is cancelled.
func (b *Broker) Submit(ctx context.Context, agentID string, configPayload []byte, timeout time.Duration) (*Result, error) {
	reqID := uuid.New().String()

	req := Request{
		ID:            reqID,
		AgentID:       agentID,
		ConfigPayload: configPayload,
	}
	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// Store request data and push ID to agent's pending queue
	pipe := b.client.Pipeline()
	pipe.Set(ctx, b.reqPrefix+reqID, reqData, requestTTL)
	pipe.RPush(ctx, b.pendingPrefix+agentID, reqID)
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("submit request to Redis: %w", err)
	}

	// Wait for result via BLPOP on the done key
	blpopTimeout := timeout + 2*time.Second
	vals, err := b.client.BLPop(ctx, blpopTimeout, b.donePrefix+reqID).Result()
	if err != nil {
		b.cleanup(reqID, agentID)
		if err == redis.Nil || ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("wait for result: %w", err)
	}
	_ = vals

	// Read the result
	resultData, err := b.client.Get(ctx, b.resultPrefix+reqID).Bytes()
	if err != nil {
		return nil, fmt.Errorf("read result: %w", err)
	}

	var result Result
	if err := json.Unmarshal(resultData, &result); err != nil {
		return nil, fmt.Errorf("unmarshal result: %w", err)
	}

	b.cleanup(reqID, "")
	return &result, nil
}

// Watch blocks and processes pending requests for a given agent.
// For each request, it calls the handler and writes the result back to Redis.
// Stops when ctx is cancelled.
func (b *Broker) Watch(ctx context.Context, agentID string, handler func(ctx context.Context, req *Request) *Result) error {
	pendingKey := b.pendingPrefix + agentID
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// BLPOP with 1s timeout. Use background context for the Redis call
		// so that a cancelled ctx doesn't cause us to lose a popped item.
		blpopCtx, blpopCancel := context.WithTimeout(context.Background(), 1*time.Second)
		vals, err := b.client.BLPop(blpopCtx, 1*time.Second, pendingKey).Result()
		blpopCancel()

		if err == redis.Nil || err != nil {
			continue
		}

		// If our parent context is done, push the item back.
		if ctx.Err() != nil {
			b.client.LPush(context.Background(), pendingKey, vals[1])
			return ctx.Err()
		}

		reqID := vals[1]

		reqData, err := b.client.Get(context.Background(), b.reqPrefix+reqID).Bytes()
		if err != nil {
			continue
		}

		var req Request
		if err := json.Unmarshal(reqData, &req); err != nil {
			continue
		}

		result := handler(ctx, &req)

		bgCtx := context.Background()
		resultData, _ := json.Marshal(result)
		pipe := b.client.Pipeline()
		pipe.Set(bgCtx, b.resultPrefix+reqID, resultData, requestTTL)
		pipe.RPush(bgCtx, b.donePrefix+reqID, "1")
		pipe.Expire(bgCtx, b.donePrefix+reqID, requestTTL)
		if _, err := pipe.Exec(bgCtx); err != nil {
			continue
		}

		b.client.Del(bgCtx, b.reqPrefix+reqID)
	}
}

func (b *Broker) cleanup(reqID, agentID string) {
	ctx := context.Background()
	b.client.Del(ctx, b.reqPrefix+reqID, b.resultPrefix+reqID, b.donePrefix+reqID)
	if agentID != "" {
		b.client.LRem(ctx, b.pendingPrefix+agentID, 1, reqID)
	}
}
