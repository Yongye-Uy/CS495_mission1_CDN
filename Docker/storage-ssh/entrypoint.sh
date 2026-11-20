#!/usr/bin/env bash
# Docker/storage-ssh/entrypoint.sh
# Configures node-specific host key and API client authorized_keys at container startup

set -euo pipefail

NODE="${CONTAINER_NODE_NAME:-sg}"
echo "[STORAGE-SSH] Initializing storage node container (Node: ${NODE})..."

KEYS_DIR="/keys"
HOST_KEY_SRC="${KEYS_DIR}/ssh_host_${NODE}_ed25519_key"
CLIENT_PUB_SRC="${KEYS_DIR}/storage_test_key.pub"

# 1. Install container-specific host key
if [ -f "${HOST_KEY_SRC}" ]; then
    echo "[STORAGE-SSH] Installing container host key from ${HOST_KEY_SRC}..."
    cp "${HOST_KEY_SRC}" /etc/ssh/ssh_host_ed25519_key
    chmod 600 /etc/ssh/ssh_host_ed25519_key
    if [ -f "${HOST_KEY_SRC}.pub" ]; then
        cp "${HOST_KEY_SRC}.pub" /etc/ssh/ssh_host_ed25519_key.pub
        chmod 644 /etc/ssh/ssh_host_ed25519_key.pub
    fi
else
    echo "[STORAGE-SSH] Warning: Host key ${HOST_KEY_SRC} not found. Generating default ed25519 host key..."
    ssh-keygen -t ed25519 -f /etc/ssh/ssh_host_ed25519_key -N ""
    chmod 600 /etc/ssh/ssh_host_ed25519_key
fi

# 2. Install API client public key into authorized_keys for cdn-sync user
if [ -f "${CLIENT_PUB_SRC}" ]; then
    echo "[STORAGE-SSH] Installing API client public key into /home/cdn-sync/.ssh/authorized_keys..."
    cat "${CLIENT_PUB_SRC}" > /home/cdn-sync/.ssh/authorized_keys
    chmod 600 /home/cdn-sync/.ssh/authorized_keys
    chown -R cdn-sync:cdn-sync /home/cdn-sync/.ssh
else
    echo "[STORAGE-SSH] Warning: Client public key ${CLIENT_PUB_SRC} not found!"
fi

# 3. Ensure permissions on storage data directory
mkdir -p /var/cdn/data
chown -R cdn-sync:cdn-sync /var/cdn/data
chmod 750 /var/cdn/data

echo "[STORAGE-SSH] Starting sshd daemon on port 22..."
exec /usr/sbin/sshd -D -e
