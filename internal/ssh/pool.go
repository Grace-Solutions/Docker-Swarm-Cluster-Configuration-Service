package ssh

import (
	"context"
	"fmt"
	"sync"

	"dscotctl/internal/logging"
)

// Pool manages SSH connections to multiple hosts.
type Pool struct {
	authConfigs map[string]AuthConfig // Per-host authentication configs
	clients     map[string]*Client
	mu          sync.RWMutex
}

// NewPool creates a new SSH connection pool with per-host authentication configs.
func NewPool(authConfigs map[string]AuthConfig) *Pool {
	return &Pool{
		authConfigs: authConfigs,
		clients:     make(map[string]*Client),
	}
}

// Get returns an SSH client for the specified host, creating a new connection if needed.
func (p *Pool) Get(ctx context.Context, host string) (*Client, error) {
	p.mu.RLock()
	client, exists := p.clients[host]
	p.mu.RUnlock()

	if exists {
		return client, nil
	}

	// Create new connection
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check again in case another goroutine created it
	if client, exists := p.clients[host]; exists {
		return client, nil
	}

	// Get auth config for this host
	authConfig, exists := p.authConfigs[host]
	if !exists {
		return nil, fmt.Errorf("no authentication config found for host %s", host)
	}

	log := logging.L()
	log.Infow(fmt.Sprintf("→ [%s] establishing SSH connection", host))

	client, err := NewClient(ctx, host, authConfig)
	if err != nil {
		log.Errorw(fmt.Sprintf("✗ [%s] SSH connection failed", host), "error", err)
		return nil, fmt.Errorf("failed to create ssh client for %s: %w", host, err)
	}

	log.Infow(fmt.Sprintf("✓ [%s] SSH connection established", host))
	p.clients[host] = client
	return client, nil
}

// Run executes a command on the specified host.
func (p *Pool) Run(ctx context.Context, host, command string) (stdout, stderr string, err error) {
	client, err := p.Get(ctx, host)
	if err != nil {
		return "", "", err
	}

	return client.Run(ctx, command)
}

// RunAll executes a command on all specified hosts in parallel.
// Returns a map of host -> result.
type RunResult struct {
	Stdout string
	Stderr string
	Err    error
}

func (p *Pool) RunAll(ctx context.Context, hosts []string, command string) map[string]*RunResult {
	results := make(map[string]*RunResult)
	var mu sync.Mutex
	var wg sync.WaitGroup

	log := logging.L()

	for _, host := range hosts {
		wg.Add(1)
		go func(h string) {
			defer wg.Done()

			stdout, stderr, err := p.Run(ctx, h, command)

			mu.Lock()
			results[h] = &RunResult{
				Stdout: stdout,
				Stderr: stderr,
				Err:    err,
			}
			mu.Unlock()

			if err != nil {
				log.Errorw("ssh command failed", "host", h, "command", command, "err", err, "stderr", stderr)
			}
		}(host)
	}

	wg.Wait()
	return results
}

// Close closes all SSH connections in the pool.
func (p *Pool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var errs []error
	for host, client := range p.clients {
		if err := client.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close connection to %s: %w", host, err))
		}
	}

	p.clients = make(map[string]*Client)

	if len(errs) > 0 {
		return fmt.Errorf("errors closing connections: %v", errs)
	}

	return nil
}

