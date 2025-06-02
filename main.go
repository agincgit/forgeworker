package main

import (
    "context"
    "time"

    log "github.com/sirupsen/logrus"
    "github.com/agincgit/forgeworker/config"
    "github.com/agincgit/forgeworker/service"
)

func main() {
    cfg := config.GetConfig("config.json")

    workerID, err := service.RegisterWorker(cfg)
    if err != nil {
        log.Fatalf("Worker registration failed: %v", err)
    }

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    service.StartHeartbeatScheduler(ctx, cfg, workerID, 30*time.Second)
    service.StartWorkerLoop(ctx, cfg, workerID, cancel)
}
