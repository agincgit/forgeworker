// Package handler provides task handling functionality including
// fetching, execution, and status updates.
package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/agincgit/forgeworker/config"
	"github.com/agincgit/forgeworker/logger"
)

// Task represents a task retrieved from TaskForge.
type Task struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	RequestorID string    `json:"requestor_id"`
}

// TaskStatusUpdate represents the payload for updating task status.
type TaskStatusUpdate struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// FetchPendingTasks retrieves pending tasks assigned to this worker.
func FetchPendingTasks(cfg *config.Config, workerID string) ([]Task, error) {
	log := logger.WithComponent("task-fetcher")
	url := fmt.Sprintf("%s/tasks?worker_id=%s&status=pending", cfg.TaskForgeAPIURL, workerID)

	client := &http.Client{Timeout: cfg.HTTPTimeout}
	resp, err := client.Get(url)
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

// ExecuteTask processes a given task based on its type.
func ExecuteTask(task Task) error {
	log := logger.WithComponent("task-executor").With().
		Str("task_id", task.ID).
		Str("task_type", task.Type).
		Logger()

	log.Info().Msg("Executing task")

	switch task.Type {
	case "data-processing":
		log.Debug().Msg("Processing data")
		time.Sleep(2 * time.Second)
	case "file-transfer":
		log.Debug().Msg("Transferring files")
		time.Sleep(3 * time.Second)
	default:
		log.Warn().Msg("Unknown task type")
		return fmt.Errorf("unsupported task type: %s", task.Type)
	}

	log.Info().Msg("Task execution completed")
	return nil
}

// UpdateTaskStatus updates the task status in TaskForge.
func UpdateTaskStatus(cfg *config.Config, taskID string, status string, message string) error {
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

	req, err := http.NewRequest("PUT", url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		log.Error().Err(err).Msg("Failed to create task status update request")
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: cfg.HTTPTimeout}
	resp, err := client.Do(req)
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
