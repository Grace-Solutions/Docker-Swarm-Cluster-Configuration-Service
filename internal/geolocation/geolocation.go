package geolocation

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"dscotctl/internal/logging"
	"dscotctl/internal/ssh"
)

// GeoInfo represents geolocation information for a node.
type GeoInfo struct {
	PublicIP    string `json:"ip"`
	Country     string `json:"country"`
	CountryCode string `json:"countryCode"`
	Region      string `json:"region"`
	RegionName  string `json:"regionName"`
	City        string `json:"city"`
	Timezone    string `json:"timezone"`
	ISP         string `json:"isp"`
}

// DetectGeoLocation detects the geolocation of a node by making an outbound call from the node itself.
func DetectGeoLocation(ctx context.Context, sshPool *ssh.Pool, hostname string) (*GeoInfo, error) {
	log := logging.L().With("node", hostname, "component", "geolocation")

	// Get public IP from the node itself
	publicIPCmd := "curl -s -4 https://api.ipify.org"
	publicIP, stderr, err := sshPool.Run(ctx, hostname, publicIPCmd)
	if err != nil {
		log.Warnw("failed to detect public IP", "error", err, "stderr", stderr)
		return &GeoInfo{PublicIP: "unknown"}, nil
	}

	publicIP = publicIP[:len(publicIP)-1] // Remove trailing newline
	log.Infow("detected public IP", "ip", publicIP)

	// Get geolocation info from ip-api.com (free, no API key required)
	geoCmd := fmt.Sprintf("curl -s 'http://ip-api.com/json/%s?fields=status,message,country,countryCode,region,regionName,city,timezone,isp'", publicIP)
	geoJSON, stderr, err := sshPool.Run(ctx, hostname, geoCmd)
	if err != nil {
		log.Warnw("failed to get geolocation", "error", err, "stderr", stderr)
		return &GeoInfo{PublicIP: publicIP}, nil
	}

	// Parse geolocation response
	var response struct {
		Status      string `json:"status"`
		Message     string `json:"message"`
		Country     string `json:"country"`
		CountryCode string `json:"countryCode"`
		Region      string `json:"region"`
		RegionName  string `json:"regionName"`
		City        string `json:"city"`
		Timezone    string `json:"timezone"`
		ISP         string `json:"isp"`
	}

	if err := json.Unmarshal([]byte(geoJSON), &response); err != nil {
		log.Warnw("failed to parse geolocation response", "error", err, "response", geoJSON)
		return &GeoInfo{PublicIP: publicIP}, nil
	}

	if response.Status != "success" {
		log.Warnw("geolocation API returned error", "message", response.Message)
		return &GeoInfo{PublicIP: publicIP}, nil
	}

	geoInfo := &GeoInfo{
		PublicIP:    publicIP,
		Country:     response.Country,
		CountryCode: response.CountryCode,
		Region:      response.Region,
		RegionName:  response.RegionName,
		City:        response.City,
		Timezone:    response.Timezone,
		ISP:         response.ISP,
	}

	log.Infow("geolocation detected",
		"country", geoInfo.Country,
		"region", geoInfo.RegionName,
		"city", geoInfo.City,
	)

	return geoInfo, nil
}

// DetectGeoLocationBatch detects geolocation for multiple nodes in parallel.
func DetectGeoLocationBatch(ctx context.Context, sshPool *ssh.Pool, hostnames []string) map[string]*GeoInfo {
	log := logging.L().With("component", "geolocation-batch")
	log.Infow("detecting geolocation for nodes", "count", len(hostnames))

	results := make(map[string]*GeoInfo)
	resultChan := make(chan struct {
		hostname string
		geoInfo  *GeoInfo
	}, len(hostnames))

	// Detect geolocation for each node in parallel
	for _, hostname := range hostnames {
		go func(h string) {
			geoInfo, _ := DetectGeoLocation(ctx, sshPool, h)
			resultChan <- struct {
				hostname string
				geoInfo  *GeoInfo
			}{h, geoInfo}
		}(hostname)
	}

	// Collect results
	for i := 0; i < len(hostnames); i++ {
		result := <-resultChan
		results[result.hostname] = result.geoInfo
	}

	log.Infow("geolocation detection complete", "count", len(results))
	return results
}

// GetPublicIPFromLocal gets the public IP from the local machine (for testing).
func GetPublicIPFromLocal(ctx context.Context) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.ipify.org", nil)
	if err != nil {
		return "", err
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(body), nil
}

