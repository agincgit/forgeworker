// ForgeWorker is a distributed task worker that connects to TaskForge API.
// It supports Kubernetes/OpenShift deployments with environment variable configuration.
package main

import (
	"context"
	"os"

	"github.com/agincgit/forgeworker/config"
	"github.com/agincgit/forgeworker/logger"
	"github.com/agincgit/forgeworker/service"
)

func main() {
	// Load configuration from environment variables
	cfg, err := config.Load()
	if err != nil {
		logger.Logger.Fatal().Err(err).Msg("Failed to load configuration")
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		logger.Logger.Fatal().Err(err).Msg("Invalid configuration")
	}

	// Setup logger with configured level and format
	logger.Setup(cfg.LogLevel, cfg.LogFormat)

	log := logger.WithComponent("main")
	log.Info().
		Str("api_url", cfg.TaskForgeAPIURL).
		Str("hostname", cfg.HostName).
		Str("worker_type", cfg.WorkerType).
		Str("log_level", cfg.LogLevel).
		Msg("ForgeWorker starting")

	// Register worker with TaskForge
	workerID, err := service.RegisterWorker(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("Worker registration failed")
	}

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start heartbeat scheduler
	service.StartHeartbeatScheduler(ctx, cfg, workerID)

	// Start worker loop (blocks until shutdown signal)
	service.StartWorkerLoop(ctx, cfg, workerID, cancel)

	log.Info().Msg("ForgeWorker shutdown complete")
	os.Exit(0)
}
