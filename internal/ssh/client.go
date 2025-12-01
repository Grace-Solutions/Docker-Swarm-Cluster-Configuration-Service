package ssh

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"clusterctl/internal/retry"
)

// Client wraps an SSH client connection for remote command execution.
type Client struct {
	client *ssh.Client
	host   string
}

// AuthConfig contains SSH authentication configuration.
type AuthConfig struct {
	Username           string
	Password           string
	PrivateKeyPEM      []byte
	PrivateKeyPath     string
	PrivateKeyPassword string // Password for encrypted private key (optional)
	Port               int    // SSH port (default: 22)
}

// NewClient creates a new SSH client connection to the specified host using the provided authentication.
func NewClient(ctx context.Context, host string, auth AuthConfig) (*Client, error) {
	var authMethods []ssh.AuthMethod

	// Try private key authentication first
	if len(auth.PrivateKeyPEM) > 0 {
		var signer ssh.Signer
		var err error

		if auth.PrivateKeyPassword != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(auth.PrivateKeyPEM, []byte(auth.PrivateKeyPassword))
		} else {
			signer, err = ssh.ParsePrivateKey(auth.PrivateKeyPEM)
		}

		if err != nil {
			return nil, fmt.Errorf("failed to parse private key: %w", err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	} else if auth.PrivateKeyPath != "" {
		keyData, err := os.ReadFile(auth.PrivateKeyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read private key from %s: %w", auth.PrivateKeyPath, err)
		}

		var signer ssh.Signer
		if auth.PrivateKeyPassword != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(keyData, []byte(auth.PrivateKeyPassword))
		} else {
			signer, err = ssh.ParsePrivateKey(keyData)
		}

		if err != nil {
			return nil, fmt.Errorf("failed to parse private key from %s: %w", auth.PrivateKeyPath, err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}

	// Add password authentication if provided
	if auth.Password != "" {
		authMethods = append(authMethods, ssh.Password(auth.Password))
	}

	if len(authMethods) == 0 {
		return nil, fmt.Errorf("no authentication method provided (need password or private key)")
	}

	// Configure SSH client
	config := &ssh.ClientConfig{
		User:            auth.Username,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // TODO: Consider using known_hosts for production
		Timeout:         10 * time.Second,
	}

	// Add port if not present
	addr := host
	if _, _, err := net.SplitHostPort(host); err != nil {
		port := "22"
		if auth.Port > 0 {
			port = fmt.Sprintf("%d", auth.Port)
		}
		addr = net.JoinHostPort(host, port)
	}

	// Dial with retry logic for transient network issues
	retryCfg := retry.SSHConfig(fmt.Sprintf("ssh-connect-%s", host))

	var client *ssh.Client
	err := retry.Do(ctx, retryCfg, func() error {
		dialer := &net.Dialer{
			Timeout: 10 * time.Second,
		}

		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			// Retry on connection refused, timeout, and network errors
			if isRetryableNetworkError(err) {
				return fmt.Errorf("failed to dial %s: %w", addr, err)
			}
			// Non-retryable error (e.g., invalid address)
			return fmt.Errorf("failed to dial %s (non-retryable): %w", addr, err)
		}

		// Perform SSH handshake
		sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
		if err != nil {
			conn.Close()
			// Retry on authentication and handshake failures (key might not be installed yet)
			if isRetryableSSHError(err) {
				return fmt.Errorf("failed to establish ssh connection to %s: %w", addr, err)
			}
			// Non-retryable error
			return fmt.Errorf("failed to establish ssh connection to %s (non-retryable): %w", addr, err)
		}

		client = ssh.NewClient(sshConn, chans, reqs)
		return nil
	})

	if err != nil {
		return nil, err
	}

	return &Client{
		client: client,
		host:   host,
	}, nil
}

// isRetryableNetworkError checks if a network error is transient and should be retried.
func isRetryableNetworkError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "temporary failure") ||
		strings.Contains(errStr, "network is unreachable") ||
		strings.Contains(errStr, "no route to host")
}

// isRetryableSSHError checks if an SSH error is transient and should be retried.
func isRetryableSSHError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "unable to authenticate") ||
		strings.Contains(errStr, "handshake failed") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "broken pipe")
}

// Run executes a command on the remote host and returns stdout, stderr, and error.
func (c *Client) Run(ctx context.Context, command string) (stdout, stderr string, err error) {
	session, err := c.client.NewSession()
	if err != nil {
		return "", "", fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	// Run command with context support
	errChan := make(chan error, 1)
	go func() {
		errChan <- session.Run(command)
	}()

	select {
	case <-ctx.Done():
		session.Signal(ssh.SIGKILL)
		return "", "", ctx.Err()
	case err := <-errChan:
		return stdoutBuf.String(), stderrBuf.String(), err
	}
}

// RunWithInput executes a command with stdin input.
func (c *Client) RunWithInput(ctx context.Context, command string, input io.Reader) (stdout, stderr string, err error) {
	session, err := c.client.NewSession()
	if err != nil {
		return "", "", fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf
	session.Stdin = input

	// Run command with context support
	errChan := make(chan error, 1)
	go func() {
		errChan <- session.Run(command)
	}()

	select {
	case <-ctx.Done():
		session.Signal(ssh.SIGKILL)
		return "", "", ctx.Err()
	case err := <-errChan:
		return stdoutBuf.String(), stderrBuf.String(), err
	}
}

// Close closes the SSH connection.
func (c *Client) Close() error {
	return c.client.Close()
}

// Host returns the hostname of the SSH connection.
func (c *Client) Host() string {
	return c.host
}

