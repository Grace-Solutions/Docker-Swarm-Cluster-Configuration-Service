package retry

import (
	"context"
	"fmt"
	"time"

	"clusterctl/internal/logging"
)

// Config defines retry behavior for operations.
type Config struct {
	MaxAttempts     int
	InitialBackoff  time.Duration
	MaxBackoff      time.Duration
	BackoffMultiple float64
	Operation       string // Description for logging
}

// DefaultConfig returns sensible defaults for most operations.
func DefaultConfig(operation string) Config {
	return Config{
		MaxAttempts:     5,
		InitialBackoff:  2 * time.Second,
		MaxBackoff:      30 * time.Second,
		BackoffMultiple: 2.0,
		Operation:       operation,
	}
}

// SSHConfig returns retry config optimized for SSH operations.
func SSHConfig(operation string) Config {
	return Config{
		MaxAttempts:     3,
		InitialBackoff:  1 * time.Second,
		MaxBackoff:      10 * time.Second,
		BackoffMultiple: 2.0,
		Operation:       operation,
	}
}

// PackageManagerConfig returns retry config for apt/yum operations.
func PackageManagerConfig(operation string) Config {
	return Config{
		MaxAttempts:     5,
		InitialBackoff:  3 * time.Second,
		MaxBackoff:      60 * time.Second,
		BackoffMultiple: 2.0,
		Operation:       operation,
	}
}

// NetworkConfig returns retry config for network operations.
func NetworkConfig(operation string) Config {
	return Config{
		MaxAttempts:     10,
		InitialBackoff:  2 * time.Second,
		MaxBackoff:      30 * time.Second,
		BackoffMultiple: 2.0,
		Operation:       operation,
	}
}

// Do executes the given function with retry logic and exponential backoff.
// Returns nil if the operation succeeds within MaxAttempts, otherwise returns the last error.
func Do(ctx context.Context, cfg Config, fn func() error) error {
	backoff := cfg.InitialBackoff
	log := logging.L()

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		err := fn()
		if err == nil {
			if attempt > 1 {
				log.Infow("operation succeeded after retry",
					"operation", cfg.Operation,
					"attempt", attempt,
					"totalAttempts", cfg.MaxAttempts)
			}
			return nil
		}

		if attempt < cfg.MaxAttempts {
			log.Warnw("operation failed, retrying",
				"operation", cfg.Operation,
				"attempt", attempt,
				"maxAttempts", cfg.MaxAttempts,
				"backoff", backoff,
				"err", err)

			select {
			case <-ctx.Done():
				return fmt.Errorf("%s: context cancelled after %d attempts: %w", cfg.Operation, attempt, ctx.Err())
			case <-time.After(backoff):
			}

			// Exponential backoff with cap
			backoff = time.Duration(float64(backoff) * cfg.BackoffMultiple)
			if backoff > cfg.MaxBackoff {
				backoff = cfg.MaxBackoff
			}
			continue
		}

		// Max attempts reached
		return fmt.Errorf("%s: failed after %d attempts: %w", cfg.Operation, attempt, err)
	}

	return fmt.Errorf("%s: unexpected retry loop exit", cfg.Operation)
}

// DoWithResult executes a function that returns a result and error, with retry logic.
func DoWithResult[T any](ctx context.Context, cfg Config, fn func() (T, error)) (T, error) {
	var result T
	backoff := cfg.InitialBackoff
	log := logging.L()

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		res, err := fn()
		if err == nil {
			if attempt > 1 {
				log.Infow("operation succeeded after retry",
					"operation", cfg.Operation,
					"attempt", attempt,
					"totalAttempts", cfg.MaxAttempts)
			}
			return res, nil
		}

		if attempt < cfg.MaxAttempts {
			log.Warnw("operation failed, retrying",
				"operation", cfg.Operation,
				"attempt", attempt,
				"maxAttempts", cfg.MaxAttempts,
				"backoff", backoff,
				"err", err)

			select {
			case <-ctx.Done():
				return result, fmt.Errorf("%s: context cancelled after %d attempts: %w", cfg.Operation, attempt, ctx.Err())
			case <-time.After(backoff):
			}

			// Exponential backoff with cap
			backoff = time.Duration(float64(backoff) * cfg.BackoffMultiple)
			if backoff > cfg.MaxBackoff {
				backoff = cfg.MaxBackoff
			}
			continue
		}

		// Max attempts reached
		return result, fmt.Errorf("%s: failed after %d attempts: %w", cfg.Operation, attempt, err)
	}

	return result, fmt.Errorf("%s: unexpected retry loop exit", cfg.Operation)
}

