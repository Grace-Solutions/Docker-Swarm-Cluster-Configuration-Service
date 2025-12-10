// Package ipdetect provides centralized IP detection with consistent precedence rules.
// This file contains SSH-based detection for remote nodes.
package ipdetect

import (
	"context"
	"encoding/json"
	"net"
	"strings"

	"clusterctl/internal/ssh"
)

// dockerNetworkListEntry represents a single line from `docker network ls --format json`.
type dockerNetworkListEntry struct {
	ID   string `json:"ID"`
	Name string `json:"Name"`
}

// dockerNetworkInspect represents the output of `docker network inspect <id> --format json`.
type dockerNetworkInspect struct {
	Name string `json:"Name"`
	IPAM struct {
		Config []struct {
			Subnet string `json:"Subnet"`
		} `json:"Config"`
	} `json:"IPAM"`
}

// GetDockerSubnetsSSH retrieves Docker network subnets from a remote node via SSH.
// Uses JSON parsing for reliability. Returns nil if Docker is not available.
func GetDockerSubnetsSSH(ctx context.Context, sshPool *ssh.Pool, node string) []*net.IPNet {
	// Get list of Docker networks as NDJSON (one JSON object per line)
	listCmd := "docker network ls --format json 2>/dev/null || true"
	listOut, _, err := sshPool.Run(ctx, node, listCmd)
	if err != nil || strings.TrimSpace(listOut) == "" {
		return nil
	}

	// Parse each line as a separate JSON object (NDJSON format)
	var networkIDs []string
	for _, line := range strings.Split(strings.TrimSpace(listOut), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry dockerNetworkListEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.ID != "" {
			networkIDs = append(networkIDs, entry.ID)
		}
	}

	if len(networkIDs) == 0 {
		return nil
	}

	var subnets []*net.IPNet

	// Inspect each network to get its subnets
	for _, netID := range networkIDs {
		inspectCmd := "docker network inspect " + netID + " --format json 2>/dev/null || true"
		inspectOut, _, err := sshPool.Run(ctx, node, inspectCmd)
		if err != nil || strings.TrimSpace(inspectOut) == "" {
			continue
		}

		// docker network inspect returns a JSON array
		var networks []dockerNetworkInspect
		if err := json.Unmarshal([]byte(inspectOut), &networks); err != nil {
			continue
		}

		for _, network := range networks {
			for _, cfg := range network.IPAM.Config {
				if cfg.Subnet == "" {
					continue
				}
				_, ipnet, err := net.ParseCIDR(cfg.Subnet)
				if err != nil {
					continue
				}
				subnets = append(subnets, ipnet)
			}
		}
	}

	return subnets
}

// DetectPrimarySSH detects the best IP address on a remote node via SSH.
// It uses the standard IP precedence: overlay IP > private IP > fallback to node string.
// Docker network subnets are excluded.
func DetectPrimarySSH(ctx context.Context, sshPool *ssh.Pool, node, overlayProvider string) string {
	overlayProvider = strings.ToLower(strings.TrimSpace(overlayProvider))

	// 1. Try overlay IP from Netbird
	if overlayProvider == "netbird" {
		stdout, _, err := sshPool.Run(ctx, node, "netbird status --json")
		if err == nil {
			var status struct {
				NetbirdIP string `json:"netbirdIp"`
			}
			if json.Unmarshal([]byte(stdout), &status) == nil && status.NetbirdIP != "" {
				ip := strings.Split(status.NetbirdIP, "/")[0]
				return ip
			}
		}
	}

	// 2. Try overlay IP from Tailscale
	if overlayProvider == "tailscale" {
		stdout, _, err := sshPool.Run(ctx, node, "tailscale status --json")
		if err == nil {
			var status struct {
				Self struct {
					TailscaleIPs []string `json:"TailscaleIPs"`
				} `json:"Self"`
			}
			if json.Unmarshal([]byte(stdout), &status) == nil && len(status.Self.TailscaleIPs) > 0 {
				return status.Self.TailscaleIPs[0]
			}
		}
	}

	// Get Docker subnets to exclude
	dockerSubnets := GetDockerSubnetsSSH(ctx, sshPool, node)

	// 3. Get all IPs and select best using precedence
	stdout, _, err := sshPool.Run(ctx, node, "hostname -I 2>/dev/null || hostname -i 2>/dev/null || echo ''")
	if err == nil {
		fields := strings.Fields(strings.TrimSpace(stdout))
		bestIP := SelectBestIP(fields, dockerSubnets)
		if bestIP != "" {
			return bestIP
		}
	}

	// 4. Fallback to SSH node string
	return node
}

// DetectNetworkInfoSSH detects the appropriate network IP and CIDR for cluster communication.
// Priority: RFC 6598 overlay (100.64.0.0/10) > RFC 1918 private > none
// Docker network subnets are excluded since they are not routable across hosts.
// Returns both the IP (for --mon-ip) and CIDR (for --cluster-network)
func DetectNetworkInfoSSH(ctx context.Context, sshPool *ssh.Pool, node string) *NetworkInfo {
	// First, get Docker network subnets to exclude
	dockerSubnets := GetDockerSubnetsSSH(ctx, sshPool, node)

	// Get all IPv4 addresses with their CIDR notation
	cmd := "ip -4 -o addr show | awk '{print $4}' | grep -v '^127\\.'"
	stdout, _, err := sshPool.Run(ctx, node, cmd)
	if err != nil {
		return nil
	}

	var cgnatInfo, rfc1918Info *NetworkInfo
	lines := strings.Split(strings.TrimSpace(stdout), "\n")

	for _, line := range lines {
		cidr := strings.TrimSpace(line)
		if cidr == "" {
			continue
		}

		ip, _, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}

		// Convert to 4-byte IPv4 representation
		ip4 := ip.To4()
		if ip4 == nil {
			continue
		}

		// Skip IPs that fall within Docker network subnets
		if IsInDockerSubnet(ip4, dockerSubnets) {
			continue
		}

		// Check RFC 6598 (CGNAT - overlay networks like Netbird/Tailscale)
		if ClassifyIP(ip4) == ClassCGNAT {
			cgnatInfo = &NetworkInfo{IP: ip4.String(), CIDR: cidr}
			continue
		}

		// Check RFC 1918 private networks (all classes have equal priority)
		if ClassifyIP(ip4) == ClassRFC1918 {
			if rfc1918Info == nil {
				rfc1918Info = &NetworkInfo{IP: ip4.String(), CIDR: cidr}
			}
		}
	}

	// Priority: RFC 6598 (overlay) > RFC 1918 (private)
	if cgnatInfo != nil {
		return cgnatInfo
	}
	if rfc1918Info != nil {
		return rfc1918Info
	}

	return nil
}

// ResolveNodeAddressSSH resolves the best address for a node with precedence:
// 1. Overlay hostname (netbird FQDN / tailscale DNSName)
// 2. Overlay IP (100.x.x.x)
// 3. Private hostname (system hostname)
// 4. Private IP (RFC 1918)
func ResolveNodeAddressSSH(ctx context.Context, sshPool *ssh.Pool, node, overlayProvider string) string {
	overlayProvider = strings.ToLower(strings.TrimSpace(overlayProvider))

	// Try overlay hostname first
	if overlayProvider == "netbird" {
		stdout, _, err := sshPool.Run(ctx, node, "netbird status --json")
		if err == nil {
			var status struct {
				FQDN      string `json:"fqdn"`
				NetbirdIP string `json:"netbirdIp"`
			}
			if json.Unmarshal([]byte(stdout), &status) == nil {
				// 1. Overlay hostname
				if status.FQDN != "" {
					return status.FQDN
				}
				// 2. Overlay IP
				if status.NetbirdIP != "" {
					return strings.Split(status.NetbirdIP, "/")[0]
				}
			}
		}
	} else if overlayProvider == "tailscale" {
		stdout, _, err := sshPool.Run(ctx, node, "tailscale status --json")
		if err == nil {
			var status struct {
				Self struct {
					DNSName      string   `json:"DNSName"`
					TailscaleIPs []string `json:"TailscaleIPs"`
				} `json:"Self"`
			}
			if json.Unmarshal([]byte(stdout), &status) == nil {
				// 1. Overlay hostname
				if status.Self.DNSName != "" {
					return strings.TrimSuffix(status.Self.DNSName, ".")
				}
				// 2. Overlay IP
				if len(status.Self.TailscaleIPs) > 0 {
					return status.Self.TailscaleIPs[0]
				}
			}
		}
	}

	// 3. Private hostname
	stdout, _, err := sshPool.Run(ctx, node, "hostname -f 2>/dev/null || hostname 2>/dev/null || echo ''")
	if err == nil {
		hostname := strings.TrimSpace(stdout)
		if hostname != "" && hostname != "localhost" {
			return hostname
		}
	}

	// 4. Private IP (use DetectPrimarySSH which handles Docker exclusion)
	return DetectPrimarySSH(ctx, sshPool, node, overlayProvider)
}

