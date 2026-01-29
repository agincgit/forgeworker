// Package service provides the core worker functionality including
// registration, heartbeat management, and task execution loop.
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/agincgit/forgeworker/config"
	"github.com/agincgit/forgeworker/handler"
	"github.com/agincgit/forgeworker/logger"
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

// RegisterWorker registers the worker with TaskForge.
func RegisterWorker(cfg *config.Config) (string, error) {
	log := logger.WithComponent("registration")
	url := fmt.Sprintf("%s/workers", cfg.TaskForgeAPIURL)

	payload := WorkerRegistrationPayload{
		WorkerType: cfg.WorkerType,
		HostName:   cfg.HostName,
		StartTime:  time.Now(),
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal worker registration payload")
		return "", err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		log.Error().Err(err).Msg("Failed to create worker registration request")
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: cfg.HTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		log.Error().Err(err).Msg("Failed to register worker")
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		log.Error().Int("status_code", resp.StatusCode).Msg("Worker registration failed")
		return "", fmt.Errorf("registration failed: %s", resp.Status)
	}

	var response struct {
		WorkerID string `json:"worker_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		log.Error().Err(err).Msg("Failed to decode worker registration response")
		return "", err
	}

	log.Info().Str("worker_id", response.WorkerID).Msg("Worker successfully registered with TaskForge")
	return response.WorkerID, nil
}

// SendHeartbeat sends a periodic heartbeat to TaskForge to indicate worker is active.
func SendHeartbeat(cfg *config.Config, workerID string) error {
	log := logger.WithWorkerID(workerID)
	url := fmt.Sprintf("%s/workers/%s/heartbeat", cfg.TaskForgeAPIURL, workerID)

	payload := WorkerHeartbeatPayload{
		WorkerID: workerID,
		LastPing: time.Now(),
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal heartbeat payload")
		return err
	}

	req, err := http.NewRequest("PUT", url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		log.Error().Err(err).Msg("Failed to create heartbeat request")
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: cfg.HTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		log.Error().Err(err).Msg("Failed to send heartbeat")
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Error().Int("status_code", resp.StatusCode).Msg("Heartbeat failed")
		return fmt.Errorf("heartbeat failed: %s", resp.Status)
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
				if err := SendHeartbeat(cfg, workerID); err != nil {
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
			executePendingTasks(cfg, workerID, log)
		}
	}
}

func executePendingTasks(cfg *config.Config, workerID string, log zerolog.Logger) {
	tasks, err := handler.FetchPendingTasks(cfg, workerID)
	if err != nil {
		log.Error().Err(err).Msg("Error fetching tasks")
		return
	}

	for _, task := range tasks {
		taskLog := log.With().Str("task_id", task.ID).Logger()
		taskLog.Info().Str("type", task.Type).Msg("Processing task")

		err := handler.ExecuteTask(task)
		if err != nil {
			taskLog.Error().Err(err).Msg("Task failed")
			handler.UpdateTaskStatus(cfg, task.ID, "failed", err.Error())
		} else {
			taskLog.Info().Msg("Task completed successfully")
			handler.UpdateTaskStatus(cfg, task.ID, "completed", "Task executed successfully")
		}
	}
}
