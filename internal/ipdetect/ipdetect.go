// Package ipdetect provides centralized IP detection with consistent precedence rules.
//
// All IP detection in the codebase should use this package to ensure consistent
// behavior. The package supports both local detection (for node agents) and
// remote detection via SSH (for controller operations).
//
// IP Precedence (highest to lowest):
//  1. CGNAT 100.64.0.0/10 (overlay networks like Netbird/Tailscale)
//  2. RFC1918 private: 10.0.0.0/8 > 172.16.0.0/12 > 192.168.0.0/16
//  3. Other non-loopback addresses
//  4. Loopback as last resort
//
// Docker network subnets are always excluded since they are not routable across hosts.
package ipdetect

import (
	"errors"
	"net"
	"os/exec"
	"strconv"
	"strings"
)

// IPClass represents the classification of an IP address for precedence ordering.
type IPClass int

const (
	// ClassOther is for public or unclassified IPs (lowest precedence after loopback).
	ClassOther IPClass = iota
	// ClassLoopback is for loopback addresses (127.x.x.x) - last resort.
	ClassLoopback
	// ClassRFC1918 is for private network IPs (10.x, 172.16-31.x, 192.168.x).
	ClassRFC1918
	// ClassCGNAT is for overlay network IPs (100.64-127.x) - highest precedence.
	ClassCGNAT
)

// NetworkInfo contains IP and CIDR information for a network address.
type NetworkInfo struct {
	IP   string // The IP address (e.g., "100.76.202.130")
	CIDR string // The CIDR notation (e.g., "100.76.202.130/32")
}

// DetectPrimary returns the preferred primary IPv4 address for the local node.
//
// Preference order:
//  1. CGNAT 100.64.0.0/10 (overlay networks)
//  2. RFC1918 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16
//  3. Other non-loopback addresses
//  4. Loopback as a last resort
//
// Docker network subnets are excluded since they are not routable across hosts.
func DetectPrimary() (net.IP, error) {
	dockerSubnets := GetDockerSubnetsLocal()
	return detectPrimaryWithExclusions(dockerSubnets)
}

// detectPrimaryWithExclusions is the internal implementation that accepts pre-fetched Docker subnets.
func detectPrimaryWithExclusions(dockerSubnets []*net.IPNet) (net.IP, error) {
	var cgnat, rfc1918, other, loopback []net.IP

	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, a := range addrs {
			ip := ipFromAddr(a)
			if ip == nil || ip.To4() == nil {
				continue
			}

			if ip.IsLoopback() {
				loopback = append(loopback, ip)
				continue
			}

			// Skip IPs in Docker subnets
			if IsInDockerSubnet(ip, dockerSubnets) {
				continue
			}

			switch ClassifyIP(ip) {
			case ClassCGNAT:
				cgnat = append(cgnat, ip)
			case ClassRFC1918:
				rfc1918 = append(rfc1918, ip)
			default:
				other = append(other, ip)
			}
		}
	}

	if len(cgnat) > 0 {
		return cgnat[0], nil
	}
	if len(rfc1918) > 0 {
		return rfc1918[0], nil
	}
	if len(other) > 0 {
		return other[0], nil
	}
	if len(loopback) > 0 {
		return loopback[0], nil
	}

	return nil, errors.New("ipdetect: no IPv4 address found")
}

// ClassifyIP returns the classification of an IP address for precedence ordering.
// Higher class values indicate higher precedence.
func ClassifyIP(ip net.IP) IPClass {
	ip4 := ip.To4()
	if ip4 == nil {
		return ClassOther
	}

	if ip4.IsLoopback() {
		return ClassLoopback
	}

	// CGNAT / Overlay networks: 100.64.0.0/10 (100.64.x.x - 100.127.x.x)
	if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
		return ClassCGNAT
	}

	// RFC1918 private networks
	if ip4[0] == 10 {
		return ClassRFC1918
	}
	if ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31 {
		return ClassRFC1918
	}
	if ip4[0] == 192 && ip4[1] == 168 {
		return ClassRFC1918
	}

	return ClassOther
}

// IsCGNAT returns true if the IP is in the CGNAT/overlay range (100.64.0.0/10).
func IsCGNAT(ip net.IP) bool {
	return ClassifyIP(ip) == ClassCGNAT
}

// IsRFC1918 returns true if the IP is in an RFC1918 private range.
func IsRFC1918(ip net.IP) bool {
	return ClassifyIP(ip) == ClassRFC1918
}

// IsInDockerSubnet checks if an IP address falls within any Docker network subnet.
func IsInDockerSubnet(ip net.IP, dockerSubnets []*net.IPNet) bool {
	for _, subnet := range dockerSubnets {
		if subnet.Contains(ip) {
			return true
		}
	}
	return false
}

// GetDockerSubnetsLocal retrieves Docker network subnets from the local system.
// Returns a slice of *net.IPNet representing Docker-managed network ranges.
// These should be excluded from IP selection since Docker IPs are not routable.
func GetDockerSubnetsLocal() []*net.IPNet {
	// Check if docker is available
	if _, err := exec.LookPath("docker"); err != nil {
		return nil
	}

	// Get list of Docker network IDs
	listCmd := exec.Command("docker", "network", "ls", "--format", "{{.ID}}")
	listOut, err := listCmd.Output()
	if err != nil {
		return nil
	}

	var subnets []*net.IPNet
	networkIDs := strings.Split(strings.TrimSpace(string(listOut)), "\n")

	for _, netID := range networkIDs {
		netID = strings.TrimSpace(netID)
		if netID == "" {
			continue
		}

		// Inspect each network for its subnet
		inspectCmd := exec.Command("docker", "network", "inspect", netID, "--format", "{{range .IPAM.Config}}{{.Subnet}}\n{{end}}")
		inspectOut, err := inspectCmd.Output()
		if err != nil {
			continue
		}

		for _, line := range strings.Split(strings.TrimSpace(string(inspectOut)), "\n") {
			cidr := strings.TrimSpace(line)
			if cidr == "" {
				continue
			}
			_, ipnet, err := net.ParseCIDR(cidr)
			if err != nil {
				continue
			}
			subnets = append(subnets, ipnet)
		}
	}

	return subnets
}

// ParseSubnetsFromCIDRs parses a slice of CIDR strings into *net.IPNet.
// Invalid CIDRs are silently skipped.
func ParseSubnetsFromCIDRs(cidrs []string) []*net.IPNet {
	var subnets []*net.IPNet
	for _, cidr := range cidrs {
		_, ipnet, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		subnets = append(subnets, ipnet)
	}
	return subnets
}

// SelectBestIP selects the best IP from a list based on precedence rules.
// Docker subnets are excluded. Returns empty string if no suitable IP found.
func SelectBestIP(ips []string, dockerSubnets []*net.IPNet) string {
	var cgnat, rfc1918, other []string

	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil || ip.To4() == nil {
			continue
		}

		if ip.IsLoopback() {
			continue
		}

		if IsInDockerSubnet(ip, dockerSubnets) {
			continue
		}

		switch ClassifyIP(ip) {
		case ClassCGNAT:
			cgnat = append(cgnat, ipStr)
		case ClassRFC1918:
			rfc1918 = append(rfc1918, ipStr)
		default:
			other = append(other, ipStr)
		}
	}

	if len(cgnat) > 0 {
		return cgnat[0]
	}
	if len(rfc1918) > 0 {
		return rfc1918[0]
	}
	if len(other) > 0 {
		return other[0]
	}
	return ""
}

// Helper functions

func inCIDR(ip net.IP, base string, prefix int) bool {
	_, network, err := net.ParseCIDR(base + "/" + strconv.Itoa(prefix))
	if err != nil {
		return false
	}
	return network.Contains(ip)
}

func ipFromAddr(a net.Addr) net.IP {
	switch v := a.(type) {
	case *net.IPNet:
		return v.IP
	case *net.IPAddr:
		return v.IP
	default:
		return nil
	}
}

