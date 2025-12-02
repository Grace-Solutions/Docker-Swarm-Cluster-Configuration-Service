# Docker Swarm Cluster Configuration Service – Documentation

This folder contains documentation for the `dswrmctl` (Docker Swarm Control) Go-based orchestrator
and its Linux wrapper scripts.

## Contents

- `README.md` (this file) – overview of the system, architecture, and usage.
- `../GO-IMPLEMENTATION-SPEC.md` – original implementation spec and design
  notes.
- `../binaries/README.md` – documentation for the Linux wrapper scripts under
  `binaries/`.

## High-level architecture

The orchestrator is a **single Go binary** (`dswrmctl`) with the primary mode being
JSON configuration-based deployment:

- `dswrmctl -config <config.json>` – deploy a complete Docker Swarm cluster with
  MicroCeph distributed storage from a JSON configuration file.
- `dswrmctl -config <config.json> -teardown` – teardown the cluster.

Legacy modes (deprecated):
- `dswrmctl master init` – prepare a host as the initial Swarm manager.
- `dswrmctl master serve` – run the controller server.
- `dswrmctl node join` – register a node and converge it onto the desired state.

Key internal packages:

- `internal/controller` – master-side TCP server, state management, and
  minimum-node gating.
- `internal/nodeagent` – node-side join/reset logic.
- `internal/overlay` – overlay provider abstraction for Netbird, Tailscale,
  WireGuard, or `none`.
- `internal/swarm` – Docker Swarm initialisation and join.
- `internal/storage` – distributed storage provider abstraction (MicroCeph).
- `internal/deps` – helper functions to ensure Docker, docker-compose,
  Netbird, Tailscale, WireGuard, and MicroCeph are installed.
- `internal/ipdetect` – IP auto-detection with CGNAT and RFC1918 preference.
- `internal/logging` – simple text logger that writes `[utc-timestamp] - [LEVEL] - message` lines to stderr and an optional log file, with level controlled by `DSWRMCTL_LOG_LEVEL` and file path by `DSWRMCTL_LOG_FILE`.

## Quickstart

The recommended approach is to use JSON configuration-based deployment:

```bash
# 1. Clone the repository
git clone https://github.com/Grace-Solutions/Docker-Swarm-Cluster-Configuration-Service.git
cd Docker-Swarm-Cluster-Configuration-Service

# 2. Create your configuration file
cp binaries/dswrmctl.json.example dswrmctl.json
# Edit dswrmctl.json with your nodes and settings

# 3. Deploy the cluster
./binaries/dswrmctl-linux-amd64 -config dswrmctl.json
```

See `binaries/dswrmctl.json.example` for a complete configuration example with:
- Node definitions (managers and workers)
- Overlay network configuration (Netbird, Tailscale, WireGuard)
- MicroCeph distributed storage settings
- Pre/post deployment scripts

## CLI overview

Main commands:

- `dswrmctl -config <config.json>` – deploy cluster from JSON configuration
- `dswrmctl -config <config.json> -teardown` – teardown cluster
- `dswrmctl -config <config.json> -dry-run` – validate configuration without deploying
- `dswrmctl -version` – show version information
- `dswrmctl -help` – show help

For full behaviour and field-level semantics, see
`../GO-IMPLEMENTATION-SPEC.md`.

## Logging and troubleshooting

`dswrmctl` logs are plain text lines in the format:

```text
[2025-01-01T12:00:00Z] - [INFO] - message
```

- By default, logs go to **stderr** and to a log file named `dswrmctl.log` in the
  current working directory.
- You can override the log file path via `DSWRMCTL_LOG_FILE`.
- You can control the minimum log level via `DSWRMCTL_LOG_LEVEL`
  (e.g. `debug`, `info`, `warn`, `error`; default is `info`).

Controller and node logs include detailed Swarm and storage events so you can
trace exactly how each node joined, which token was used, and what the current
storage status is.

## Linux wrapper scripts

For convenience, Linux wrapper scripts live under `../binaries` and execute
pre-built binaries relative to the script directory:

- `cluster-master-init.sh` – wraps `dswrmctl master init`.
- `cluster-master-serve.sh` – wraps `dswrmctl master serve`.
- `cluster-node-join.sh` – wraps `dswrmctl node join`.

Each script:

- Detects `uname -m` and selects `dswrmctl-linux-amd64` or
  `dswrmctl-linux-arm64` accordingly.
- Runs the appropriate `dswrmctl` subcommand, passing through any
  additional CLI flags.

Refer to `../binaries/README.md` for usage examples and notes about
permissions and environment variables (e.g. Netbird/Tailscale setup keys).

