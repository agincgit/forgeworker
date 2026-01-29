// Package registry provides a task handler registry for ForgeWorker.
// It allows users to register custom handlers for different task types,
// making ForgeWorker suitable for use as a library.
package registry

import (
	"context"
	"fmt"
	"sync"

	"github.com/rs/zerolog"
)

// Task represents a task to be executed.
type Task struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	Status      string                 `json:"status"`
	Payload     map[string]interface{} `json:"payload,omitempty"`
	RequestorID string                 `json:"requestor_id,omitempty"`
}

// Result represents the result of task execution.
type Result struct {
	Success bool
	Message string
	Data    map[string]interface{}
}

// Handler is a function that processes a task.
// It receives a context for cancellation and the task to process.
// It should return a Result indicating success/failure and any output data.
type Handler func(ctx context.Context, task Task, log zerolog.Logger) Result

// Registry manages task handlers for different task types.
type Registry struct {
	mu       sync.RWMutex
	handlers map[string]Handler
	fallback Handler
}

// New creates a new task handler registry.
func New() *Registry {
	return &Registry{
		handlers: make(map[string]Handler),
	}
}

// Register adds a handler for a specific task type.
// If a handler already exists for the task type, it will be replaced.
func (r *Registry) Register(taskType string, handler Handler) *Registry {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[taskType] = handler
	return r
}

// RegisterMultiple adds handlers for multiple task types at once.
func (r *Registry) RegisterMultiple(handlers map[string]Handler) *Registry {
	r.mu.Lock()
	defer r.mu.Unlock()
	for taskType, handler := range handlers {
		r.handlers[taskType] = handler
	}
	return r
}

// SetFallback sets a fallback handler for unknown task types.
// If not set, unknown task types will return an error.
func (r *Registry) SetFallback(handler Handler) *Registry {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fallback = handler
	return r
}

// Get returns the handler for a task type.
// Returns nil if no handler is registered and no fallback is set.
func (r *Registry) Get(taskType string) Handler {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if handler, ok := r.handlers[taskType]; ok {
		return handler
	}
	return r.fallback
}

// Has checks if a handler is registered for the given task type.
func (r *Registry) Has(taskType string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.handlers[taskType]
	return ok
}

// Execute runs the appropriate handler for the given task.
func (r *Registry) Execute(ctx context.Context, task Task, log zerolog.Logger) Result {
	handler := r.Get(task.Type)
	if handler == nil {
		return Result{
			Success: false,
			Message: fmt.Sprintf("no handler registered for task type: %s", task.Type),
		}
	}

	return handler(ctx, task, log)
}

// Types returns a list of all registered task types.
func (r *Registry) Types() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	types := make([]string, 0, len(r.handlers))
	for t := range r.handlers {
		types = append(types, t)
	}
	return types
}

// Clear removes all registered handlers.
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers = make(map[string]Handler)
	r.fallback = nil
}

// DefaultRegistry is the global registry instance.
var DefaultRegistry = New()

// Register adds a handler to the default registry.
func Register(taskType string, handler Handler) {
	DefaultRegistry.Register(taskType, handler)
}

// Execute runs a task using the default registry.
func Execute(ctx context.Context, task Task, log zerolog.Logger) Result {
	return DefaultRegistry.Execute(ctx, task, log)
}
