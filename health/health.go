// Package health provides HTTP health check endpoints for Kubernetes
// liveness and readiness probes.
package health

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// Status represents the health status of the worker.
type Status struct {
	Healthy     bool      `json:"healthy"`
	Ready       bool      `json:"ready"`
	WorkerID    string    `json:"worker_id,omitempty"`
	Registered  bool      `json:"registered"`
	LastHeartbeat time.Time `json:"last_heartbeat,omitempty"`
	TasksInFlight int      `json:"tasks_in_flight"`
	Uptime      string    `json:"uptime"`
	Message     string    `json:"message,omitempty"`
}

// Checker tracks the health state of the worker.
type Checker struct {
	mu              sync.RWMutex
	workerID        string
	registered      bool
	lastHeartbeat   time.Time
	tasksInFlight   int
	startTime       time.Time
	shutdownStarted bool
}

// NewChecker creates a new health checker.
func NewChecker() *Checker {
	return &Checker{
		startTime: time.Now(),
	}
}

// SetWorkerID sets the worker ID after registration.
func (c *Checker) SetWorkerID(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.workerID = id
	c.registered = true
}

// UpdateHeartbeat records a successful heartbeat.
func (c *Checker) UpdateHeartbeat() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastHeartbeat = time.Now()
}

// IncrementTasksInFlight increments the in-flight task counter.
func (c *Checker) IncrementTasksInFlight() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tasksInFlight++
}

// DecrementTasksInFlight decrements the in-flight task counter.
func (c *Checker) DecrementTasksInFlight() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tasksInFlight > 0 {
		c.tasksInFlight--
	}
}

// TasksInFlight returns the current number of in-flight tasks.
func (c *Checker) TasksInFlight() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.tasksInFlight
}

// StartShutdown marks the worker as shutting down.
func (c *Checker) StartShutdown() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.shutdownStarted = true
}

// IsShuttingDown returns true if shutdown has started.
func (c *Checker) IsShuttingDown() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.shutdownStarted
}

// GetStatus returns the current health status.
func (c *Checker) GetStatus() Status {
	c.mu.RLock()
	defer c.mu.RUnlock()

	status := Status{
		Healthy:       !c.shutdownStarted,
		Ready:         c.registered && !c.shutdownStarted,
		WorkerID:      c.workerID,
		Registered:    c.registered,
		LastHeartbeat: c.lastHeartbeat,
		TasksInFlight: c.tasksInFlight,
		Uptime:        time.Since(c.startTime).Round(time.Second).String(),
	}

	if c.shutdownStarted {
		status.Message = "shutdown in progress"
	} else if !c.registered {
		status.Message = "worker not yet registered"
	}

	return status
}

// Server provides HTTP endpoints for health checks.
type Server struct {
	checker *Checker
	server  *http.Server
	log     zerolog.Logger
}

// NewServer creates a new health check server.
func NewServer(checker *Checker, addr string, log zerolog.Logger) *Server {
	s := &Server{
		checker: checker,
		log:     log.With().Str("component", "health-server").Logger(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/readyz", s.handleReadyz)
	mux.HandleFunc("/status", s.handleStatus)

	s.server = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	return s
}

// Start starts the health check server in a goroutine.
func (s *Server) Start() {
	go func() {
		s.log.Info().Str("addr", s.server.Addr).Msg("Starting health check server")
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.log.Error().Err(err).Msg("Health check server error")
		}
	}()
}

// Stop gracefully stops the health check server.
func (s *Server) Stop(ctx context.Context) error {
	s.log.Info().Msg("Stopping health check server")
	return s.server.Shutdown(ctx)
}

// handleHealthz handles the liveness probe endpoint.
// Returns 200 if the worker is alive, 503 if shutting down.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	status := s.checker.GetStatus()

	w.Header().Set("Content-Type", "application/json")
	if status.Healthy {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"healthy": status.Healthy,
		"uptime":  status.Uptime,
	})
}

// handleReadyz handles the readiness probe endpoint.
// Returns 200 if the worker is ready to accept tasks.
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	status := s.checker.GetStatus()

	w.Header().Set("Content-Type", "application/json")
	if status.Ready {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"ready":      status.Ready,
		"registered": status.Registered,
		"message":    status.Message,
	})
}

// handleStatus handles the full status endpoint.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	status := s.checker.GetStatus()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(status)
}
