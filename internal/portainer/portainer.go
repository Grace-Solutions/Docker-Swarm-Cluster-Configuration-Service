package portainer

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"clusterctl/internal/ipdetect"
	"clusterctl/internal/logging"
)

const (
	portainerAgentImage = "portainer/agent:latest"
	portainerCEImage    = "portainer/portainer-ce:latest"
	portainerDataPath   = "/mnt/GlusterFS/Docker/Swarm/0001/data/Portainer"
)

// serviceExists checks if a Docker Swarm service with the given name exists.
func serviceExists(ctx context.Context, serviceName string) (bool, error) {
	cmd := exec.CommandContext(ctx, "docker", "service", "inspect", serviceName)
	err := cmd.Run()
	if err != nil {
		// If the service doesn't exist, docker service inspect returns an error.
		// We need to distinguish between "service not found" and other errors.
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Exit code 1 typically means service not found.
			if exitErr.ExitCode() == 1 {
				return false, nil
			}
		}
		// Some other error occurred.
		return false, err
	}
	// Service exists.
	return true, nil
}

// DeployPortainer deploys Portainer Agent as a global service and Portainer CE as a replicated service.
// This should only be called on a worker node after the swarm is initialized.
// It uses the existing Docker Swarm overlay networks (DOCKER-SWARM-INTERNAL and DOCKER-SWARM-EXTERNAL).
// If the services already exist (deployed by another worker), this function will skip deployment gracefully.
func DeployPortainer(ctx context.Context) error {
	log := logging.L()
	log.Infow("checking if Portainer deployment is needed")

	// Check if we're in a swarm.
	if err := exec.CommandContext(ctx, "docker", "info", "--format", "{{.Swarm.LocalNodeState}}").Run(); err != nil {
		return fmt.Errorf("portainer: not in a swarm or docker not available: %w", err)
	}

	// Check if both services already exist (another worker already deployed them).
	agentExists, err := serviceExists(ctx, "portainer_agent")
	if err != nil {
		return fmt.Errorf("portainer: failed to check if agent exists: %w", err)
	}

	ceExists, err := serviceExists(ctx, "portainer")
	if err != nil {
		return fmt.Errorf("portainer: failed to check if portainer CE exists: %w", err)
	}

	if agentExists && ceExists {
		log.Infow("portainer services already deployed by another worker, skipping deployment")
		return nil
	}

	log.Infow("deploying Portainer and Portainer Agent to Docker Swarm")

	// Deploy Portainer Agent as a global service.
	if err := deployPortainerAgent(ctx); err != nil {
		return fmt.Errorf("portainer: failed to deploy agent: %w", err)
	}

	// Deploy Portainer CE as a replicated service (replica=1).
	if err := deployPortainerCE(ctx); err != nil {
		return fmt.Errorf("portainer: failed to deploy portainer: %w", err)
	}

	log.Infow("portainer deployment completed successfully")
	return nil
}

// deployPortainerAgent deploys the Portainer Agent as a global service.
func deployPortainerAgent(ctx context.Context) error {
	log := logging.L()

	// Check if the service already exists (in case of race condition).
	exists, err := serviceExists(ctx, "portainer_agent")
	if err != nil {
		return fmt.Errorf("failed to check if portainer agent exists: %w", err)
	}
	if exists {
		log.Infow("portainer agent service already exists, skipping deployment")
		return nil
	}

	log.Infow("deploying portainer agent as global service")

	// Create the Portainer Agent service.
	args := []string{
		"service", "create",
		"--name", "portainer_agent",
		"--mode", "global",
		"--constraint", "node.platform.os==linux",
		"--network", "DOCKER-SWARM-INTERNAL",
		"--mount", "type=bind,src=/var/run/docker.sock,dst=/var/run/docker.sock",
		"--mount", "type=bind,src=/var/lib/docker/volumes,dst=/var/lib/docker/volumes",
		portainerAgentImage,
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		// Check if the error is because the service already exists (race condition).
		if strings.Contains(string(output), "already exists") || strings.Contains(err.Error(), "already exists") {
			log.Infow("portainer agent service already exists (race condition), skipping deployment")
			return nil
		}
		return fmt.Errorf("failed to create portainer agent service: %w, output: %s", err, string(output))
	}

	log.Infow("portainer agent service created successfully")
	return nil
}

// deployPortainerCE deploys Portainer CE as a replicated service with replica count of 1.
// Runs on worker nodes only.
func deployPortainerCE(ctx context.Context) error {
	log := logging.L()

	// Check if the service already exists (in case of race condition).
	exists, err := serviceExists(ctx, "portainer")
	if err != nil {
		return fmt.Errorf("failed to check if portainer CE exists: %w", err)
	}
	if exists {
		log.Infow("portainer service already exists, skipping deployment")
		return nil
	}

	log.Infow("deploying portainer CE as replicated service (replica=1, workers only)")

	// Ensure the data directory exists.
	mkdirCmd := exec.CommandContext(ctx, "mkdir", "-p", portainerDataPath)
	if err := mkdirCmd.Run(); err != nil {
		log.Warnw(fmt.Sprintf("failed to create portainer data directory (may already exist): %v", err))
	}

	// Detect the primary IP for logging purposes.
	// Priority: overlay (CGNAT) > private (RFC1918) > other non-loopback > loopback.
	primaryIP, err := ipdetect.DetectPrimary()
	primaryIPStr := "<node-ip>"
	if err != nil {
		log.Warnw(fmt.Sprintf("failed to detect primary IP: %v", err))
	} else {
		primaryIPStr = primaryIP.String()
	}

	// Create the Portainer CE service.
	// Use mode=ingress (default) to enable routing mesh - accessible on any node.
	// This provides automatic failover: if Portainer moves to another worker, clients don't need to change IPs.
	args := []string{
		"service", "create",
		"--name", "portainer",
		"--replicas", "1",
		"--constraint", "node.platform.os==linux",
		"--constraint", "node.role==worker",
		"--network", "DOCKER-SWARM-INTERNAL",
		"--network", "DOCKER-SWARM-EXTERNAL",
		"--publish", "published=9443,target=9443,protocol=tcp",
		"--publish", "published=8000,target=8000,protocol=tcp",
		"--mount", fmt.Sprintf("type=bind,src=%s,dst=/data", portainerDataPath),
		portainerCEImage,
		"-H", "tcp://tasks.portainer_agent:9001",
		"--tlsskipverify",
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		// Check if the error is because the service already exists (race condition).
		if strings.Contains(string(output), "already exists") || strings.Contains(err.Error(), "already exists") {
			log.Infow("portainer service already exists (race condition), skipping deployment")
			return nil
		}
		return fmt.Errorf("failed to create portainer service: %w, output: %s", err, string(output))
	}

	log.Infow(fmt.Sprintf("portainer service created successfully: accessible at https://<any-node-ip>:9443 (routing mesh enabled), data stored at %s", portainerDataPath))
	log.Infow(fmt.Sprintf("example: https://%s:9443", primaryIPStr))
	return nil
}

