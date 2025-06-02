package handler

import (
    "bytes"
    "encoding/json"
    "fmt"
    "net/http"
    "time"

    log "github.com/sirupsen/logrus"
    "github.com/agincgit/forgeworker/config"
)

// Task represents a task retrieved from TaskForge.
type Task struct {
    ID          string    `json:"id"`
    Type        string    `json:"type"`
    Status      string    `json:"status"`
    CreatedAt   time.Time `json:"created_at"`
    RequestorID string    `json:"requestor_id"`
}

// FetchPendingTasks retrieves pending tasks assigned to this worker.
func FetchPendingTasks(cfg *config.Config, workerID string) ([]Task, error) {
    url := fmt.Sprintf("%s/tasks?worker_id=%s&status=pending", cfg.TaskForgeAPIURL, workerID)

    resp, err := http.Get(url)
    if err != nil {
        log.Errorf("Failed to fetch tasks: %v", err)
        return nil, err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        log.Errorf("Task fetching failed with status code: %d", resp.StatusCode)
        return nil, fmt.Errorf("failed to fetch tasks: %s", resp.Status)
    }

    var tasks []Task
    if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
        log.Errorf("Failed to decode task response: %v", err)
        return nil, err
    }

    log.Infof("Fetched %d pending tasks", len(tasks))
    return tasks, nil
}

// ExecuteTask processes a given task based on its type.
func ExecuteTask(task Task) error {
    log.Infof("Executing task ID: %s, Type: %s", task.ID, task.Type)

    switch task.Type {
    case "data-processing":
        log.Info("Processing data...")
        time.Sleep(2 * time.Second)
    case "file-transfer":
        log.Info("Transferring files...")
        time.Sleep(3 * time.Second)
    default:
        log.Warnf("Unknown task type: %s", task.Type)
        return fmt.Errorf("unsupported task type: %s", task.Type)
    }

    log.Infof("Task ID %s completed successfully", task.ID)
    return nil
}

// UpdateTaskStatus updates the task status in TaskForge.
func UpdateTaskStatus(cfg *config.Config, taskID string, status string, message string) error {
    url := fmt.Sprintf("%s/tasks/%s", cfg.TaskForgeAPIURL, taskID)

    payload := map[string]string{
        "status":  status,
        "message": message,
    }

    payloadBytes, err := json.Marshal(payload)
    if err != nil {
        log.Errorf("Failed to marshal task status update payload: %v", err)
        return err
    }

    req, err := http.NewRequest("PUT", url, bytes.NewBuffer(payloadBytes))
    if err != nil {
        log.Errorf("Failed to create task status update request: %v", err)
        return err
    }
    req.Header.Set("Content-Type", "application/json")

    client := &http.Client{Timeout: 10 * time.Second}
    resp, err := client.Do(req)
    if err != nil {
        log.Errorf("Failed to update task status: %v", err)
        return err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        log.Errorf("Task status update failed with status code: %d", resp.StatusCode)
        return fmt.Errorf("status update failed: %s", resp.Status)
    }

    log.Infof("Task ID %s status updated to %s", taskID, status)
    return nil
}
