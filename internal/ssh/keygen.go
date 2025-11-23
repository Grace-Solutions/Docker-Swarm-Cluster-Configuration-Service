package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"

	"golang.org/x/crypto/ssh"
)

// KeyPair represents an SSH key pair.
type KeyPair struct {
	PrivateKey []byte // PEM-encoded private key
	PublicKey  []byte // OpenSSH authorized_keys format
}

// GenerateKeyPair generates a new ED25519 SSH key pair.
// ED25519 is preferred over RSA for better security and performance.
func GenerateKeyPair() (*KeyPair, error) {
	// Generate ED25519 key pair
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate ed25519 key: %w", err)
	}

	// Encode private key to PEM format
	privateKeyBytes, err := ssh.MarshalPrivateKey(privateKey, "")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal private key: %w", err)
	}

	privateKeyPEM := pem.EncodeToMemory(privateKeyBytes)

	// Convert public key to SSH authorized_keys format
	sshPublicKey, err := ssh.NewPublicKey(publicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create ssh public key: %w", err)
	}

	publicKeyBytes := ssh.MarshalAuthorizedKey(sshPublicKey)

	return &KeyPair{
		PrivateKey: privateKeyPEM,
		PublicKey:  publicKeyBytes,
	}, nil
}

