// Package config provides configuration management for ForgeWorker.
// Configuration is loaded from environment variables, making it suitable
// for Kubernetes/OpenShift deployments.
package config

import (
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

// Config holds application settings loaded from environment variables.
// This struct can be used by consumers who want to embed ForgeWorker
// as a library.
type Config struct {
	// LogLevel sets the logging verbosity (trace, debug, info, warn, error, fatal, panic)
	LogLevel string `env:"LOG_LEVEL" envDefault:"info"`

	// LogFormat sets the log output format (json, console)
	LogFormat string `env:"LOG_FORMAT" envDefault:"json"`

	// TaskForgeAPIURL is the base URL for the TaskForge API
	TaskForgeAPIURL string `env:"TASKFORGE_API_URL" envDefault:"https://api.taskforge.local"`

	// WorkerID allows overriding the auto-detected worker identifier
	// If not set, hostname will be used
	WorkerID string `env:"WORKER_ID"`

	// WorkerType identifies the type of worker for registration
	WorkerType string `env:"WORKER_TYPE" envDefault:"ForgeWorker"`

	// PollInterval is the duration between task polling cycles
	PollInterval time.Duration `env:"POLL_INTERVAL" envDefault:"10s"`

	// HeartbeatInterval is the duration between heartbeat signals
	HeartbeatInterval time.Duration `env:"HEARTBEAT_INTERVAL" envDefault:"30s"`

	// HTTPTimeout is the timeout for HTTP requests to TaskForge API
	HTTPTimeout time.Duration `env:"HTTP_TIMEOUT" envDefault:"10s"`

	// HostName is the detected or configured hostname of the worker
	// This is set automatically if WorkerID is not provided
	HostName string `env:"-"`
}

// Load parses environment variables and returns a Config instance.
// It automatically detects the hostname if WorkerID is not set.
func Load() (*Config, error) {
	cfg := &Config{}

	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config from environment: %w", err)
	}

	// Set hostname for worker identification
	cfg.HostName = cfg.WorkerID
	if cfg.HostName == "" {
		cfg.HostName = detectHostname()
	}

	return cfg, nil
}

// MustLoad is like Load but panics on error.
// Useful for application startup where config is required.
func MustLoad() *Config {
	cfg, err := Load()
	if err != nil {
		panic(fmt.Sprintf("failed to load config: %v", err))
	}
	return cfg
}

// NewWithDefaults creates a Config with default values without reading
// environment variables. Useful for testing or programmatic configuration.
func NewWithDefaults() *Config {
	return &Config{
		LogLevel:          "info",
		LogFormat:         "json",
		TaskForgeAPIURL:   "https://api.taskforge.local",
		WorkerType:        "ForgeWorker",
		PollInterval:      10 * time.Second,
		HeartbeatInterval: 30 * time.Second,
		HTTPTimeout:       10 * time.Second,
		HostName:          detectHostname(),
	}
}

// detectHostname attempts to determine the machine's hostname.
// Falls back to a random worker name if detection fails.
func detectHostname() string {
	hostName, err := os.Hostname()
	if err == nil && hostName != "" {
		return hostName
	}

	// Fallback to command execution if os.Hostname() fails
	out, cmdErr := exec.Command("hostname").Output()
	if cmdErr == nil {
		return strings.TrimSpace(string(out))
	}

	// Generate random worker name as last resort
	return generateWorkerName()
}

// generateWorkerName generates a random worker name.
func generateWorkerName() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("worker-%x", b)
}

// Validate checks if the configuration has all required fields set correctly.
func (c *Config) Validate() error {
	if c.TaskForgeAPIURL == "" {
		return fmt.Errorf("TASKFORGE_API_URL is required")
	}
	if c.HostName == "" {
		return fmt.Errorf("worker hostname could not be determined")
	}
	if c.PollInterval <= 0 {
		return fmt.Errorf("POLL_INTERVAL must be positive")
	}
	if c.HeartbeatInterval <= 0 {
		return fmt.Errorf("HEARTBEAT_INTERVAL must be positive")
	}
	return nil
}
