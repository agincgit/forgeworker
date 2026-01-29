// ForgeWorker is a distributed task worker that connects to TaskForge API.
// It supports Kubernetes/OpenShift deployments with environment variable configuration.
//
// Usage as a standalone application:
//
//	TASKFORGE_API_URL=https://api.example.com ./forgeworker
//
// Usage as a library:
//
//	import (
//	    "github.com/agincgit/forgeworker/config"
//	    "github.com/agincgit/forgeworker/registry"
//	    "github.com/agincgit/forgeworker/service"
//	)
//
//	cfg := config.MustLoad()
//	reg := registry.New()
//	reg.Register("my-task", myHandler)
//	worker := service.NewWorker(cfg, reg)
//	worker.Run(context.Background())
package main

import (
	"context"
	"os"
	"time"

	"github.com/agincgit/forgeworker/config"
	"github.com/agincgit/forgeworker/logger"
	"github.com/agincgit/forgeworker/registry"
	"github.com/agincgit/forgeworker/service"
	"github.com/rs/zerolog"
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
		Str("health_addr", cfg.HealthServerAddr).
		Bool("auth_enabled", cfg.HasAuth()).
		Msg("ForgeWorker starting")

	// Create task registry and register default handlers
	reg := registry.New()
	registerDefaultHandlers(reg)

	// Create and run worker
	worker := service.NewWorker(cfg, reg)
	if err := worker.Run(context.Background()); err != nil {
		log.Fatal().Err(err).Msg("Worker failed")
	}

	log.Info().Msg("ForgeWorker shutdown complete")
	os.Exit(0)
}

// registerDefaultHandlers registers the built-in task handlers.
// Users embedding ForgeWorker as a library can register their own handlers instead.
func registerDefaultHandlers(reg *registry.Registry) {
	// Data processing handler
	reg.Register("data-processing", func(ctx context.Context, task registry.Task, log zerolog.Logger) registry.Result {
		log.Debug().Msg("Processing data")

		// Simulate work with context awareness
		select {
		case <-ctx.Done():
			return registry.Result{
				Success: false,
				Message: "task cancelled",
			}
		case <-time.After(2 * time.Second):
		}

		return registry.Result{
			Success: true,
			Message: "Data processing completed",
		}
	})

	// File transfer handler
	reg.Register("file-transfer", func(ctx context.Context, task registry.Task, log zerolog.Logger) registry.Result {
		log.Debug().Msg("Transferring files")

		// Simulate work with context awareness
		select {
		case <-ctx.Done():
			return registry.Result{
				Success: false,
				Message: "task cancelled",
			}
		case <-time.After(3 * time.Second):
		}

		return registry.Result{
			Success: true,
			Message: "File transfer completed",
		}
	})

	// Set fallback for unknown task types
	reg.SetFallback(func(ctx context.Context, task registry.Task, log zerolog.Logger) registry.Result {
		log.Warn().Str("task_type", task.Type).Msg("Unknown task type")
		return registry.Result{
			Success: false,
			Message: "unsupported task type: " + task.Type,
		}
	})
}
