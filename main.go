package main

import (
	"time"

	"github.com/agincgit/forgeworker/config"
	"github.com/agincgit/forgeworker/service"
	log "github.com/sirupsen/logrus"
)

func main() {
	cfg := config.GetConfig("config.json")

	workerID, err := service.RegisterWorker(cfg)
	if err != nil {
		log.Fatalf("Worker registration failed: %v", err)
	}

	service.StartHeartbeatScheduler(cfg, workerID, 30*time.Second)
	service.StartWorkerLoop(cfg, workerID)
}
