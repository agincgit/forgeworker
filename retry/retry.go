// Package retry provides retry logic with exponential backoff.
// It's designed for use with HTTP requests and other operations
// that may fail transiently.
package retry

import (
	"context"
	"math"
	"math/rand"
	"time"

	"github.com/rs/zerolog"
)

// Config holds retry configuration.
type Config struct {
	// MaxAttempts is the maximum number of attempts (including the first one).
	MaxAttempts int

	// InitialDelay is the delay before the first retry.
	InitialDelay time.Duration

	// MaxDelay is the maximum delay between retries.
	MaxDelay time.Duration

	// Multiplier is the factor by which the delay increases after each retry.
	Multiplier float64

	// Jitter adds randomness to delays to prevent thundering herd.
	// Value between 0 and 1, where 0.1 means ±10% jitter.
	Jitter float64
}

// DefaultConfig returns a sensible default retry configuration.
func DefaultConfig() Config {
	return Config{
		MaxAttempts:  5,
		InitialDelay: 1 * time.Second,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
		Jitter:       0.1,
	}
}

// Operation is a function that can be retried.
// It should return nil on success, or an error to trigger a retry.
type Operation func(ctx context.Context) error

// RetryableError wraps an error and indicates whether it's retryable.
type RetryableError struct {
	Err       error
	Retryable bool
}

func (e *RetryableError) Error() string {
	return e.Err.Error()
}

func (e *RetryableError) Unwrap() error {
	return e.Err
}

// Retryable wraps an error as retryable.
func Retryable(err error) error {
	return &RetryableError{Err: err, Retryable: true}
}

// NotRetryable wraps an error as not retryable.
func NotRetryable(err error) error {
	return &RetryableError{Err: err, Retryable: false}
}

// IsRetryable checks if an error should be retried.
// By default, all errors are retryable unless wrapped with NotRetryable.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	if re, ok := err.(*RetryableError); ok {
		return re.Retryable
	}
	return true // Default: retry all errors
}

// Do executes the operation with retries according to the config.
func Do(ctx context.Context, cfg Config, op Operation, log zerolog.Logger) error {
	var lastErr error

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		// Check context before attempting
		if ctx.Err() != nil {
			return ctx.Err()
		}

		lastErr = op(ctx)
		if lastErr == nil {
			return nil
		}

		// Check if error is retryable
		if !IsRetryable(lastErr) {
			log.Debug().
				Err(lastErr).
				Int("attempt", attempt).
				Msg("Non-retryable error, stopping retries")
			return lastErr
		}

		// Don't sleep after the last attempt
		if attempt == cfg.MaxAttempts {
			break
		}

		delay := calculateDelay(cfg, attempt)
		log.Debug().
			Err(lastErr).
			Int("attempt", attempt).
			Int("max_attempts", cfg.MaxAttempts).
			Dur("next_delay", delay).
			Msg("Operation failed, retrying")

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}

	log.Warn().
		Err(lastErr).
		Int("attempts", cfg.MaxAttempts).
		Msg("All retry attempts exhausted")

	return lastErr
}

// DoWithDefault executes the operation with default retry config.
func DoWithDefault(ctx context.Context, op Operation, log zerolog.Logger) error {
	return Do(ctx, DefaultConfig(), op, log)
}

// calculateDelay computes the delay for a given attempt with jitter.
func calculateDelay(cfg Config, attempt int) time.Duration {
	// Exponential backoff: delay = initialDelay * multiplier^(attempt-1)
	delay := float64(cfg.InitialDelay) * math.Pow(cfg.Multiplier, float64(attempt-1))

	// Cap at max delay
	if delay > float64(cfg.MaxDelay) {
		delay = float64(cfg.MaxDelay)
	}

	// Add jitter
	if cfg.Jitter > 0 {
		jitterRange := delay * cfg.Jitter
		delay = delay - jitterRange + (rand.Float64() * 2 * jitterRange)
	}

	return time.Duration(delay)
}

func init() {
	rand.Seed(time.Now().UnixNano())
}
