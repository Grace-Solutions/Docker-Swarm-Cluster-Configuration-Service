package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"strings"

	"clusterctl/internal/config"
	"clusterctl/internal/defaults"
	"clusterctl/internal/logging"
	"clusterctl/internal/ssh"
)

// KeepalivedNodeConfig holds the resolved configuration for a single node.
type KeepalivedNodeConfig struct {
	Hostname  string // SSH hostname for this node
	Priority  int    // VRRP priority (1-254)
	State     string // "MASTER" or "BACKUP"
	Interface string // Network interface for VRRP
	VIP       string // Virtual IP address with CIDR
}

// KeepalivedDeployment holds the complete Keepalived deployment configuration.
type KeepalivedDeployment struct {
	Enabled   bool                    // Whether Keepalived is enabled
	VIP       string                  // Virtual IP address (without CIDR)
	VIPCIDR   string                  // Virtual IP address with CIDR (e.g., 192.168.1.250/24)
	Interface string                  // Network interface for VRRP
	RouterID  int                     // VRRP router ID
	AuthPass  string                  // VRRP authentication password
	Nodes     []*KeepalivedNodeConfig // Per-node configurations
}

// RFC1918 private network ranges
var rfc1918Networks = []string{
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
}

// PrepareKeepalivedDeployment prepares the Keepalived configuration for all nodes.
// This must be called after Swarm setup and before service deployment.
func PrepareKeepalivedDeployment(ctx context.Context, sshPool *ssh.Pool, cfg *config.Config) (*KeepalivedDeployment, error) {
	log := logging.L().With("component", "keepalived")

	if !cfg.IsKeepalivedEnabled() {
		log.Infow("Keepalived is not enabled globally, skipping")
		return &KeepalivedDeployment{Enabled: false}, nil
	}

	keepalivedNodes := cfg.GetKeepalivedNodes()
	if len(keepalivedNodes) == 0 {
		log.Infow("no nodes have Keepalived enabled, skipping")
		return &KeepalivedDeployment{Enabled: false}, nil
	}

	log.Infow("preparing Keepalived deployment", "nodeCount", len(keepalivedNodes))

	globalKA := cfg.GetKeepalived()

	// Use first enabled node to detect interface and VIP
	firstNode := keepalivedNodes[0].SSHFQDNorIP

	// Detect or use configured interface
	iface := globalKA.Interface
	if config.IsAutoValue(iface) || iface == "" {
		detected, err := detectRFC1918Interface(ctx, sshPool, firstNode)
		if err != nil {
			return nil, fmt.Errorf("failed to auto-detect RFC1918 interface: %w", err)
		}
		iface = detected
		log.Infow("auto-detected RFC1918 interface", "interface", iface, "node", firstNode)
	}

	// Get interface details (IP and netmask)
	ifaceIP, ifaceCIDR, err := getInterfaceDetails(ctx, sshPool, firstNode, iface)
	if err != nil {
		return nil, fmt.Errorf("failed to get interface details: %w", err)
	}
	log.Infow("interface details", "interface", iface, "ip", ifaceIP, "cidr", ifaceCIDR)

	// Detect or use configured VIP
	vip := globalKA.VIP
	if config.IsAutoValue(vip) || vip == "" {
		detected, err := findUnusedVIP(ctx, sshPool, firstNode, ifaceIP, ifaceCIDR)
		if err != nil {
			return nil, fmt.Errorf("failed to auto-detect unused VIP: %w", err)
		}
		vip = detected
		log.Infow("auto-detected unused VIP", "vip", vip)
	}

	// Generate or use configured auth password
	authPass := globalKA.AuthPass
	if config.IsAutoValue(authPass) || authPass == "" {
		authPass = generateAuthPassword()
		log.Infow("generated Keepalived auth password", "password", authPass)
	}

	// Use default or configured router ID
	routerID := globalKA.RouterID
	if routerID == 0 {
		routerID = defaults.KeepalivedRouterID
	}

	// Build per-node configurations
	deployment := &KeepalivedDeployment{
		Enabled:   true,
		VIP:       vip,
		VIPCIDR:   fmt.Sprintf("%s/%s", vip, extractCIDRPrefix(ifaceCIDR)),
		Interface: iface,
		RouterID:  routerID,
		AuthPass:  authPass,
		Nodes:     make([]*KeepalivedNodeConfig, 0, len(keepalivedNodes)),
	}

	for i, node := range keepalivedNodes {
		nodeConfig := resolveNodeConfig(node, i, iface, deployment.VIPCIDR)
		deployment.Nodes = append(deployment.Nodes, nodeConfig)
		log.Infow("resolved node configuration",
			"hostname", nodeConfig.Hostname,
			"priority", nodeConfig.Priority,
			"state", nodeConfig.State,
		)
	}

	// Log complete configuration
	log.Infow("Keepalived deployment prepared",
		"vip", deployment.VIPCIDR,
		"interface", deployment.Interface,
		"routerId", deployment.RouterID,
		"authPass", deployment.AuthPass,
		"nodeCount", len(deployment.Nodes),
	)

	return deployment, nil
}

// resolveNodeConfig resolves the per-node configuration with auto-values.
func resolveNodeConfig(node config.NodeConfig, nodeIndex int, iface, vipCIDR string) *KeepalivedNodeConfig {
	// Resolve priority
	priority := defaults.KeepalivedBasePriority - nodeIndex
	if !config.IsAutoValue(node.Keepalived.Priority) && node.Keepalived.Priority != "" {
		if p, err := strconv.Atoi(node.Keepalived.Priority); err == nil {
			priority = p
		}
	}

	// Resolve state
	state := "BACKUP"
	if nodeIndex == 0 {
		state = "MASTER"
	}
	if !config.IsAutoValue(node.Keepalived.State) && node.Keepalived.State != "" {
		state = strings.ToUpper(node.Keepalived.State)
	}

	return &KeepalivedNodeConfig{
		Hostname:  node.SSHFQDNorIP,
		Priority:  priority,
		State:     state,
		Interface: iface,
		VIP:       vipCIDR,
	}
}

// detectRFC1918Interface finds the first network interface with an RFC1918 IP address.
func detectRFC1918Interface(ctx context.Context, sshPool *ssh.Pool, host string) (string, error) {
	// Get all interfaces with their IPs
	cmd := `ip -o -4 addr show | awk '{print $2, $4}' | grep -v '^lo '`
	stdout, stderr, err := sshPool.Run(ctx, host, cmd)
	if err != nil {
		return "", fmt.Errorf("failed to list interfaces: %w (stderr: %s)", err, stderr)
	}

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		iface := parts[0]
		ipCIDR := parts[1]

		// Skip Docker and overlay interfaces
		if strings.HasPrefix(iface, "docker") || strings.HasPrefix(iface, "br-") ||
			strings.HasPrefix(iface, "veth") || strings.HasPrefix(iface, "wg") {
			continue
		}

		// Check if IP is RFC1918
		ip, _, err := net.ParseCIDR(ipCIDR)
		if err != nil {
			continue
		}

		if isRFC1918(ip) {
			return iface, nil
		}
	}

	return "", fmt.Errorf("no RFC1918 interface found")
}

// getInterfaceDetails returns the IP address and CIDR prefix for an interface.
func getInterfaceDetails(ctx context.Context, sshPool *ssh.Pool, host, iface string) (string, string, error) {
	cmd := fmt.Sprintf(`ip -o -4 addr show %s | awk '{print $4}'`, iface)
	stdout, stderr, err := sshPool.Run(ctx, host, cmd)
	if err != nil {
		return "", "", fmt.Errorf("failed to get interface details: %w (stderr: %s)", err, stderr)
	}

	ipCIDR := strings.TrimSpace(stdout)
	if ipCIDR == "" {
		return "", "", fmt.Errorf("no IP address found on interface %s", iface)
	}

	ip, ipNet, err := net.ParseCIDR(ipCIDR)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse IP/CIDR %s: %w", ipCIDR, err)
	}

	ones, _ := ipNet.Mask.Size()
	return ip.String(), fmt.Sprintf("%d", ones), nil
}

// findUnusedVIP finds an unused IP address in the subnet using ARP scanning.
func findUnusedVIP(ctx context.Context, sshPool *ssh.Pool, host, ifaceIP, cidrPrefix string) (string, error) {
	log := logging.L().With("component", "keepalived")

	// Parse the interface IP to get the network
	ip := net.ParseIP(ifaceIP)
	if ip == nil {
		return "", fmt.Errorf("invalid interface IP: %s", ifaceIP)
	}

	prefix, _ := strconv.Atoi(cidrPrefix)
	mask := net.CIDRMask(prefix, 32)
	network := ip.Mask(mask)

	// Calculate broadcast address
	broadcast := make(net.IP, len(network))
	for i := range network {
		broadcast[i] = network[i] | ^mask[i]
	}

	// Try IPs from the high end of the range (.254, .253, .252, etc.)
	// Skip .255 (broadcast) and try up to 10 addresses
	candidateIPs := []string{}
	for i := 254; i >= 245; i-- {
		candidateIP := net.IPv4(network[0], network[1], network[2], byte(i))
		// Skip if it matches the interface IP
		if candidateIP.String() == ifaceIP {
			continue
		}
		candidateIPs = append(candidateIPs, candidateIP.String())
	}

	// Ensure arping is installed
	installCmd := "command -v arping || apt-get update && apt-get install -y arping iputils-arping 2>/dev/null || yum install -y arping 2>/dev/null || true"
	sshPool.Run(ctx, host, installCmd)

	// Try each candidate IP with arping
	for _, candidate := range candidateIPs {
		// arping -c 2 -w 1 -D <ip> returns 0 if IP is in use, 1 if unused
		// Using -D (duplicate address detection mode)
		arpCmd := fmt.Sprintf("arping -c 2 -w 1 -D -I $(ip route get %s | grep -oP 'dev \\K\\S+') %s", candidate, candidate)
		_, _, err := sshPool.Run(ctx, host, arpCmd)
		if err != nil {
			// arping returned non-zero, meaning IP is likely unused
			log.Infow("found unused IP candidate", "ip", candidate)
			return candidate, nil
		}
		log.Infow("IP is in use", "ip", candidate)
	}

	return "", fmt.Errorf("no unused IP found in range %s.245-%s.254", network[:3], network[:3])
}

// isRFC1918 checks if an IP address is in RFC1918 private address space.
func isRFC1918(ip net.IP) bool {
	for _, cidr := range rfc1918Networks {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// extractCIDRPrefix extracts the prefix length from a CIDR string.
func extractCIDRPrefix(cidr string) string {
	return cidr // Already just the prefix number
}

// generateAuthPassword generates a random password for VRRP authentication.
func generateAuthPassword() string {
	bytes := make([]byte, defaults.KeepalivedAuthPassLength/2)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// InstallAndConfigureKeepalived installs and configures Keepalived on all enabled nodes.
func InstallAndConfigureKeepalived(ctx context.Context, sshPool *ssh.Pool, deployment *KeepalivedDeployment) error {
	if !deployment.Enabled || len(deployment.Nodes) == 0 {
		return nil
	}

	log := logging.L().With("component", "keepalived")

	for _, nodeConfig := range deployment.Nodes {
		log.Infow("installing and configuring Keepalived",
			"node", nodeConfig.Hostname,
			"state", nodeConfig.State,
			"priority", nodeConfig.Priority,
		)

		if err := installKeepalivedOnNode(ctx, sshPool, nodeConfig, deployment); err != nil {
			return fmt.Errorf("failed to configure Keepalived on %s: %w", nodeConfig.Hostname, err)
		}

		log.Infow("✅ Keepalived configured", "node", nodeConfig.Hostname)
	}

	return nil
}

// installKeepalivedOnNode installs and configures Keepalived on a single node.
func installKeepalivedOnNode(ctx context.Context, sshPool *ssh.Pool, nodeConfig *KeepalivedNodeConfig, deployment *KeepalivedDeployment) error {
	host := nodeConfig.Hostname

	// Install keepalived idempotently
	installCmd := `
if ! command -v keepalived &> /dev/null; then
    echo "Installing keepalived..."
    if command -v apt-get &> /dev/null; then
        apt-get update && apt-get install -y keepalived
    elif command -v yum &> /dev/null; then
        yum install -y keepalived
    elif command -v dnf &> /dev/null; then
        dnf install -y keepalived
    else
        echo "ERROR: No supported package manager found"
        exit 1
    fi
else
    echo "keepalived already installed"
fi
`
	stdout, stderr, err := sshPool.Run(ctx, host, installCmd)
	if err != nil {
		return fmt.Errorf("failed to install keepalived: %w (stderr: %s)", err, stderr)
	}
	logging.L().Infow("keepalived install output", "stdout", strings.TrimSpace(stdout))

	// Generate keepalived.conf
	keepalivedConf := generateKeepalivedConf(nodeConfig, deployment)

	// Write configuration
	writeCmd := fmt.Sprintf(`cat > /etc/keepalived/keepalived.conf << 'KEEPALIVED_EOF'
%s
KEEPALIVED_EOF`, keepalivedConf)

	if _, stderr, err := sshPool.Run(ctx, host, writeCmd); err != nil {
		return fmt.Errorf("failed to write keepalived.conf: %w (stderr: %s)", err, stderr)
	}

	// Write health check script
	if err := WriteHealthCheckScript(ctx, sshPool, host); err != nil {
		return fmt.Errorf("failed to write health check script: %w", err)
	}

	// Enable and restart keepalived
	restartCmd := `systemctl enable keepalived && systemctl restart keepalived`
	if _, stderr, err := sshPool.Run(ctx, host, restartCmd); err != nil {
		return fmt.Errorf("failed to restart keepalived: %w (stderr: %s)", err, stderr)
	}

	return nil
}

// generateKeepalivedConf generates the keepalived.conf content for a node.
func generateKeepalivedConf(nodeConfig *KeepalivedNodeConfig, deployment *KeepalivedDeployment) string {
	// Generate the health check script for Docker Swarm
	checkScript := `#!/bin/bash
# Check if this node is active in Docker Swarm
if docker node ls &>/dev/null; then
    exit 0
else
    exit 1
fi`

	conf := fmt.Sprintf(`# Keepalived configuration - Generated by dscotctl
# VIP: %s | Interface: %s | Node State: %s

global_defs {
    router_id %s_%d
    script_user root
    enable_script_security
}

vrrp_script chk_docker_swarm {
    script "/etc/keepalived/check_docker_swarm.sh"
    interval 5
    weight -20
    fall 2
    rise 2
}

vrrp_instance %s {
    state %s
    interface %s
    virtual_router_id %d
    priority %d
    advert_int %d

    authentication {
        auth_type PASS
        auth_pass %s
    }

    virtual_ipaddress {
        %s
    }

    track_script {
        chk_docker_swarm
    }
}
`,
		deployment.VIPCIDR,
		nodeConfig.Interface,
		nodeConfig.State,
		defaults.KeepalivedVRRPInstance,
		deployment.RouterID,
		defaults.KeepalivedVRRPInstance,
		nodeConfig.State,
		nodeConfig.Interface,
		deployment.RouterID,
		nodeConfig.Priority,
		defaults.KeepalivedAdvertInterval,
		deployment.AuthPass,
		deployment.VIPCIDR,
	)

	// Add the check script as a separate file command
	// We'll write this as part of the configuration
	_ = checkScript // Will be written separately

	return conf
}

// WriteHealthCheckScript writes the Docker Swarm health check script to a node.
func WriteHealthCheckScript(ctx context.Context, sshPool *ssh.Pool, host string) error {
	script := `#!/bin/bash
# Docker Swarm health check for Keepalived
# Returns 0 if node is healthy in swarm, 1 otherwise

if ! command -v docker &> /dev/null; then
    exit 1
fi

if docker node ls &>/dev/null; then
    exit 0
else
    exit 1
fi
`
	cmd := fmt.Sprintf(`cat > /etc/keepalived/check_docker_swarm.sh << 'SCRIPT_EOF'
%s
SCRIPT_EOF
chmod +x /etc/keepalived/check_docker_swarm.sh`, script)

	_, stderr, err := sshPool.Run(ctx, host, cmd)
	if err != nil {
		return fmt.Errorf("failed to write health check script: %w (stderr: %s)", err, stderr)
	}

	return nil
}

