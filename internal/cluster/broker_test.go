package cluster

import (
	"context"
	"testing"
	"time"
)

func TestBrokerSubmitAndWatch(t *testing.T) {
	_, client := TestRegistry(t, "127.0.0.1")
	broker := TestBrokerWithPrefix(t, client, "brokertest:")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go broker.Watch(ctx, "test-agent", func(ctx context.Context, req *Request) *Result {
		return &Result{
			Success: true,
			Stdout:  "handled: " + string(req.ConfigPayload),
		}
	})

	time.Sleep(100 * time.Millisecond)

	result, err := broker.Submit(context.Background(), "test-agent", []byte("set foo"), 5*time.Second)
	if err != nil {
		t.Fatalf("Submit error: %v", err)
	}
	if !result.Success {
		t.Fatal("expected success")
	}
	if result.Stdout != "handled: set foo" {
		t.Fatalf("unexpected stdout: %q", result.Stdout)
	}
}

func TestBrokerSubmitTimeout(t *testing.T) {
	_, client := TestRegistry(t, "127.0.0.1")
	broker := TestBrokerWithPrefix(t, client, "brokertest2:")

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_, err := broker.Submit(ctx, "missing-agent", []byte("set foo"), 500*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}
