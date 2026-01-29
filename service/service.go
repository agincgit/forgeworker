// Package service provides the core worker functionality including
// registration, heartbeat management, and task execution loop.
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/agincgit/forgeworker/client"
	"github.com/agincgit/forgeworker/config"
	"github.com/agincgit/forgeworker/handler"
	"github.com/agincgit/forgeworker/health"
	"github.com/agincgit/forgeworker/logger"
	"github.com/agincgit/forgeworker/registry"
	"github.com/agincgit/forgeworker/retry"
	"github.com/rs/zerolog"
)

// WorkerRegistrationPayload represents the data sent to TaskForge for worker registration.
type WorkerRegistrationPayload struct {
	WorkerType string    `json:"worker_type"`
	HostName   string    `json:"host_name"`
	StartTime  time.Time `json:"start_time"`
}

// WorkerHeartbeatPayload represents the data sent for heartbeat updates.
type WorkerHeartbeatPayload struct {
	WorkerID string    `json:"worker_id"`
	LastPing time.Time `json:"last_ping"`
}

// Worker represents a ForgeWorker instance with all its components.
type Worker struct {
	cfg           *config.Config
	client        *client.Client
	registry      *registry.Registry
	healthChecker *health.Checker
	healthServer  *health.Server
	taskHandler   *handler.TaskHandler
	workerID      string
	log           zerolog.Logger

	// Graceful shutdown
	shutdownOnce sync.Once
	shutdownCh   chan struct{}
	taskWg       sync.WaitGroup
}

// NewWorker creates a new Worker instance.
func NewWorker(cfg *config.Config, reg *registry.Registry) *Worker {
	log := logger.WithComponent("worker")

	apiClient := client.New(client.Config{
		BaseURL: cfg.TaskForgeAPIURL,
		Token:   cfg.APIToken,
		Timeout: cfg.HTTPTimeout,
		RetryConfig: &retry.Config{
			MaxAttempts:  cfg.RetryMaxAttempts,
			InitialDelay: cfg.RetryInitialDelay,
			MaxDelay:     cfg.RetryMaxDelay,
			Multiplier:   2.0,
			Jitter:       0.1,
		},
	}, log)

	healthChecker := health.NewChecker()
	var healthServer *health.Server
	if cfg.HealthServerAddr != "" {
		healthServer = health.NewServer(healthChecker, cfg.HealthServerAddr, log)
	}

	return &Worker{
		cfg:           cfg,
		client:        apiClient,
		registry:      reg,
		healthChecker: healthChecker,
		healthServer:  healthServer,
		taskHandler:   handler.NewTaskHandler(cfg, reg),
		log:           log,
		shutdownCh:    make(chan struct{}),
	}
}

// Run starts the worker and blocks until shutdown.
func (w *Worker) Run(ctx context.Context) error {
	// Start health server if configured
	if w.healthServer != nil {
		w.healthServer.Start()
	}

	// Register with TaskForge
	workerID, err := w.register(ctx)
	if err != nil {
		return fmt.Errorf("registration failed: %w", err)
	}
	w.workerID = workerID
	w.healthChecker.SetWorkerID(workerID)

	// Create cancellable context for graceful shutdown
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Start heartbeat scheduler
	go w.heartbeatLoop(ctx)

	// Start task polling loop
	go w.taskLoop(ctx)

	// Wait for shutdown signal
	select {
	case sig := <-sigChan:
		w.log.Info().Str("signal", sig.String()).Msg("Received shutdown signal")
	case <-ctx.Done():
		w.log.Info().Msg("Context cancelled")
	}

	// Initiate graceful shutdown
	return w.shutdown(ctx, cancel)
}

// register registers the worker with TaskForge using retry logic.
func (w *Worker) register(ctx context.Context) (string, error) {
	log := w.log.With().Str("component", "registration").Logger()

	var workerID string

	err := retry.Do(ctx, retry.Config{
		MaxAttempts:  w.cfg.RetryMaxAttempts,
		InitialDelay: w.cfg.RetryInitialDelay,
		MaxDelay:     w.cfg.RetryMaxDelay,
		Multiplier:   2.0,
		Jitter:       0.1,
	}, func(ctx context.Context) error {
		payload := WorkerRegistrationPayload{
			WorkerType: w.cfg.WorkerType,
			HostName:   w.cfg.HostName,
			StartTime:  time.Now(),
		}

		resp, err := w.client.Post(ctx, "/workers", payload)
		if err != nil {
			return err
		}

		if resp.StatusCode != 201 {
			return fmt.Errorf("registration failed: status %d", resp.StatusCode)
		}

		var response struct {
			WorkerID string `json:"worker_id"`
		}
		if err := resp.DecodeJSON(&response); err != nil {
			return err
		}

		workerID = response.WorkerID
		return nil
	}, log)

	if err != nil {
		return "", err
	}

	log.Info().Str("worker_id", workerID).Msg("Worker successfully registered with TaskForge")
	return workerID, nil
}

// heartbeatLoop sends periodic heartbeats to TaskForge.
func (w *Worker) heartbeatLoop(ctx context.Context) {
	log := w.log.With().Str("component", "heartbeat").Str("worker_id", w.workerID).Logger()
	log.Info().Dur("interval", w.cfg.HeartbeatInterval).Msg("Starting heartbeat scheduler")

	ticker := time.NewTicker(w.cfg.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("Heartbeat scheduler stopped")
			return
		case <-ticker.C:
			if err := w.sendHeartbeat(ctx); err != nil {
				log.Error().Err(err).Msg("Heartbeat failed")
			} else {
				w.healthChecker.UpdateHeartbeat()
			}
		}
	}
}

// sendHeartbeat sends a single heartbeat to TaskForge.
func (w *Worker) sendHeartbeat(ctx context.Context) error {
	payload := WorkerHeartbeatPayload{
		WorkerID: w.workerID,
		LastPing: time.Now(),
	}

	path := fmt.Sprintf("/workers/%s/heartbeat", w.workerID)
	resp, err := w.client.Put(ctx, path, payload)
	if err != nil {
		return err
	}

	if !resp.IsSuccess() {
		return fmt.Errorf("heartbeat failed: status %d", resp.StatusCode)
	}

	return nil
}

// taskLoop polls for and executes tasks.
func (w *Worker) taskLoop(ctx context.Context) {
	log := w.log.With().Str("component", "task-loop").Str("worker_id", w.workerID).Logger()
	log.Info().Dur("poll_interval", w.cfg.PollInterval).Msg("Starting task loop")

	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("Task loop stopped")
			return
		case <-ticker.C:
			w.processPendingTasks(ctx, log)
		}
	}
}

// processPendingTasks fetches and processes pending tasks.
func (w *Worker) processPendingTasks(ctx context.Context, log zerolog.Logger) {
	// Check if we're shutting down
	if w.healthChecker.IsShuttingDown() {
		return
	}

	tasks, err := w.taskHandler.FetchPendingTasks(ctx, w.workerID)
	if err != nil {
		log.Error().Err(err).Msg("Error fetching tasks")
		return
	}

	for _, task := range tasks {
		// Check context before processing each task
		if ctx.Err() != nil {
			return
		}

		w.processTask(ctx, task, log)
	}
}

// processTask executes a single task.
func (w *Worker) processTask(ctx context.Context, task handler.Task, log zerolog.Logger) {
	taskLog := log.With().Str("task_id", task.ID).Str("task_type", task.Type).Logger()
	taskLog.Info().Msg("Processing task")

	// Track in-flight tasks for graceful shutdown
	w.healthChecker.IncrementTasksInFlight()
	w.taskWg.Add(1)
	defer func() {
		w.healthChecker.DecrementTasksInFlight()
		w.taskWg.Done()
	}()

	result, err := w.taskHandler.ExecuteTask(ctx, task)
	if err != nil {
		taskLog.Error().Err(err).Msg("Task execution error")
		w.taskHandler.UpdateTaskStatus(ctx, task.ID, "failed", err.Error(), nil)
		return
	}

	if result.Success {
		taskLog.Info().Msg("Task completed successfully")
		w.taskHandler.UpdateTaskStatus(ctx, task.ID, "completed", result.Message, result.Data)
	} else {
		taskLog.Warn().Str("message", result.Message).Msg("Task failed")
		w.taskHandler.UpdateTaskStatus(ctx, task.ID, "failed", result.Message, result.Data)
	}
}

// shutdown performs graceful shutdown.
func (w *Worker) shutdown(ctx context.Context, cancel context.CancelFunc) error {
	var shutdownErr error

	w.shutdownOnce.Do(func() {
		w.log.Info().Msg("Starting graceful shutdown")
		w.healthChecker.StartShutdown()

		// Cancel context to stop accepting new work
		cancel()

		// Wait for in-flight tasks with timeout
		done := make(chan struct{})
		go func() {
			w.taskWg.Wait()
			close(done)
		}()

		select {
		case <-done:
			w.log.Info().Msg("All in-flight tasks completed")
		case <-time.After(w.cfg.ShutdownTimeout):
			w.log.Warn().Msg("Shutdown timeout reached, some tasks may not have completed")
		}

		// Stop health server
		if w.healthServer != nil {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			if err := w.healthServer.Stop(shutdownCtx); err != nil {
				w.log.Error().Err(err).Msg("Error stopping health server")
			}
		}

		w.log.Info().Msg("Graceful shutdown complete")
	})

	return shutdownErr
}

// Legacy functions for backward compatibility

// RegisterWorker registers the worker with TaskForge.
func RegisterWorker(ctx context.Context, cfg *config.Config) (string, error) {
	log := logger.WithComponent("registration")

	apiClient := client.New(client.Config{
		BaseURL: cfg.TaskForgeAPIURL,
		Token:   cfg.APIToken,
		Timeout: cfg.HTTPTimeout,
	}, log)

	payload := WorkerRegistrationPayload{
		WorkerType: cfg.WorkerType,
		HostName:   cfg.HostName,
		StartTime:  time.Now(),
	}

	resp, err := apiClient.Post(ctx, "/workers", payload)
	if err != nil {
		log.Error().Err(err).Msg("Failed to register worker")
		return "", err
	}

	if resp.StatusCode != 201 {
		log.Error().Int("status_code", resp.StatusCode).Msg("Worker registration failed")
		return "", fmt.Errorf("registration failed: status %d", resp.StatusCode)
	}

	var response struct {
		WorkerID string `json:"worker_id"`
	}
	if err := json.Unmarshal(resp.Body, &response); err != nil {
		log.Error().Err(err).Msg("Failed to decode worker registration response")
		return "", err
	}

	log.Info().Str("worker_id", response.WorkerID).Msg("Worker successfully registered with TaskForge")
	return response.WorkerID, nil
}

// SendHeartbeat sends a periodic heartbeat to TaskForge.
func SendHeartbeat(ctx context.Context, cfg *config.Config, workerID string) error {
	log := logger.WithWorkerID(workerID)

	apiClient := client.New(client.Config{
		BaseURL: cfg.TaskForgeAPIURL,
		Token:   cfg.APIToken,
		Timeout: cfg.HTTPTimeout,
	}, log)

	payload := WorkerHeartbeatPayload{
		WorkerID: workerID,
		LastPing: time.Now(),
	}

	path := fmt.Sprintf("/workers/%s/heartbeat", workerID)
	resp, err := apiClient.Put(ctx, path, payload)
	if err != nil {
		log.Error().Err(err).Msg("Failed to send heartbeat")
		return err
	}

	if !resp.IsSuccess() {
		log.Error().Int("status_code", resp.StatusCode).Msg("Heartbeat failed")
		return fmt.Errorf("heartbeat failed: status %d", resp.StatusCode)
	}

	log.Debug().Msg("Heartbeat sent successfully")
	return nil
}

// StartHeartbeatScheduler starts a periodic task to send heartbeats.
func StartHeartbeatScheduler(ctx context.Context, cfg *config.Config, workerID string) {
	log := logger.WithWorkerID(workerID)
	log.Info().Dur("interval", cfg.HeartbeatInterval).Msg("Starting heartbeat scheduler")

	go func() {
		ticker := time.NewTicker(cfg.HeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				log.Info().Msg("Heartbeat scheduler stopped")
				return
			case <-ticker.C:
				if err := SendHeartbeat(ctx, cfg, workerID); err != nil {
					log.Error().Err(err).Msg("Heartbeat error")
				}
			}
		}
	}()
}

// StartWorkerLoop runs the worker loop to fetch, execute, and update task statuses.
func StartWorkerLoop(ctx context.Context, cfg *config.Config, workerID string, cancel context.CancelFunc) {
	log := logger.WithWorkerID(workerID)
	log.Info().Dur("poll_interval", cfg.PollInterval).Msg("Starting worker loop")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-sigChan:
			log.Warn().Msg("Received shutdown signal, stopping worker loop")
			cancel()
			return
		case <-ctx.Done():
			log.Info().Msg("Context canceled, stopping worker loop")
			return
		case <-ticker.C:
			executePendingTasks(ctx, cfg, workerID, log)
		}
	}
}

func executePendingTasks(ctx context.Context, cfg *config.Config, workerID string, log zerolog.Logger) {
	tasks, err := handler.FetchPendingTasksLegacy(ctx, cfg, workerID)
	if err != nil {
		log.Error().Err(err).Msg("Error fetching tasks")
		return
	}

	for _, task := range tasks {
		taskLog := log.With().Str("task_id", task.ID).Logger()
		taskLog.Info().Str("type", task.Type).Msg("Processing task")

		err := handler.ExecuteTaskLegacy(ctx, task)
		if err != nil {
			taskLog.Error().Err(err).Msg("Task failed")
			handler.UpdateTaskStatusLegacy(ctx, cfg, task.ID, "failed", err.Error())
		} else {
			taskLog.Info().Msg("Task completed successfully")
			handler.UpdateTaskStatusLegacy(ctx, cfg, task.ID, "completed", "Task executed successfully")
		}
	}
}
