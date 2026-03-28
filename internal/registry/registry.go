package registry

import (
	"sync"

	agentpb "github.com/tjjh89017/vrouter-daemon/gen/go/agentpb"
)

// AgentStream is the server-side stream for sending messages to an agent.
type AgentStream = agentpb.AgentService_ConnectServer

// Entry holds the connection state for a registered agent.
type Entry struct {
	Stream       AgentStream
	AgentVersion string
	StatusJSON   []byte
}

// Registry tracks connected agents by their agent ID.
type Registry struct {
	mu     sync.RWMutex
	agents map[string]*Entry
}

// New creates a new Registry.
func New() *Registry {
	return &Registry{
		agents: make(map[string]*Entry),
	}
}

// Register adds an agent stream to the registry. Returns false if the agent ID
// is already registered.
func (r *Registry) Register(agentID string, stream AgentStream) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.agents[agentID]; exists {
		return false
	}
	r.agents[agentID] = &Entry{Stream: stream}
	return true
}

// Deregister removes an agent from the registry.
func (r *Registry) Deregister(agentID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.agents, agentID)
}

// IsConnected returns true if the agent is registered.
func (r *Registry) IsConnected(agentID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.agents[agentID]
	return exists
}

// GetEntry returns the entry for an agent, or nil if not found.
func (r *Registry) GetEntry(agentID string) *Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.agents[agentID]
}

// UpdateStatus updates the cached status for an agent.
func (r *Registry) UpdateStatus(agentID string, version string, statusJSON []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if entry, exists := r.agents[agentID]; exists {
		entry.AgentVersion = version
		entry.StatusJSON = statusJSON
	}
}
