package gluster

import (
	"context"
	"fmt"
	"strings"

	"clusterctl/internal/logging"
	"clusterctl/internal/retry"
	"clusterctl/internal/ssh"
)

// DiskInfo represents information about a disk on a node.
type DiskInfo struct {
	Device     string // e.g., "sdb"
	Size       string // e.g., "100G"
	Type       string // e.g., "disk"
	Mountpoint string // empty if not mounted
}

// DetectAvailableDisks detects non-OS disks available for GlusterFS on a node.
// Returns a list of available disks that are either:
// 1. Not mounted at all, OR
// 2. Already mounted at an expected GlusterFS brick path (reusable)
func DetectAvailableDisks(ctx context.Context, sshPool *ssh.Pool, host string) ([]DiskInfo, error) {
	log := logging.L().With("component", "gluster-disk", "host", host)

	// Use lsblk to list all block devices with their mountpoints
	// Format: NAME,SIZE,TYPE,MOUNTPOINT
	// Include ALL disks (mounted or not) - we'll filter later
	cmd := `lsblk -ndo NAME,SIZE,TYPE,MOUNTPOINT | grep 'disk' | awk '{print $1"|"$2"|"$3"|"$4}'`

	stdout, stderr, err := sshPool.Run(ctx, host, cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to list disks: %w (stderr: %s)", err, stderr)
	}

	var disks []DiskInfo
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}

		parts := strings.Split(line, "|")
		if len(parts) < 3 {
			continue
		}

		mountpoint := ""
		if len(parts) >= 4 {
			mountpoint = parts[3]
		}

		disk := DiskInfo{
			Device:     parts[0],
			Size:       parts[1],
			Type:       parts[2],
			Mountpoint: mountpoint,
		}

		// Skip if it's the OS disk (usually sda, vda, nvme0n1)
		// We detect OS disk by checking if any partition is mounted at /
		if isOSDisk(ctx, sshPool, host, disk.Device) {
			log.Infow("skipping OS disk", "device", disk.Device)
			continue
		}

		// Accept disk if:
		// 1. Not mounted (available for fresh setup)
		// 2. Already mounted at a GlusterFS brick path (reusable)
		if mountpoint == "" {
			disks = append(disks, disk)
			log.Infow("found available disk (not mounted)", "device", disk.Device, "size", disk.Size)
		} else if strings.Contains(mountpoint, "GlusterFS") || strings.Contains(mountpoint, "glusterfs") || strings.Contains(mountpoint, "brick") {
			// This disk is already mounted for GlusterFS, we can reuse it
			disks = append(disks, disk)
			log.Infow("found available disk (already mounted for GlusterFS)", "device", disk.Device, "size", disk.Size, "mountpoint", mountpoint)
		} else {
			log.Infow("skipping mounted disk (not GlusterFS)", "device", disk.Device, "mountpoint", mountpoint)
		}
	}

	return disks, nil
}

// isOSDisk checks if a disk contains the OS by checking if any partition is mounted at /.
func isOSDisk(ctx context.Context, sshPool *ssh.Pool, host, device string) bool {
	// Check if any partition of this disk is mounted at /
	cmd := fmt.Sprintf(`lsblk -nlo NAME,MOUNTPOINT /dev/%s | grep -E '/$' || true`, device)
	stdout, _, err := sshPool.Run(ctx, host, cmd)
	if err != nil {
		return false
	}

	return strings.TrimSpace(stdout) != ""
}

// FormatAndMountDisk formats a disk with XFS and mounts it at the specified path.
// It also adds the mount to /etc/fstab for persistence.
// If the disk is already mounted elsewhere, it unmounts and remounts at the correct path.
func FormatAndMountDisk(ctx context.Context, sshPool *ssh.Pool, host, device, mountPath string) error {
	log := logging.L().With("component", "gluster-disk", "host", host, "device", device, "mountPath", mountPath)

	devicePath := fmt.Sprintf("/dev/%s", device)

	// Check if this device is currently mounted somewhere
	checkDeviceMountCmd := fmt.Sprintf("lsblk -ndo MOUNTPOINT /dev/%s 2>/dev/null || true", device)
	stdout, _, _ := sshPool.Run(ctx, host, checkDeviceMountCmd)
	currentMountpoint := strings.TrimSpace(stdout)

	if currentMountpoint != "" && currentMountpoint != mountPath {
		// Device is mounted somewhere else - unmount it first
		log.Infow("disk mounted at different path, unmounting first", "currentMount", currentMountpoint)
		unmountCmd := fmt.Sprintf("umount %s 2>/dev/null || true", devicePath)
		_, _, _ = sshPool.Run(ctx, host, unmountCmd)
		// Also remove old fstab entry
		oldFstabCmd := fmt.Sprintf("sed -i '\\|%s|d' /etc/fstab 2>/dev/null || true", devicePath)
		_, _, _ = sshPool.Run(ctx, host, oldFstabCmd)
	}

	// Check if already formatted with XFS
	checkCmd := fmt.Sprintf("blkid -o value -s TYPE %s 2>/dev/null || true", devicePath)
	stdout, _, _ = sshPool.Run(ctx, host, checkCmd)
	fsType := strings.TrimSpace(stdout)

	if fsType != "xfs" {
		log.Infow("formatting disk with XFS", "currentType", fsType)

		// Format with XFS (inode size 512 as recommended by GlusterFS)
		formatCmd := fmt.Sprintf("mkfs.xfs -i size=512 -f %s", devicePath)
		retryCfg := retry.DefaultConfig(fmt.Sprintf("format-disk-%s-%s", host, device))

		err := retry.Do(ctx, retryCfg, func() error {
			_, stderr, err := sshPool.Run(ctx, host, formatCmd)
			if err != nil {
				return fmt.Errorf("failed to format disk: %w (stderr: %s)", err, stderr)
			}
			return nil
		})

		if err != nil {
			return err
		}

		log.Infow("✅ disk formatted with XFS")
	} else {
		log.Infow("disk already formatted with XFS")
	}

	// Create mount directory
	mkdirCmd := fmt.Sprintf("mkdir -p %s", mountPath)
	_, stderr, err := sshPool.Run(ctx, host, mkdirCmd)
	if err != nil {
		return fmt.Errorf("failed to create mount directory: %w (stderr: %s)", err, stderr)
	}

	// Check if already mounted at the correct path
	checkMountCmd := fmt.Sprintf("mountpoint -q %s && echo 'mounted' || echo 'not-mounted'", mountPath)
	stdout, _, _ = sshPool.Run(ctx, host, checkMountCmd)
	isMounted := strings.TrimSpace(stdout) == "mounted"

	if !isMounted {
		// Mount the disk
		log.Infow("mounting disk")
		mountCmd := fmt.Sprintf("mount %s %s", devicePath, mountPath)
		_, stderr, err = sshPool.Run(ctx, host, mountCmd)
		if err != nil {
			return fmt.Errorf("failed to mount disk: %w (stderr: %s)", err, stderr)
		}
		log.Infow("✅ disk mounted")
	} else {
		log.Infow("disk already mounted at correct path")
	}

	// Add to /etc/fstab if not already present
	log.Infow("adding to /etc/fstab")
	fstabEntry := fmt.Sprintf("%s %s xfs defaults 0 2", devicePath, mountPath)
	checkFstabCmd := fmt.Sprintf("grep -q '%s' /etc/fstab && echo 'exists' || echo 'not-exists'", devicePath)
	stdout, _, _ = sshPool.Run(ctx, host, checkFstabCmd)
	fstabExists := strings.TrimSpace(stdout) == "exists"

	if !fstabExists {
		addFstabCmd := fmt.Sprintf("echo '%s' >> /etc/fstab", fstabEntry)
		_, stderr, err = sshPool.Run(ctx, host, addFstabCmd)
		if err != nil {
			return fmt.Errorf("failed to add to fstab: %w (stderr: %s)", err, stderr)
		}
		log.Infow("✅ added to /etc/fstab")
	} else {
		log.Infow("already in /etc/fstab")
	}

	return nil
}

