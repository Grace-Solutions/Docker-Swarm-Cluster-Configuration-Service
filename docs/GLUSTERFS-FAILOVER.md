# GlusterFS Automatic Failover Architecture

## Overview

This document explains how automatic failover works in our GlusterFS setup and why we don't need DNS round-robin or virtual IPs.

## How GlusterFS Failover Works

### The Magic of Client-Side Intelligence

When you mount a GlusterFS volume, the client doesn't just connect to one server. Here's what actually happens:

1. **Initial Connection**: Client connects to the primary server (e.g., `localhost`)
2. **Download Volfile**: Client downloads the volume configuration file (volfile)
3. **Volfile Contains ALL Bricks**: The volfile lists all 6 bricks in your replica set
4. **Client Knows Everything**: The FUSE client now has a complete map of the cluster
5. **Transparent Failover**: If any brick fails, the client automatically uses another brick

**Key Point**: Even though you mount from `localhost`, the client can read/write to ANY of the 6 bricks!

### Mount Configuration

#### Workers (with local bricks)
```bash
mount -t glusterfs \
  -o backupvolfile-server=worker2:worker3:worker4:worker5:worker6 \
  localhost:0001 /mnt/GlusterFS/Docker/Swarm/0001/data
```

**Why localhost?**
- ✅ Fastest performance (no network latency for local reads)
- ✅ Reduces network traffic
- ✅ Still has full failover capability

**What are backup servers for?**
- Used if `localhost` is unreachable when **first mounting**
- Used if you need to **re-download the volfile** (rare)
- NOT used for normal read/write operations (client already knows all bricks)

#### Managers (no local bricks)
```bash
mount -t glusterfs \
  -o backupvolfile-server=worker2:worker3:worker4:worker5:worker6 \
  worker1:0001 /mnt/GlusterFS/Docker/Swarm/0001/data
```

**Why worker1?**
- Managers don't have bricks, so they must use a remote server
- worker1 is arbitrary - could be any worker
- If worker1 fails, client automatically uses worker2, worker3, etc.

### /etc/fstab Configuration

#### Workers
```
localhost:0001 /mnt/GlusterFS/Docker/Swarm/0001/data glusterfs defaults,_netdev,backupvolfile-server=worker2:worker3:worker4:worker5:worker6 0 0
```

#### Managers
```
worker1:0001 /mnt/GlusterFS/Docker/Swarm/0001/data glusterfs defaults,_netdev,backupvolfile-server=worker2:worker3:worker4:worker5:worker6 0 0
```

**Mount Options Explained**:
- `defaults`: Standard mount options
- `_netdev`: Wait for network before mounting (important for boot)
- `backupvolfile-server=...`: Backup servers for initial connection

## Failover Scenarios

### Scenario 1: Local Brick Fails (Worker Node)

**What happens:**
1. Worker has mount from `localhost:0001`
2. Local glusterd service crashes or brick becomes unavailable
3. GlusterFS client detects failure
4. Client automatically reads from other bricks (worker2, worker3, etc.)
5. Writes go to all available bricks
6. **No remount needed, no downtime**

**When local brick comes back:**
1. GlusterFS self-heal daemon detects the brick is back
2. Automatically syncs missed changes from other replicas
3. Brick rejoins the replica set
4. Everything is consistent again

### Scenario 2: Remote Server Fails (Manager Node)

**What happens:**
1. Manager has mount from `worker1:0001`
2. worker1 becomes unavailable
3. GlusterFS client automatically connects to worker2
4. Reads/writes continue seamlessly
5. **No remount needed, no downtime**

### Scenario 3: Multiple Servers Fail

**With 6 replicas, you can lose up to 5 servers:**
1. Client tries each server in order
2. Uses first available server
3. As long as 1 brick is available, data is accessible
4. When servers come back, self-heal syncs everything

## Why NOT Use DNS Round-Robin?

### Problems with DNS RR:

❌ **DNS Caching**: Responses are cached (TTL), so you might always get the same server

❌ **No Health Checking**: DNS doesn't know if a server is down

❌ **Stale Connections**: If the server you connected to goes down, DNS won't help

❌ **Complexity**: Adds another layer that can fail

❌ **Not Needed**: GlusterFS already has built-in failover!

### Why Current Approach is Better:

✅ **Client-side intelligence**: Client knows about all servers

✅ **Instant failover**: No DNS lookup delay

✅ **Health-aware**: Client detects failures immediately

✅ **Simple**: No DNS infrastructure needed

✅ **Works offline**: Even if DNS fails, failover still works

## Testing Failover

### Test 1: Stop Local Glusterd

```bash
# On a worker node
systemctl stop glusterd

# Try to access the mount - should still work!
ls -la /mnt/GlusterFS/Docker/Swarm/0001/data/
echo "test" > /mnt/GlusterFS/Docker/Swarm/0001/data/test.txt

# Restart glusterd
systemctl start glusterd
```

### Test 2: Network Partition

```bash
# On a worker node, block traffic to another worker
iptables -A OUTPUT -d <worker2-ip> -j DROP

# Access should still work via other workers
cat /mnt/GlusterFS/Docker/Swarm/0001/data/test.txt

# Remove the block
iptables -D OUTPUT -d <worker2-ip> -j DROP
```

### Test 3: Verify Replication

```bash
# Create a file on one node
echo "test from node1" > /mnt/GlusterFS/Docker/Swarm/0001/data/test.txt

# Check on another node - should see the same file immediately
ssh worker2 "cat /mnt/GlusterFS/Docker/Swarm/0001/data/test.txt"
```

## Performance Considerations

### Read Performance

**Workers (local brick)**:
- First read: Local brick (fastest - no network)
- Subsequent reads: Cached or local brick
- If local brick fails: Network read from another brick

**Managers (no local brick)**:
- All reads: Network read from a worker
- GlusterFS uses read-ahead and caching to optimize

### Write Performance

**All nodes**:
- Writes go to ALL bricks (replica count = 6)
- Write completes when majority of bricks acknowledge
- Slower than reads, but ensures data safety

## Monitoring

### Check Mount Status
```bash
mount | grep gluster
df -h | grep gluster
```

### Check Cluster Health
```bash
gluster peer status
gluster volume status
gluster volume heal <volume> info
```

### Check Self-Heal Status
```bash
gluster volume heal <volume> info
gluster volume heal <volume> info split-brain
```

## Summary

✅ **No DNS needed**: GlusterFS has built-in failover

✅ **No VIP needed**: Client-side intelligence handles failover

✅ **High availability**: Can lose 5 out of 6 servers

✅ **Automatic recovery**: Self-heal syncs when servers return

✅ **Simple**: Just mount with backup servers

✅ **Fast**: Local brick first, network only if needed

**Your GlusterFS setup is production-ready with true high availability!** 🎯

