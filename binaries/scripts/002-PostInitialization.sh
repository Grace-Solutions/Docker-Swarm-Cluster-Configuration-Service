#!/bin/bash
# 002-PostInitialization.sh - Runs after all services are deployed
# Environment variables available:
#   STORAGE_MOUNT_PATH     - Base storage mount path (e.g., /mnt/MicroCephFS/docker-swarm-0001)
#   SERVICE_DATA_DIR       - Service data subdirectory name (e.g., "data")
#   SERVICE_DEFINITIONS_DIR- Service definitions subdirectory name (e.g., "ServiceDefinitions")
#   PRIMARY_MASTER         - Primary master node hostname
#   HAS_DEDICATED_WORKERS  - "true" if cluster has dedicated workers
#   DISTRIBUTED_STORAGE    - "true" if distributed storage is enabled
#   NODE_HOSTNAME          - This node's hostname
#   DEPLOYED_SERVICES      - Comma-separated list of deployed stack names

set -e

echo "[PostInit] Starting post-initialization..."
echo "[PostInit] PRIMARY_MASTER: ${PRIMARY_MASTER}"
echo "[PostInit] DEPLOYED_SERVICES: ${DEPLOYED_SERVICES}"

# Exit early if no services were deployed
if [ -z "${DEPLOYED_SERVICES}" ]; then
    echo "[PostInit] No services deployed, skipping post-initialization"
    exit 0
fi

# Convert comma-separated list to array
IFS=',' read -ra SERVICES <<< "${DEPLOYED_SERVICES}"

echo "[PostInit] Checking ${#SERVICES[@]} deployed service(s)..."

# Wait for services to stabilize
STABILIZATION_DELAY=15
echo "[PostInit] Waiting ${STABILIZATION_DELAY}s for services to stabilize..."
sleep ${STABILIZATION_DELAY}

# Check each deployed stack
for STACK_NAME in "${SERVICES[@]}"; do
    echo ""
    echo "[PostInit] ============================================"
    echo "[PostInit] Stack: ${STACK_NAME}"
    echo "[PostInit] ============================================"
    
    # Get services in this stack
    echo "[PostInit] → Service status:"
    docker service ls --filter "label=com.docker.stack.namespace=${STACK_NAME}" --format "table {{.Name}}\t{{.Mode}}\t{{.Replicas}}\t{{.Image}}"
    
    # Get all service names in this stack
    SERVICE_NAMES=$(docker service ls --filter "label=com.docker.stack.namespace=${STACK_NAME}" --format "{{.Name}}")
    
    for SVC_NAME in ${SERVICE_NAMES}; do
        echo ""
        echo "[PostInit] → Logs for ${SVC_NAME} (last 10 lines):"  
        docker service logs --tail 10 "${SVC_NAME}" 2>&1 || echo "[PostInit] (no logs available yet)"
        echo ""
        echo ""
        docker service ps "${SVC_NAME}" --no-trunc 2>&1 || echo "[PostInit] (no service deployment issues detected)"
        echo ""
        echo ""
    done
done

echo ""
echo "[PostInit] ✅ Post-initialization complete"

