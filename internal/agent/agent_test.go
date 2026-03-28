package agent

import (
	"context"
	"testing"
	"time"
)

func TestNewDefaults(t *testing.T) {
	a := New("localhost:50051", "test-agent")

	if a.serverAddr != "localhost:50051" {
		t.Fatalf("expected serverAddr localhost:50051, got %s", a.serverAddr)
	}
	if a.agentID != "test-agent" {
		t.Fatalf("expected agentID test-agent, got %s", a.agentID)
	}
	if a.version != "dev" {
		t.Fatalf("expected version dev, got %s", a.version)
	}
}

func TestNewWithOptions(t *testing.T) {
	handler := func(ctx context.Context, config string) (string, string, int, error) {
		return "ok", "", 0, nil
	}

	a := New("server:1234", "my-agent",
		WithVersion("2.0.0"),
		WithConfigHandler(handler),
		WithReconnect(5*time.Second, 60*time.Second),
	)

	if a.version != "2.0.0" {
		t.Fatalf("expected version 2.0.0, got %s", a.version)
	}
	if a.reconnectMin != 5*time.Second {
		t.Fatalf("expected reconnectMin 5s, got %v", a.reconnectMin)
	}
	if a.reconnectMax != 60*time.Second {
		t.Fatalf("expected reconnectMax 60s, got %v", a.reconnectMax)
	}
}

func TestRunCancelledContext(t *testing.T) {
	a := New("localhost:0", "test-agent",
		WithReconnect(10*time.Millisecond, 10*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := a.Run(ctx)
	if err != context.DeadlineExceeded {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
}
