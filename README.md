# Docker Swarm Cluster Configuration Service

`clusterctl` is a Go-based orchestrator that automates Docker Swarm
initialization, node joins, overlay networking, and GlusterFS integration.

The project is designed around a **single binary** (`clusterctl`) that supports
two deployment modes:

1. **Server-Initiated Deployment (Recommended)** - Deploy from a JSON configuration file
2. **Legacy Node-Agent Mode** - Nodes register with a controller server

## Quick Start (Server-Initiated Deployment)

The recommended way to deploy a cluster is using the `deploy` command with a JSON configuration file:

```bash
# 1. Create a configuration file (see clusterctl.json.example)
cp clusterctl.json.example clusterctl.json

# 2. Edit the configuration with your nodes and credentials
nano clusterctl.json

# 3. Deploy the cluster
./clusterctl deploy --config clusterctl.json
```

This will:
- ✅ Install Docker and GlusterFS on all nodes via SSH
- ✅ Configure overlay network (Netbird/Tailscale/WireGuard)
- ✅ Setup GlusterFS with replication and failover
- ✅ Initialize Docker Swarm with managers and workers
- ✅ Deploy Portainer (optional)

### Configuration File Format

See `clusterctl.json.example` for a complete example. The configuration has two main sections:

#### Global Settings

```json
{
  "globalSettings": {
    "clusterName": "production-swarm",
    "overlayProvider": "netbird",
    "glusterVolume": "docker-swarm-0001",
    "glusterMount": "/mnt/GlusterFS/Docker/Swarm/0001/data",
    "glusterBrick": "/mnt/GlusterFS/Docker/Swarm/0001/brick",
    "deployPortainer": true,
    "portainerPassword": ""
  }
}
```

- `clusterName`: Name of the Docker Swarm cluster (required)
- `overlayProvider`: Default overlay network provider: `netbird`, `tailscale`, `wireguard`, or `none` (default: `none`)
- `glusterVolume`: GlusterFS volume name (default: `docker-swarm-0001`)
- `glusterMount`: Default mount path for GlusterFS (default: `/mnt/GlusterFS/Docker/Swarm/0001/data`)
- `glusterBrick`: Default brick path for GlusterFS (default: `/mnt/GlusterFS/Docker/Swarm/0001/brick`)
- `deployPortainer`: Deploy Portainer web UI (default: `true`)
- `portainerPassword`: Portainer admin password (default: auto-generated)

#### Per-Node Configuration

Each node supports extensive per-node configuration with overrides:

```json
{
  "nodes": [
    {
      "hostname": "manager1.example.com",
      "username": "root",
      "password": "",
      "privateKeyPath": "/root/.ssh/id_ed25519",
      "sshPort": 22,
      "primaryMaster": true,
      "role": "manager",
      "overlayProvider": "",
      "overlayConfig": "your-netbird-setup-key-here",
      "glusterEnabled": false,
      "glusterMount": "",
      "glusterBrick": "",
      "advertiseAddr": ""
    },
    {
      "hostname": "worker1.example.com",
      "username": "admin",
      "password": "your-password",
      "sshPort": 2222,
      "role": "worker",
      "overlayProvider": "tailscale",
      "overlayConfig": "your-tailscale-auth-key-here",
      "glusterEnabled": true
    }
  ]
}
```

**SSH Connection Settings:**
- `hostname`: Hostname or IP address (required)
- `username`: SSH username (default: `root`)
- `password`: SSH password (use this OR `privateKeyPath`)
- `privateKeyPath`: Path to SSH private key (use this OR `password`)
- `sshPort`: SSH port (default: `22`)

**Node Role Settings:**
- `primaryMaster`: Mark as primary master (exactly one required, must be a manager)
- `role`: `manager` or `worker` (required)

**Overlay Network Settings (per-node overrides):**
- `overlayProvider`: Override global overlay provider for this node
- `overlayConfig`: Provider-specific config:
  - **Netbird**: Setup key (e.g., `NB_SETUP_KEY`)
  - **Tailscale**: Auth key (e.g., `TS_AUTHKEY`)
  - **WireGuard**: Interface name or config path (e.g., `wg0` or `/etc/wireguard/wg0.conf`)

**GlusterFS Settings (per-node overrides):**
- `glusterEnabled`: Enable GlusterFS on this node (workers only)
- `glusterMount`: Override global mount path for this node
- `glusterBrick`: Override global brick path for this node

**Docker Swarm Settings:**
- `advertiseAddr`: Override auto-detected advertise address for Swarm

### SSH Multi-Session Support

The deployer uses **parallel SSH sessions** for maximum performance:
- All nodes are configured **simultaneously** using goroutines
- Each node gets its own SSH connection from the pool
- Operations like dependency installation, overlay setup, and GlusterFS configuration run in parallel
- This dramatically reduces deployment time for large clusters

The SSH pool (`internal/ssh/pool.go`) manages connections efficiently:
- Connections are created on-demand and reused
- Each host can have different authentication credentials
- Thread-safe with mutex protection
- `RunAll()` method executes commands on multiple hosts in parallel

## Features

- **Swarm master orchestration**
  - Initialise a Swarm manager and optional GlusterFS state paths.
  - Run a controller server that coordinates nodes via JSON-over-TCP.
- **Node convergence**
  - Nodes register with the controller and are converged onto the desired
    state (Swarm role, overlay provider, GlusterFS participation).
- **Overlay providers**
  - Netbird (`netbird`)
  - Tailscale (`tailscale`)
  - WireGuard (`wireguard`)
  - Or no overlay (`none`)
- **GlusterFS support**
  - Optional brick preparation, volume creation, and mounts.
- **Auto-installation of dependencies**
  - Docker and Docker Compose (`docker` CLI plugin and/or `docker-compose`).
  - Netbird, Tailscale, WireGuard tools.
  - GlusterFS client utilities.

## Legacy Mode: Node-Agent Deployment

**Note:** This mode is deprecated. Use the `deploy` command with JSON config instead.

On a fresh Linux host that can reach your Netbird/Tailscale/WireGuard network,
you can get to the binaries and start the primary master controller with
GlusterFS state/brick/mount paths wired up by default:

```bash
git clone https://github.com/Grace-Solutions/Docker-Swarm-Cluster-Configuration-Service.git && \
  cd ./Docker-Swarm-Cluster-Configuration-Service && \
  chmod -R -v +x ./ && \
  cd ./binaries && \
  clear && \
  ./cluster-master-init.sh \
    --primary-master \
    --enable-glusterfs \
    --listen 0.0.0.0:7000 \
    --min-managers 3 \
    --min-workers 6 \
    --wait-for-minimum
```

One-line version:

```bash
git clone https://github.com/Grace-Solutions/Docker-Swarm-Cluster-Configuration-Service.git && cd ./Docker-Swarm-Cluster-Configuration-Service && chmod -R -v +x ./ && cd ./binaries && clear && ./cluster-master-init.sh --primary-master --enable-glusterfs --listen 0.0.0.0:7000 --min-managers 3 --min-workers 6 --wait-for-minimum
```

With `--enable-glusterfs` and the default `--state-dir`:

- **State dir (controller + data mount):** `/mnt/GlusterFS/Docker/Swarm/0001/data`
- **Brick dir (where Gluster bricks live on worker nodes):** `/mnt/GlusterFS/Docker/Swarm/0001/brick`
- **Volume name:** `0001` (derived from the parent directory name)

The advertise address is automatically detected using IP priority: overlay (CGNAT) > private (RFC1918) > other non-loopback > loopback.

### Quickstart: additional manager node (Linux)

On another Linux host that should participate as a Swarm **manager**:

```bash
git clone https://github.com/Grace-Solutions/Docker-Swarm-Cluster-Configuration-Service.git && \
  cd ./Docker-Swarm-Cluster-Configuration-Service && \
  chmod -R -v +x ./ && \
  cd ./binaries && \
  clear && \
  ./cluster-node-join.sh \
    --master <PRIMARY_MANAGER_IP>:7000 \
    --role manager \
    --overlay-provider netbird \
    --overlay-config <NETBIRD_SETUP_KEY>
```

One-line version:

```bash
git clone https://github.com/Grace-Solutions/Docker-Swarm-Cluster-Configuration-Service.git && cd ./Docker-Swarm-Cluster-Configuration-Service && chmod -R -v +x ./ && cd ./binaries && clear && ./cluster-node-join.sh --master <PRIMARY_MANAGER_IP>:7000 --role manager --overlay-provider netbird --overlay-config <NETBIRD_SETUP_KEY>
```

### Quickstart: worker node (Linux)

On a Linux host that should run Swarm **worker** tasks:

```bash
git clone https://github.com/Grace-Solutions/Docker-Swarm-Cluster-Configuration-Service.git && \
  cd ./Docker-Swarm-Cluster-Configuration-Service && \
  chmod -R -v +x ./ && \
  cd ./binaries && \
  clear && \
  ./cluster-node-join.sh \
    --master <PRIMARY_MANAGER_IP>:7000 \
    --role worker \
    --overlay-provider netbird \
    --overlay-config <NETBIRD_SETUP_KEY> \
    --enable-glusterfs \
    --deploy-portainer
```

One-line version:

```bash
git clone https://github.com/Grace-Solutions/Docker-Swarm-Cluster-Configuration-Service.git && cd ./Docker-Swarm-Cluster-Configuration-Service && chmod -R -v +x ./ && cd ./binaries && clear && ./cluster-node-join.sh --master <PRIMARY_MANAGER_IP>:7000 --role worker --overlay-provider netbird --overlay-config <NETBIRD_SETUP_KEY> --enable-glusterfs --deploy-portainer
```

Replace `<NETBIRD_SETUP_KEY>` with your Netbird setup key (or switch
`--overlay-provider` / `--overlay-config` to match your chosen overlay).

**Note**: The `--deploy-portainer` flag deploys Portainer CE and Portainer Agent as Docker Swarm services. Portainer will be accessible at `https://<any-node-ip>:9443` via the routing mesh. Only specify this flag on **one worker node** to avoid duplicate deployments.

## CLI overview

The main entry points are:

- `clusterctl master init [flags]`
- `clusterctl master serve [flags]`
- `clusterctl master reset [flags]`
- `clusterctl node join [flags]`
- `clusterctl node reset [flags]`

Run `clusterctl help` or any command with `-h`/`--help` for detailed flags.

## Linux wrapper scripts

For convenience, Linux wrapper scripts live under `./binaries` and execute
pre-built `clusterctl` binaries relative to the script directory:

- `cluster-master-init.sh` wraps `clusterctl master init`.
- `cluster-master-serve.sh` wraps `clusterctl master serve` (listen/server
  mode).
- `cluster-node-join.sh` wraps `clusterctl node join` (node/client mode).

Each script:

- Detects the architecture via `uname -m`.
- Selects `clusterctl-linux-amd64` or `clusterctl-linux-arm64` from the
  `binaries/` directory.
- Passes through all additional arguments to the underlying `clusterctl`
  subcommand.

See `binaries/README.md` for examples and usage notes.

## Building

From the repository root:

- Linux/amd64:

  ```bash
  GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o binaries/clusterctl-linux-amd64 ./cmd/clusterctl
  ```

- Linux/arm64:

  ```bash
  GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o binaries/clusterctl-linux-arm64 ./cmd/clusterctl
  ```

- Windows/amd64:

  ```bash
  GOOS=windows GOARCH=amd64 go build -o binaries/clusterctl-windows-amd64.exe ./cmd/clusterctl
  ```

Pre-built binaries for these targets are tracked under `./binaries`.

## Documentation

- `GO-IMPLEMENTATION-SPEC.md` – the original design and behavioural spec.
- `docs/README.md` – higher-level architecture and CLI overview.
- `binaries/README.md` – documentation for the Linux wrapper scripts and
  binaries.

## Logging

`clusterctl` writes plain-text log lines in the format:

```text
[2025-01-01T12:00:00Z] - [INFO] - message
```

- Logs are emitted to **stderr** and to a log file named `clusterctl.log` in the
  current working directory by default.
- Override the log file path via `CLUSTERCTL_LOG_FILE`.
- Control the minimum log level via `CLUSTERCTL_LOG_LEVEL`
  (e.g. `debug`, `info`, `warn`, `error`; default is `info`).

Controller and node logs include detailed Swarm and GlusterFS events after each
join so you can see which token was used, which Swarm cluster the node joined,
and the current GlusterFS volume/mount status on that node.

## Notes

- The Go implementation is idempotent: commands like `node join` and
  `master init` are safe to re-run and converge the system onto the desired
  state.
- Overlay provider config is passed as a **string** via `--overlay-config` and
  mapped to provider-specific environment variables (e.g. `NB_SETUP_KEY` for
  Netbird, `TS_AUTHKEY` for Tailscale).
- Dependency installers (`internal/deps`) make a best-effort to support
  multiple Linux distributions, with Ubuntu/Debian (`apt-get`) given
  precedence.
