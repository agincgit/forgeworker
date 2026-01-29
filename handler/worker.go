// Package handler provides task handling functionality including
// fetching, execution, and status updates.
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/agincgit/forgeworker/client"
	"github.com/agincgit/forgeworker/config"
	"github.com/agincgit/forgeworker/logger"
	"github.com/agincgit/forgeworker/registry"
	"github.com/agincgit/forgeworker/retry"
)

// Task represents a task retrieved from TaskForge.
type Task struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	Status      string                 `json:"status"`
	Payload     map[string]interface{} `json:"payload,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
	RequestorID string                 `json:"requestor_id"`
}

// TaskStatusUpdate represents the payload for updating task status.
type TaskStatusUpdate struct {
	Status  string                 `json:"status"`
	Message string                 `json:"message"`
	Result  map[string]interface{} `json:"result,omitempty"`
}

// TaskHandler manages task operations with the TaskForge API.
type TaskHandler struct {
	client   *client.Client
	registry *registry.Registry
	cfg      *config.Config
}

// NewTaskHandler creates a new task handler.
func NewTaskHandler(cfg *config.Config, reg *registry.Registry) *TaskHandler {
	log := logger.WithComponent("task-handler")

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

	return &TaskHandler{
		client:   apiClient,
		registry: reg,
		cfg:      cfg,
	}
}

// FetchPendingTasks retrieves pending tasks assigned to this worker.
func (h *TaskHandler) FetchPendingTasks(ctx context.Context, workerID string) ([]Task, error) {
	log := logger.WithComponent("task-fetcher").With().Str("worker_id", workerID).Logger()

	path := fmt.Sprintf("/tasks?worker_id=%s&status=pending", workerID)
	resp, err := h.client.Get(ctx, path)
	if err != nil {
		log.Error().Err(err).Msg("Failed to fetch tasks")
		return nil, err
	}

	if !resp.IsSuccess() {
		log.Error().Int("status_code", resp.StatusCode).Msg("Task fetching failed")
		return nil, fmt.Errorf("failed to fetch tasks: status %d", resp.StatusCode)
	}

	var tasks []Task
	if err := resp.DecodeJSON(&tasks); err != nil {
		log.Error().Err(err).Msg("Failed to decode task response")
		return nil, err
	}

	log.Debug().Int("count", len(tasks)).Msg("Fetched pending tasks")
	return tasks, nil
}

// ExecuteTask processes a given task using the registry.
func (h *TaskHandler) ExecuteTask(ctx context.Context, task Task) (registry.Result, error) {
	log := logger.WithComponent("task-executor").With().
		Str("task_id", task.ID).
		Str("task_type", task.Type).
		Logger()

	log.Info().Msg("Executing task")

	// Convert to registry.Task
	regTask := registry.Task{
		ID:          task.ID,
		Type:        task.Type,
		Status:      task.Status,
		Payload:     task.Payload,
		RequestorID: task.RequestorID,
	}

	result := h.registry.Execute(ctx, regTask, log)

	if result.Success {
		log.Info().Msg("Task execution completed")
	} else {
		log.Warn().Str("message", result.Message).Msg("Task execution failed")
	}

	return result, nil
}

// UpdateTaskStatus updates the task status in TaskForge.
func (h *TaskHandler) UpdateTaskStatus(ctx context.Context, taskID string, status string, message string, result map[string]interface{}) error {
	log := logger.WithComponent("task-updater").With().
		Str("task_id", taskID).
		Str("status", status).
		Logger()

	path := fmt.Sprintf("/tasks/%s", taskID)
	payload := TaskStatusUpdate{
		Status:  status,
		Message: message,
		Result:  result,
	}

	resp, err := h.client.Put(ctx, path, payload)
	if err != nil {
		log.Error().Err(err).Msg("Failed to update task status")
		return err
	}

	if !resp.IsSuccess() {
		log.Error().Int("status_code", resp.StatusCode).Msg("Task status update failed")
		return fmt.Errorf("status update failed: status %d", resp.StatusCode)
	}

	log.Debug().Msg("Task status updated")
	return nil
}

// Legacy functions for backward compatibility
// These use the default HTTP client without retries

// FetchPendingTasksLegacy retrieves pending tasks (legacy, use TaskHandler instead).
func FetchPendingTasksLegacy(ctx context.Context, cfg *config.Config, workerID string) ([]Task, error) {
	log := logger.WithComponent("task-fetcher")
	url := fmt.Sprintf("%s/tasks?worker_id=%s&status=pending", cfg.TaskForgeAPIURL, workerID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	if cfg.APIToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIToken)
	}

	httpClient := &http.Client{Timeout: cfg.HTTPTimeout}
	resp, err := httpClient.Do(req)
	if err != nil {
		log.Error().Err(err).Str("worker_id", workerID).Msg("Failed to fetch tasks")
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Error().
			Int("status_code", resp.StatusCode).
			Str("worker_id", workerID).
			Msg("Task fetching failed")
		return nil, fmt.Errorf("failed to fetch tasks: %s", resp.Status)
	}

	var tasks []Task
	if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
		log.Error().Err(err).Msg("Failed to decode task response")
		return nil, err
	}

	log.Debug().
		Int("count", len(tasks)).
		Str("worker_id", workerID).
		Msg("Fetched pending tasks")
	return tasks, nil
}

// ExecuteTaskLegacy processes a task (legacy, use TaskHandler with registry instead).
func ExecuteTaskLegacy(ctx context.Context, task Task) error {
	log := logger.WithComponent("task-executor").With().
		Str("task_id", task.ID).
		Str("task_type", task.Type).
		Logger()

	log.Info().Msg("Executing task")

	// Check for context cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	switch task.Type {
	case "data-processing":
		log.Debug().Msg("Processing data")
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	case "file-transfer":
		log.Debug().Msg("Transferring files")
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	default:
		log.Warn().Msg("Unknown task type")
		return fmt.Errorf("unsupported task type: %s", task.Type)
	}

	log.Info().Msg("Task execution completed")
	return nil
}

// UpdateTaskStatusLegacy updates task status (legacy, use TaskHandler instead).
func UpdateTaskStatusLegacy(ctx context.Context, cfg *config.Config, taskID string, status string, message string) error {
	log := logger.WithComponent("task-updater").With().
		Str("task_id", taskID).
		Str("status", status).
		Logger()

	url := fmt.Sprintf("%s/tasks/%s", cfg.TaskForgeAPIURL, taskID)

	payload := TaskStatusUpdate{
		Status:  status,
		Message: message,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal task status update payload")
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		log.Error().Err(err).Msg("Failed to create task status update request")
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.APIToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIToken)
	}

	httpClient := &http.Client{Timeout: cfg.HTTPTimeout}
	resp, err := httpClient.Do(req)
	if err != nil {
		log.Error().Err(err).Msg("Failed to update task status")
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Error().Int("status_code", resp.StatusCode).Msg("Task status update failed")
		return fmt.Errorf("status update failed: %s", resp.Status)
	}

	log.Debug().Msg("Task status updated")
	return nil
}
