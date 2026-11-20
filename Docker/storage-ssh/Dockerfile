FROM debian:bookworm-slim

# Install OpenSSH server, SFTP server, rsync, and utilities
RUN apt-get update && apt-get install -y --no-install-recommends \
    openssh-server \
    openssh-sftp-server \
    rsync \
    bash \
    ca-certificates \
    procps \
    net-tools \
    && rm -rf /var/lib/apt/lists/*

# Create unprivileged cdn-sync system user and storage directory
RUN useradd -m -s /bin/bash cdn-sync && \
    mkdir -p /home/cdn-sync/.ssh && \
    chmod 700 /home/cdn-sync/.ssh && \
    chown -R cdn-sync:cdn-sync /home/cdn-sync && \
    mkdir -p /var/cdn/data && \
    chown -R cdn-sync:cdn-sync /var/cdn/data && \
    chmod 750 /var/cdn/data && \
    mkdir -p /run/sshd && \
    chmod 0755 /run/sshd

# Configure OpenSSH for strict key-based authentication
RUN echo "Port 22" > /etc/ssh/sshd_config.d/cdn-storage.conf && \
    echo "PermitRootLogin no" >> /etc/ssh/sshd_config.d/cdn-storage.conf && \
    echo "PasswordAuthentication no" >> /etc/ssh/sshd_config.d/cdn-storage.conf && \
    echo "PubkeyAuthentication yes" >> /etc/ssh/sshd_config.d/cdn-storage.conf && \
    echo "AuthorizedKeysFile /home/cdn-sync/.ssh/authorized_keys" >> /etc/ssh/sshd_config.d/cdn-storage.conf && \
    echo "HostKey /etc/ssh/ssh_host_ed25519_key" >> /etc/ssh/sshd_config.d/cdn-storage.conf && \
    echo "Subsystem sftp /usr/lib/openssh/sftp-server" >> /etc/ssh/sshd_config.d/cdn-storage.conf

COPY Docker/storage-ssh/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

EXPOSE 22

VOLUME ["/var/cdn/data"]

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
