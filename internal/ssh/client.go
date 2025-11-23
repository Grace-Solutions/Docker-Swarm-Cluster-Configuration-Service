package ssh

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"time"

	"golang.org/x/crypto/ssh"
)

// Client wraps an SSH client connection for remote command execution.
type Client struct {
	client *ssh.Client
	host   string
}

// NewClient creates a new SSH client connection to the specified host.
// privateKeyPEM is the PEM-encoded private key.
// user is the SSH username (typically "root").
func NewClient(ctx context.Context, host string, user string, privateKeyPEM []byte) (*Client, error) {
	// Parse private key
	signer, err := ssh.ParsePrivateKey(privateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	// Configure SSH client
	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // TODO: Consider using known_hosts for production
		Timeout:         10 * time.Second,
	}

	// Add port if not present
	addr := host
	if _, _, err := net.SplitHostPort(host); err != nil {
		addr = net.JoinHostPort(host, "22")
	}

	// Dial with context support
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
	}

	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to dial %s: %w", addr, err)
	}

	// Perform SSH handshake
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to establish ssh connection to %s: %w", addr, err)
	}

	client := ssh.NewClient(sshConn, chans, reqs)

	return &Client{
		client: client,
		host:   host,
	}, nil
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

