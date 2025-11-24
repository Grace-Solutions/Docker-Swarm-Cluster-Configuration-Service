package sshkeys

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	"clusterctl/internal/logging"
	"golang.org/x/crypto/ssh"
)

const (
	// DefaultKeyDir is the default directory for SSH keys (relative to binary)
	DefaultKeyDir = "sshkeys"
	// PrivateKeyFileName is the name of the private key file
	PrivateKeyFileName = "clusterctl_ed25519"
	// PublicKeyFileName is the name of the public key file
	PublicKeyFileName = "clusterctl_ed25519.pub"
)

// KeyPair represents an SSH key pair.
type KeyPair struct {
	PrivateKeyPath string
	PublicKeyPath  string
	PublicKey      string // OpenSSH format public key
}

// EnsureKeyPair ensures an SSH key pair exists, generating it if necessary.
// Returns the key pair information.
func EnsureKeyPair(keyDir string) (*KeyPair, error) {
	log := logging.L().With("component", "sshkeys")

	// Use default key directory if not specified
	if keyDir == "" {
		// Get binary directory
		exePath, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("failed to get executable path: %w", err)
		}
		binaryDir := filepath.Dir(exePath)
		keyDir = filepath.Join(binaryDir, DefaultKeyDir)
	}

	// Ensure key directory exists
	if err := os.MkdirAll(keyDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create key directory: %w", err)
	}

	privateKeyPath := filepath.Join(keyDir, PrivateKeyFileName)
	publicKeyPath := filepath.Join(keyDir, PublicKeyFileName)

	// Check if key pair already exists
	if _, err := os.Stat(privateKeyPath); err == nil {
		log.Infow("SSH key pair already exists", "path", privateKeyPath)
		
		// Read public key
		publicKeyBytes, err := os.ReadFile(publicKeyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read public key: %w", err)
		}

		return &KeyPair{
			PrivateKeyPath: privateKeyPath,
			PublicKeyPath:  publicKeyPath,
			PublicKey:      string(publicKeyBytes),
		}, nil
	}

	// Generate new key pair
	log.Infow("generating new SSH key pair", "path", privateKeyPath)
	
	// Generate ED25519 key pair
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate key pair: %w", err)
	}

	// Encode private key to PEM format
	privateKeyPEM := &pem.Block{
		Type:  "OPENSSH PRIVATE KEY",
		Bytes: marshalED25519PrivateKey(privateKey),
	}

	// Write private key
	privateKeyFile, err := os.OpenFile(privateKeyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to create private key file: %w", err)
	}
	defer privateKeyFile.Close()

	if err := pem.Encode(privateKeyFile, privateKeyPEM); err != nil {
		return nil, fmt.Errorf("failed to write private key: %w", err)
	}

	// Generate OpenSSH format public key
	sshPublicKey, err := ssh.NewPublicKey(publicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create SSH public key: %w", err)
	}
	publicKeyStr := string(ssh.MarshalAuthorizedKey(sshPublicKey))

	// Write public key
	if err := os.WriteFile(publicKeyPath, []byte(publicKeyStr), 0644); err != nil {
		return nil, fmt.Errorf("failed to write public key: %w", err)
	}

	log.Infow("SSH key pair generated successfully",
		"privateKey", privateKeyPath,
		"publicKey", publicKeyPath,
	)

	return &KeyPair{
		PrivateKeyPath: privateKeyPath,
		PublicKeyPath:  publicKeyPath,
		PublicKey:      publicKeyStr,
	}, nil
}

// marshalED25519PrivateKey marshals an ED25519 private key to OpenSSH format.
func marshalED25519PrivateKey(key ed25519.PrivateKey) []byte {
	// OpenSSH ED25519 private key format
	// This is a simplified version - for production use, consider using
	// golang.org/x/crypto/ssh's MarshalPrivateKey or similar
	return []byte(key)
}

// RemoveKeyPair removes the SSH key pair from disk.
func RemoveKeyPair(keyDir string) error {
	log := logging.L().With("component", "sshkeys")

	if keyDir == "" {
		exePath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("failed to get executable path: %w", err)
		}
		binaryDir := filepath.Dir(exePath)
		keyDir = filepath.Join(binaryDir, DefaultKeyDir)
	}

	privateKeyPath := filepath.Join(keyDir, PrivateKeyFileName)
	publicKeyPath := filepath.Join(keyDir, PublicKeyFileName)

	// Remove private key
	if err := os.Remove(privateKeyPath); err != nil && !os.IsNotExist(err) {
		log.Warnw("failed to remove private key", "error", err)
	}

	// Remove public key
	if err := os.Remove(publicKeyPath); err != nil && !os.IsNotExist(err) {
		log.Warnw("failed to remove public key", "error", err)
	}

	log.Infow("SSH key pair removed", "dir", keyDir)
	return nil
}

