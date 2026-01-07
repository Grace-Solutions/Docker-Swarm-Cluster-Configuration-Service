package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseBindMounts(t *testing.T) {
	// Test with sample YAML content
	content := `services:
  EdgeLoadBalancer:
    image: nginx:latest
    volumes:
      - /mnt/MicroCephFS/docker-swarm-0001/data/EdgeLoadBalancer/conf/nginx.conf:/etc/nginx/nginx.conf:ro
      - /mnt/MicroCephFS/docker-swarm-0001/data/EdgeLoadBalancer/conf/conf.d:/etc/nginx/conf.d:ro
      - /mnt/MicroCephFS/docker-swarm-0001/data/EdgeLoadBalancer/conf/sites-enabled:/etc/nginx/sites-enabled:ro
      - /mnt/MicroCephFS/docker-swarm-0001/data/EdgeLoadBalancer/acme-challenge:/etc/nginx/acme-challenge:ro
      - /var/lib/nginx/cache:/var/cache/nginx:rw
      - /var/lib/nginx/tmp:/var/lib/nginx/tmp:rw
      - /var/log/nginx:/var/log/nginx:rw
`

	storageMountPath := "/mnt/MicroCephFS/docker-swarm-0001"

	result := parseBindMounts(content, storageMountPath)

	t.Logf("Storage paths found: %d", len(result.StoragePaths))
	for _, p := range result.StoragePaths {
		t.Logf("  Storage: %s", p)
	}

	t.Logf("Local paths found: %d", len(result.LocalPaths))
	for _, p := range result.LocalPaths {
		t.Logf("  Local: %s", p)
	}

	// Verify expected counts
	if len(result.StoragePaths) == 0 {
		t.Errorf("Expected storage paths, got none")
	}
	if len(result.LocalPaths) == 0 {
		t.Errorf("Expected local paths, got none")
	}
}

func TestParseBindMountsFromRealYAMLs(t *testing.T) {
	// Find the binaries/services directory relative to test
	servicesDir := filepath.Join("..", "..", "binaries", "services")

	if _, err := os.Stat(servicesDir); os.IsNotExist(err) {
		t.Skipf("Services directory not found at %s", servicesDir)
	}

	files, err := os.ReadDir(servicesDir)
	if err != nil {
		t.Fatalf("Failed to read services dir: %v", err)
	}

	storageMountPath := "/mnt/MicroCephFS/docker-swarm-0001"

	for _, file := range files {
		if filepath.Ext(file.Name()) != ".yml" && filepath.Ext(file.Name()) != ".yaml" {
			continue
		}

		t.Run(file.Name(), func(t *testing.T) {
			content, err := os.ReadFile(filepath.Join(servicesDir, file.Name()))
			if err != nil {
				t.Fatalf("Failed to read file: %v", err)
			}

			// Apply variable replacement first (simulates deployment processing)
			processedContent := replaceStoragePaths(string(content), storageMountPath)
			result := parseBindMounts(processedContent, storageMountPath)

			t.Logf("File: %s", file.Name())
			t.Logf("  Storage paths: %d", len(result.StoragePaths))
			for _, p := range result.StoragePaths {
				t.Logf("    %s", p)
			}
			t.Logf("  Local paths: %d", len(result.LocalPaths))
			for _, p := range result.LocalPaths {
				t.Logf("    %s", p)
			}
		})
	}
}

func TestIsFilePath(t *testing.T) {
	tests := []struct {
		path     string
		isFile   bool
		reason   string
	}{
		{"/etc/nginx/nginx.conf", true, "has .conf extension"},
		{"/etc/nginx/conf.d", false, "conf.d is a directory pattern"},
		{"/etc/nginx/sites-enabled", false, "no extension"},
		{"/etc/nginx/stream.d", false, "stream.d is a directory pattern"},
		{"/var/log/nginx", false, "no extension"},
		{"/etc/ssl/certs/ca.crt", true, "has .crt extension"},
		{"/etc/nginx/.htpasswd", false, "hidden file, treated as directory"},
		{"/mnt/data/file.tar.gz", true, "has .gz extension"},
		{"/mnt/data/v1.0", false, "version number, not a file extension"},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			result := isFilePath(tc.path)
			if result != tc.isFile {
				t.Errorf("isFilePath(%q) = %v, want %v (%s)", tc.path, result, tc.isFile, tc.reason)
			}
		})
	}
}

func TestGetDirectoryForPath(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/etc/nginx/nginx.conf", "/etc/nginx"},
		{"/etc/nginx/conf.d", "/etc/nginx/conf.d"},       // Directory with dot - keep as-is
		{"/etc/nginx/stream.d", "/etc/nginx/stream.d"},   // Directory with dot - keep as-is
		{"/var/lib/nginx/cache", "/var/lib/nginx/cache"}, // No extension - directory
		{"/etc/ssl/certs/ca.crt", "/etc/ssl/certs"},      // File - get parent
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			result := getDirectoryForPath(tc.path)
			if result != tc.expected {
				t.Errorf("getDirectoryForPath(%q) = %q, want %q", tc.path, result, tc.expected)
			}
		})
	}
}

func TestParseBindMountsRegexDetails(t *testing.T) {
	// Test specific lines to debug regex matching
	testLines := []struct {
		line     string
		expected string
	}{
		{"      - /mnt/MicroCephFS/docker-swarm-0001/data/EdgeLoadBalancer/conf/nginx.conf:/etc/nginx/nginx.conf:ro", "/mnt/MicroCephFS/docker-swarm-0001/data/EdgeLoadBalancer/conf"},
		{"      - /var/lib/nginx/cache:/var/cache/nginx:rw", "/var/lib/nginx/cache"},
		{"    - /mnt/test/data:/container/data", "/mnt/test/data"},
		{"      - named_volume:/data", ""}, // Named volume, should not match
		{"      - /etc/nginx/conf.d:/etc/nginx/conf.d:ro", "/etc/nginx/conf.d"}, // Directory with dot
	}

	for _, tc := range testLines {
		t.Run(tc.line, func(t *testing.T) {
			result := parseBindMounts(tc.line, "/mnt/MicroCephFS/docker-swarm-0001")

			totalPaths := append(result.StoragePaths, result.LocalPaths...)
			if tc.expected == "" {
				if len(totalPaths) > 0 {
					t.Errorf("Expected no paths, got: %v", totalPaths)
				}
			} else {
				if len(totalPaths) == 0 {
					t.Errorf("Expected path %s, got none", tc.expected)
				} else {
					t.Logf("Got path: %s (expected: %s)", totalPaths[0], tc.expected)
				}
			}
		})
	}
}

