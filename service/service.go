package service

import (
    "bytes"
    "encoding/json"
    "fmt"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"

    log "github.com/sirupsen/logrus"
    "github.com/agincgit/forgeworker/config"
    "github.com/agincgit/forgeworker/handler"
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
    url := fmt.Sprintf("%s/workers", cfg.TaskForgeAPIURL)

    payload := WorkerRegistrationPayload{
        WorkerType: "ForgeWorker",
        HostName:   cfg.HostName,
        StartTime:  time.Now(),
    }

    payloadBytes, err := json.Marshal(payload)
    if err != nil {
        log.Errorf("Failed to marshal worker registration payload: %v", err)
        return "", err
    }

    req, err := http.NewRequest("POST", url, bytes.NewBuffer(payloadBytes))
    if err != nil {
        log.Errorf("Failed to create worker registration request: %v", err)
        return "", err
    }
    req.Header.Set("Content-Type", "application/json")

    client := &http.Client{Timeout: 10 * time.Second}
    resp, err := client.Do(req)
    if err != nil {
        log.Errorf("Failed to register worker: %v", err)
        return "", err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusCreated {
        log.Errorf("Worker registration failed with status code: %d", resp.StatusCode)
        return "", fmt.Errorf("registration failed: %s", resp.Status)
    }

    var response struct {
        WorkerID string `json:"worker_id"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
        log.Errorf("Failed to decode worker registration response: %v", err)
        return "", err
    }

    log.Infof("Worker successfully registered with TaskForge. WorkerID: %s", response.WorkerID)
    return response.WorkerID, nil
}

// SendHeartbeat sends a periodic heartbeat to TaskForge to indicate worker is active.
func SendHeartbeat(cfg *config.Config, workerID string) error {
    url := fmt.Sprintf("%s/workers/%s/heartbeat", cfg.TaskForgeAPIURL, workerID)

    payload := WorkerHeartbeatPayload{
        WorkerID: workerID,
        LastPing: time.Now(),
    }

    payloadBytes, err := json.Marshal(payload)
    if err != nil {
        log.Errorf("Failed to marshal heartbeat payload: %v", err)
        return err
    }

    req, err := http.NewRequest("PUT", url, bytes.NewBuffer(payloadBytes))
    if err != nil {
        log.Errorf("Failed to create heartbeat request: %v", err)
        return err
    }
    req.Header.Set("Content-Type", "application/json")

    client := &http.Client{Timeout: 10 * time.Second}
    resp, err := client.Do(req)
    if err != nil {
        log.Errorf("Failed to send heartbeat: %v", err)
        return err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        log.Errorf("Heartbeat failed with status code: %d", resp.StatusCode)
        return fmt.Errorf("heartbeat failed: %s", resp.Status)
    }

    log.Infof("Heartbeat successfully sent for WorkerID: %s", workerID)
    return nil
}

// StartHeartbeatScheduler starts a periodic task to send heartbeats.
func StartHeartbeatScheduler(cfg *config.Config, workerID string, interval time.Duration) {
    log.Infof("Starting heartbeat scheduler for WorkerID: %s", workerID)
    go func() {
        ticker := time.NewTicker(interval)
        defer ticker.Stop()
        for range ticker.C {
            if err := SendHeartbeat(cfg, workerID); err != nil {
                log.Errorf("Heartbeat error: %v", err)
            }
        }
    }()
}

// StartWorkerLoop runs the worker loop to fetch, execute, and update task statuses.
func StartWorkerLoop(cfg *config.Config, workerID string) {
    log.Info("Starting worker loop...")
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-sigChan:
            log.Warn("Received shutdown signal, stopping worker loop.")
            return
        case <-ticker.C:
            executePendingTasks(cfg, workerID)
        }
    }
}

func executePendingTasks(cfg *config.Config, workerID string) {
    tasks, err := handler.FetchPendingTasks(cfg, workerID)
    if err != nil {
        log.Errorf("Error fetching tasks: %v", err)
        return
    }

    for _, task := range tasks {
        log.Infof("Processing task ID: %s", task.ID)
        err := handler.ExecuteTask(task)
        if err != nil {
            log.Errorf("Task %s failed: %v", task.ID, err)
            handler.UpdateTaskStatus(cfg, task.ID, "failed", err.Error())
        } else {
            handler.UpdateTaskStatus(cfg, task.ID, "completed", "Task executed successfully")
        }
    }
}
