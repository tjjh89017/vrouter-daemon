package registry

import (
	"testing"
)

func TestRegisterAndIsConnected(t *testing.T) {
	r := New()

	if r.IsConnected("agent-1") {
		t.Fatal("expected agent-1 to not be connected")
	}

	ok := r.Register("agent-1", nil)
	if !ok {
		t.Fatal("expected Register to return true")
	}

	if !r.IsConnected("agent-1") {
		t.Fatal("expected agent-1 to be connected")
	}
}

func TestRegisterDuplicate(t *testing.T) {
	r := New()
	r.Register("agent-1", nil)

	ok := r.Register("agent-1", nil)
	if ok {
		t.Fatal("expected Register to return false for duplicate")
	}
}

func TestDeregister(t *testing.T) {
	r := New()
	r.Register("agent-1", nil)
	r.Deregister("agent-1")

	if r.IsConnected("agent-1") {
		t.Fatal("expected agent-1 to not be connected after deregister")
	}
}

func TestGetEntry(t *testing.T) {
	r := New()
	r.Register("agent-1", nil)

	entry := r.GetEntry("agent-1")
	if entry == nil {
		t.Fatal("expected entry to not be nil")
	}

	entry = r.GetEntry("agent-2")
	if entry != nil {
		t.Fatal("expected entry to be nil for unregistered agent")
	}
}

func TestUpdateStatus(t *testing.T) {
	r := New()
	r.Register("agent-1", nil)
	r.UpdateStatus("agent-1", "1.0.0", []byte(`{"up":true}`))

	entry := r.GetEntry("agent-1")
	if entry.AgentVersion != "1.0.0" {
		t.Fatalf("expected version 1.0.0, got %s", entry.AgentVersion)
	}
	if string(entry.StatusJSON) != `{"up":true}` {
		t.Fatalf("unexpected status JSON: %s", entry.StatusJSON)
	}
}

func TestReRegisterAfterDeregister(t *testing.T) {
	r := New()
	r.Register("agent-1", nil)
	r.Deregister("agent-1")

	ok := r.Register("agent-1", nil)
	if !ok {
		t.Fatal("expected Register to succeed after deregister")
	}
}
