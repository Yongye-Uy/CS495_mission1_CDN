#!/usr/bin/env bash
# Docker/setup-test-ssh.sh
# Deterministic SSH key setup script for local Docker testing.
# Generates API client key pair and 3 distinct storage container host keys.
# Pre-populates Docker/api/cdn_known_hosts for strict host-key verification.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
KEYS_DIR="${SCRIPT_DIR}/keys"
API_DIR="${SCRIPT_DIR}/api"

echo "=== Initializing Docker Local Test SSH Infrastructure ==="
mkdir -p "${KEYS_DIR}" "${API_DIR}"

# 1. Generate API Client SSH Key (ed25519)
CLIENT_KEY="${KEYS_DIR}/storage_test_key"
if [ ! -f "${CLIENT_KEY}" ]; then
    echo "[*] Generating API Client SSH Key pair (${CLIENT_KEY})..."
    ssh-keygen -t ed25519 -f "${CLIENT_KEY}" -N "" -C "api-client-test"
    chmod 600 "${CLIENT_KEY}"
    chmod 644 "${CLIENT_KEY}.pub"
else
    echo "[*] API Client SSH Key already exists: ${CLIENT_KEY}"
fi

# 2. Generate Distinct Host Keys for each Storage Container
NODES=("sg" "us" "lon")
for NODE in "${NODES[@]}"; do
    HOST_KEY="${KEYS_DIR}/ssh_host_${NODE}_ed25519_key"
    if [ ! -f "${HOST_KEY}" ]; then
        echo "[*] Generating Host Key for storage-${NODE} (${HOST_KEY})..."
        ssh-keygen -t ed25519 -f "${HOST_KEY}" -N "" -C "host-storage-${NODE}"
        chmod 600 "${HOST_KEY}"
        chmod 644 "${HOST_KEY}.pub"
    else
        echo "[*] Host Key for storage-${NODE} already exists: ${HOST_KEY}"
    fi
done

# 3. Pre-populate api/cdn_known_hosts with container hostnames and exact host keys
KNOWN_HOSTS="${API_DIR}/cdn_known_hosts"
echo "[*] Generating ${KNOWN_HOSTS}..."
rm -f "${KNOWN_HOSTS}"

for NODE in "${NODES[@]}"; do
    PUB_KEY_CONTENT=$(cat "${KEYS_DIR}/ssh_host_${NODE}_ed25519_key.pub" | awk '{print $1 " " $2}')
    # Map Docker container name and network alias
    echo "storage-${NODE} ${PUB_KEY_CONTENT}" >> "${KNOWN_HOSTS}"
    echo "storage-${NODE}.cdn-net ${PUB_KEY_CONTENT}" >> "${KNOWN_HOSTS}"
    echo "storage-${NODE},127.0.0.1 ${PUB_KEY_CONTENT}" >> "${KNOWN_HOSTS}"
done
chmod 644 "${KNOWN_HOSTS}"

echo "=== SSH Test Setup Complete! ==="
echo "Private keys stored in: Docker/keys/ (excluded via .gitignore)"
echo "Known hosts written to: Docker/api/cdn_known_hosts (excluded via .gitignore)"
