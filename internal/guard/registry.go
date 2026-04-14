package guard

import (
	"context"
	"fmt"
	"sync"

	"github.com/github/gh-aw-mcpg/internal/logger"
)

var debugLog = logger.New("guard:registry")

// Registry manages guard instances for different MCP servers
type Registry struct {
	guards map[string]Guard // serverID -> guard
	mu     sync.RWMutex
}

// NewRegistry creates a new guard registry
func NewRegistry() *Registry {
	debugLog.Print("Creating new guard registry")
	return &Registry{
		guards: make(map[string]Guard),
	}
}

// Register registers a guard for a specific server
func (r *Registry) Register(serverID string, guard Guard) {
	debugLog.Printf("Registering guard for serverID=%s, guardName=%s", serverID, guard.Name())
	r.mu.Lock()
	defer r.mu.Unlock()

	r.guards[serverID] = guard
	logger.LogInfo("guard", "Registered guard '%s' for server '%s'", guard.Name(), serverID)
}

// Get retrieves the guard for a server, or returns a noop guard if not found
func (r *Registry) Get(serverID string) Guard {
	debugLog.Printf("Getting guard for serverID=%s", serverID)
	r.mu.RLock()
	defer r.mu.RUnlock()

	if guard, ok := r.guards[serverID]; ok {
		debugLog.Printf("Found guard for serverID=%s, guardName=%s", serverID, guard.Name())
		return guard
	}

	// Return noop guard as default
	debugLog.Printf("No guard registered for serverID=%s, returning noop guard", serverID)
	return NewNoopGuard()
}

// Has checks if a guard is registered for a server
func (r *Registry) Has(serverID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.guards[serverID]
	return ok
}

// HasNonNoopGuard returns true if any registered guard is not a noop guard
func (r *Registry) HasNonNoopGuard() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, g := range r.guards {
		if g.Name() != "noop" {
			return true
		}
	}
	return false
}

// Remove removes a guard registration
func (r *Registry) Remove(serverID string) {
	debugLog.Printf("Removing guard for serverID=%s", serverID)
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.guards, serverID)
	logger.LogInfo("guard", "Removed guard for server '%s'", serverID)
}

// List returns all registered server IDs
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	serverIDs := make([]string, 0, len(r.guards))
	for id := range r.guards {
		serverIDs = append(serverIDs, id)
	}
	return serverIDs
}

// GetGuardInfo returns information about all registered guards
func (r *Registry) GetGuardInfo() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	info := make(map[string]string)
	for serverID, guard := range r.guards {
		info[serverID] = guard.Name()
	}
	return info
}

// Close closes all registered guards that implement Close(context.Context) error.
// It should be called during server shutdown to release WASM runtime resources.
func (r *Registry) Close(ctx context.Context) {
	type closableGuard struct {
		id string
		c  interface{ Close(context.Context) error }
	}

	r.mu.Lock()
	closers := make([]closableGuard, 0, len(r.guards))
	for id, g := range r.guards {
		if c, ok := g.(interface{ Close(context.Context) error }); ok {
			closers = append(closers, closableGuard{id: id, c: c})
		}
	}
	r.mu.Unlock()

	for _, guard := range closers {
		if err := guard.c.Close(ctx); err != nil {
			logger.LogWarn("guard", "Failed to close guard for server %s: %v", guard.id, err)
		}
	}
}

// GuardFactory is a function that creates a guard instance
type GuardFactory func() (Guard, error)

// RegisteredGuards maps guard names to their factory functions
var registeredGuards = make(map[string]GuardFactory)
var registeredGuardsMu sync.RWMutex

// RegisterGuardType registers a guard type with a factory function
// This allows dynamic guard creation by name
func RegisterGuardType(name string, factory GuardFactory) {
	registeredGuardsMu.Lock()
	defer registeredGuardsMu.Unlock()
	registeredGuards[name] = factory
	logger.LogInfo("guard", "Registered guard type: %s", name)
}

// CreateGuard creates a guard instance by name using registered factories
func CreateGuard(name string) (Guard, error) {
	debugLog.Printf("Creating guard with name=%s", name)
	registeredGuardsMu.RLock()
	defer registeredGuardsMu.RUnlock()

	// Handle built-in guards
	if name == "noop" || name == "" {
		debugLog.Print("Using built-in noop guard")
		return NewNoopGuard(), nil
	}

	// Try to find in registered factories
	if factory, ok := registeredGuards[name]; ok {
		debugLog.Printf("Found factory for guard type: %s", name)
		return factory()
	}

	debugLog.Printf("Unknown guard type: %s", name)
	return nil, fmt.Errorf("unknown guard type: %s", name)
}

// GetRegisteredGuardTypes returns all registered guard type names
func GetRegisteredGuardTypes() []string {
	registeredGuardsMu.RLock()
	defer registeredGuardsMu.RUnlock()

	types := []string{"noop"} // Always include noop
	for name := range registeredGuards {
		types = append(types, name)
	}
	return types
}
