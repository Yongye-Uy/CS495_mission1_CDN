# Mission 1 — Distributed File Storage / Mini CDN

A distributed file storage and content delivery network (Mini CDN) built in idiomatic **Go** using **Gin**, designed for university Distributed Systems coursework.

---

## 1. System Overview & Architecture

The architecture consists of **two production applications** plus **native Linux storage instances**:

1. **Website Frontend (`Website/`):**
   - High-performance Go Gin application serving the web dashboard, file index, preview modals, and storage node management interface.
   - Built-in reverse proxy that securely injects the `X-Admin-Key` header server-to-server for administrative node operations, keeping credentials completely hidden from client browsers.
2. **Central API Controller (`API + MongoDB/`):**
   - Central coordinator orchestrating file metadata, GeoIP proximity routing, and parallel replication.
   - Ephemeral upload buffering via `os.CreateTemp()` — uploads buffer temporarily for rsync and are automatically deleted upon upload completion.
   - Persistent storage in **MongoDB** configured with a single-node replica set (`rs0`).
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
             │  • File Dashboard   │       │  • CENTRAL_API_     │
             │  • Node Management  │       │    ADMIN_KEY Auth   │
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
                        │  • Ephemeral Upload Buffers │
                        │  • Mutation Barrier RWLock  │
                        │  • 3/3 Strict Replication   │
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
             └─────────────────────┘       │  • OpenSSH / SFTP   │
                                           │  • rsync over SSH   │
                                           │  • /var/cdn/data    │
                                           └─────────────────────┘
```

---

## 2. Core Distributed Systems Protocols

### A. Strict 3/3 Complete Replication & Quorum
- When a file is uploaded, it is buffered into an ephemeral temporary file on Central API disk while computing SHA-256 and size.
- Parallel rsync-over-SSH replicates the file across all active storage servers (Singapore, US, London):
  - **3/3 Replicas Succeeded:** Status becomes `COMPLETE` (HTTP `201 Created`).
  - **1/3 or 2/3 Succeeded:** Status becomes `PARTIAL` (HTTP `207 Multi-Status`, with `quorumMet: true` if $\ge 2$).
  - **0/3 Succeeded:** Status becomes `FAILED` (HTTP `502 Bad Gateway`).
- The ephemeral temporary file is immediately removed upon upload completion. Only verified replica nodes are recorded in MongoDB metadata.

### B. Catalog-Driven Node Synchronization
When onboarding a new storage node:
1. **Pre-flight Check:** Strict SSH host-key verification and `/var/cdn/data` accessibility probe. Node marked `SYNCING`.
2. **Phase 1 (Catalog Transfer):** Copies active catalog files from online replicas to the target node via rsync over SSH using ephemeral temp buffers.
3. **Phase 2 (Final Barrier & Reconciliation):** Acquires exclusive `MutationBarrier.Lock()`, reconciles any concurrent uploads or deletes, audits the remote filesystem via SFTP, removes any untracked or deleted files (ensuring zero zombie files), registers the new replica in MongoDB, and transitions the node to `ONLINE` before releasing the lock.

---

## 3. Server Setup Guides

Complete step-by-step setup guides for deploying to AWS Lightsail or Ubuntu VMs:

- [Server #1: Website Frontend Setup](file:///c:/Year4/CS%20495%20-%20Distributed%20Systems/Mission%201%20CDN/WEBSITE_SERVER_SETUP.md)
- [Server #2: Central API & MongoDB Setup](file:///c:/Year4/CS%20495%20-%20Distributed%20Systems/Mission%201%20CDN/API_SERVER_SETUP.md)
- [Servers #3, #4, #5: Storage Servers Setup](file:///c:/Year4/CS%20495%20-%20Distributed%20Systems/Mission%201%20CDN/STORAGE_SERVER_SETUP.md)

---

## 4. Administrative Security

- **Central API Key:** Set via `CENTRAL_API_ADMIN_KEY` on Website server and `ADMIN_API_KEY` on Central API server.
- **Server-to-Server Injection:** The Website backend transparently injects `X-Admin-Key` when proxying or sending node mutations to Central API. The secret is never exposed to browser JavaScript or HTML.
- **Node Management UI:** Direct access to `/nodes` and `/nodes/add` for ease of testing and grading.

---

## 5. CLI Usage (`on_board`)

You can also manage storage nodes via the CLI tool on the Central API server:

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
