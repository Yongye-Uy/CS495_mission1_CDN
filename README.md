# Mission 1 — Distributed File Storage / Mini CDN

A distributed file storage and content delivery network (Mini CDN) built in idiomatic **Go** using **Gin**, designed for university Distributed Systems coursework.

---

## 1. System Overview & Architecture

The architecture consists of **two production applications** plus **native Linux storage instances**:

1. **Website Frontend (`Website/`):**
   - High-performance Go Gin application serving the web dashboard, file index, preview modals, and administrative interface.
   - Admin authentication with bcrypt password verification, rate limiting, HttpOnly/SameSite session cookies (`cdn_admin_session`), and CSRF token protection.
   - Built-in HTTPS reverse proxy that securely injects the `X-Admin-Key` header server-to-server.
2. **Central API Controller (`API + MongoDB/`):**
   - Central coordinator orchestrating file metadata, GeoIP proximity routing, replication management, and background recovery.
   - Persistent storage in **MongoDB** configured as a single-node replica set (`rs0`) supporting multi-document atomic transactions.
   - Replicated file mutations tracked via an append-only **Mutation Journal** (`mutation_journal` collection).
   - In-memory read-write **Mutation Barrier** ensuring consistency during node synchronization.
3. **Storage Servers (Native Linux / AWS Lightsail):**
   - Ordinary Linux machines running standard **OpenSSH**, **SFTP**, and **rsync** under a dedicated `cdn-sync` user with `/var/cdn/data` (`chmod 750`).
   - **Zero custom Go code on the storage servers** — all file transfers, audits, and deletions use standard OpenSSH/SFTP/rsync protocols.
   - Strict ED25519 host-key verification enforced via `known_hosts`.

```text
                                CLIENT / BROWSER
                                       │
                        ┌──────────────┴──────────────┐
                        │                             │
                        ▼                             ▼
             ┌─────────────────────┐       ┌─────────────────────┐
             │    Website (Gin)    │       │    on_board (CLI)   │
             │      Port 3000      │       │     Go CLI Tool     │
             │  • Admin Session    │       │  • CENTRAL_API_     │
             │  • CSRF Protection  │       │    ADMIN_KEY Auth   │
             └──────────┬──────────┘       └──────────┬──────────┘
                        │                             │
                        │ HTTP / JSON                 │ HTTP / JSON
                        │ (Injected X-Admin-Key)      │ (X-Admin-Key Header)
                        └──────────────┬──────────────┘
                                       │
                                       ▼
                        ┌─────────────────────────────┐
                        │      Central API (Gin)      │
                        │          Port 8080          │
                        │  ─────────────────────────  │
                        │  • Atomic Mongo Transactions│
                        │  • Mutation Barrier RWLock  │
                        │  • 3/3 Strict Replication   │
                        │  • Background Quorum Worker │
                        │  • GeoIP Proximity Routing  │
                        └──────────────┬──────────────┘
                                       │
                        ┌──────────────┴──────────────┐
                        │                             │
                        ▼                             ▼
             ┌─────────────────────┐       ┌─────────────────────┐
             │    MongoDB (rs0)    │       │ Storage Nodes (SSH) │
             │      Port 27017     │       │ • Singapore (sg)    │
             │  ─────────────────  │       │ • US (us)           │
             │  • files            │       │ • London (lon)      │
             │  • storage_nodes    │       │  ─────────────────  │
             │  • mutation_journal │       │  • OpenSSH / SFTP   │
             │  • sync_jobs        │       │  • rsync over SSH   │
             │  • atomic_counters  │       │  • /var/cdn/data    │
             └─────────────────────┘       └─────────────────────┘
```

---

## 2. Core Distributed Systems Protocols

### A. Strict 3/3 Complete Replication & Quorum
- When a file is uploaded, it streams directly to `/var/lib/mission1/staging/<file_id>` while computing SHA-256.
- The upload sequence, mutation journal entry, and file document are committed atomically in MongoDB.
- Parallel rsync-over-SSH replicates the file across all 3 storage servers:
  - **3/3 Replicas Succeeded:** Status becomes `COMPLETE`. The local staged file is safely deleted.
  - **1/3 or 2/3 Succeeded:** Status becomes `PARTIAL` (`quorumMet: true` if $\ge 2$). The local staged file is **retained on disk**.
  - **0/3 Succeeded:** Status becomes `FAILED`.
- A background **Replication Recovery Worker** continuously scans for `PARTIAL` files and heals incomplete nodes using the retained staged file or healthy replicas.

### B. Two-Phase Synchronization Protocol (Node Onboarding)
When onboarding a new storage node:
1. **Pre-flight Check:** Strict SSH host-key verification and `/var/cdn/data` accessibility probe.
2. **Phase 1 (Snapshot):** Captures current sequence $S_{\text{start}}$. Copies all active catalog files to the new node via rsync.
3. **Phase 2 (Catch-up Loop):** Reads and replays mutations where $\text{seq} > S_{\text{start}}$.
4. **Final Barrier:** Acquires exclusive `MutationBarrier.Lock()`, replays the final journal tail, audits the remote filesystem via SFTP, commits node status to `ONLINE` in MongoDB, and releases the barrier.
5. **Zero Zombie Files:** Deleted files are never resurrected on the new node.

---

## 3. Local Development & Docker Testing

For local testing, Docker simulates the 3 isolated storage servers and replica set:

```bash
# 1. Generate local test SSH keypairs and cdn_known_hosts
cd Docker
./setup-test-ssh.sh       # On Linux/macOS
# or:
.\setup-test-ssh.ps1      # On Windows PowerShell

# 2. Start the local cluster
docker compose up -d --build

# 3. Access Web Dashboard
# Open http://localhost:3000
```

---

## 4. Administrative Security

- **Central API Key:** Set via `CENTRAL_API_ADMIN_KEY` or `ADMIN_API_KEY`.
- **Website Admin Sign-In:** Accessible at `/admin/login`. Guarded by bcrypt password verification and rate limiting (max 5 failed attempts per minute).
- **Session Security:** `cdn_admin_session` cookie signed via HMAC-SHA256 with `HttpOnly`, `SameSite: Strict`, and 24-hour expiration.
- **CSRF Protection:** Tokens validated on all mutation requests.

---

## 5. CLI Usage (`on_board`)

```bash
export API_URL="http://localhost:8080"
export CENTRAL_API_ADMIN_KEY="your-secret-admin-key"

# Add a storage node
./on_board addnode ip=18.219.221.233 region=US country="United States"

# List storage nodes
./on_board listnodes

# Remove a storage node
./on_board removenode <node-id>

# Check system health
./on_board health
```
