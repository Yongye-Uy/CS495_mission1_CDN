# Mission 1 — Distributed File Storage / Mini CDN

A distributed file storage and content delivery network (Mini CDN) built in idiomatic **Go** using **Gin**, designed for university Distributed Systems coursework.

---

## 1. Project Overview

The system replicates files across distributed storage worker nodes and dynamically serves files to clients from the geographically nearest healthy storage node using great-circle Haversine routing with automatic failover.

### UPDATE 5 Key Additions:
- **Go Gin Web & API Frameworks:** Both Website and Central API use high-performance **Gin** engines.
- **Dynamic Worker Onboarding:** Central API no longer relies on hardcoded storage nodes. New storage workers can be onboarded dynamically at runtime via CLI or Web UI.
- **Worker Verification & State Machine:** Onboarding strictly verifies worker health (`GET /health`) before activation:
  $$\text{PENDING} \longrightarrow \text{Worker Health Check} \longrightarrow \text{SYNCING} \longrightarrow \text{rsync Files} \longrightarrow \text{ONLINE}$$
- **MongoDB Node Registry:** Active and pending storage workers are tracked in the `storage_nodes` collection in MongoDB.
- **Safe rsync File Synchronization:** When onboarding a new worker, existing files are synchronized using `rsync` (with automatic fallback to HTTP stream sync if rsync daemon is not running).
- **Dedicated CLI Tool (`on_board`):** A standalone Go command-line tool communicating over HTTP with the Central API:
  ```bash
  ./on_board addnode ip=198.45.98.44 region=Asia [country=Japan] [port=8081] [lat=35.67] [lon=139.65]
  ./on_board listnodes
  ./on_board removenode <node-id>
  ./on_board health
  ```
- **Storage Node Management UI:** Stock Bootstrap 5 interface with:
  - `/files`: Distributed file index, drag-and-drop upload, geolocation routing debugger.
  - `/nodes`: Active storage workers table with live status badges, health check, and remove actions.
  - `/nodes/add`: Onboarding form with demo quick-fill presets and step-by-step state progress feedback.
  - Color palette: Simple Black & White, **Blue** for Confirm Upload, **Red** for Cancel and Delete, real Bootstrap Icons (no emojis).

---

## 2. System Architecture

```text
                                CLIENT / BROWSER
                                       │
                        ┌──────────────┴──────────────┐
                        │                             │
                        ▼                             ▼
             ┌─────────────────────┐       ┌─────────────────────┐
             │    Website (Gin)    │       │    on_board (CLI)   │
             │      Port 3000      │       │     Go CLI Tool     │
             └──────────┬──────────┘       └──────────┬──────────┘
                        │                             │
                        │ HTTP / JSON                 │ HTTP / JSON
                        └──────────────┬──────────────┘
                                       │
                                       ▼
                         ┌───────────────────────────┐
                         │     Central API (Gin)     │
                         │         Port 8080         │
                         │  ───────────────────────  │
                         │  • Node Management        │
                         │  • Dynamic Geo-Routing    │
                         │  • Fanout Replication     │
                         │  • Worker Health Probing  │
                         └─────────────┬─────────────┘
                                       │
                        ┌──────────────┴──────────────┐
                        │                             │
                        ▼                             ▼
             ┌─────────────────────┐       ┌─────────────────────┐
             │       MongoDB       │       │   Existing Worker   │
             │     Port 27017      │       │ (e.g. Singapore)    │
             │  ─────────────────  │       └──────────┬──────────┘
             │  • storage_nodes    │                  │
             │  • files (metadata) │                  │ rsync daemon
             └─────────────────────┘                  │ (:873)
                                                      ▼
                                           ┌─────────────────────┐
                                           │   New Storage Node  │
                                           │   (e.g. Tokyo/Asia) │
                                           │    Port 8081 / 8084 │
                                           └─────────────────────┘
```

---

## 3. Project Structure

```text
Mission 1 CDN/
├── Website/                        # Web Dashboard (Go + Gin)
│   ├── cmd/main.go                 # Gin router, template & static server, API reverse proxy
│   ├── internal/
│   │   ├── config/config.go        # Port and API URL configuration
│   │   └── handlers/               # Gin web handlers & proxy
│   ├── static/
│   │   ├── app.js                  # Main dashboard script (files, upload, geo inspection)
│   │   ├── nodes.js                # Storage nodes table & health probe script
│   │   ├── add_node.js             # Add node form with live step-by-step onboarding
│   │   └── style.css               # Clean Stock Bootstrap 5 styling (B&W theme)
│   ├── templates/
│   │   ├── index.html              # Main dashboard template
│   │   ├── nodes.html              # Storage nodes management template
│   │   └── add_node.html           # Add storage node template
│   └── go.mod
│
├── API + MongoDB/                  # Central API Controller (Go + Gin + MongoDB)
│   ├── cmd/main.go                 # Central coordinator entry point, Gin routing, seed loader
│   ├── handlers/
│   │   ├── file_handler.go         # File upload, list, download, view, location debug
│   │   ├── node_handler.go         # Dynamic node onboarding, list, health, delete
│   │   └── *_test.go               # Gin handler unit tests
│   ├── internal/config/config.go   # MongoDB URI, initial storage nodes, upload limits
│   ├── models/
│   │   ├── file.go                 # FileMetadata, Replica, SystemStatus models
│   │   └── node.go                 # StorageNode model and status constants
│   ├── repository/
│   │   ├── file_repository.go      # Mongo and Mock file metadata repository
│   │   └── node_repository.go      # Mongo and Mock storage node repository
│   ├── services/
│   │   ├── file_service.go         # Dynamic replication & nearest-node download
│   │   ├── node_service.go         # 6-step worker verification and onboarding pipeline
│   │   ├── sync_service.go         # Safe rsync execution and synchronization coordinator
│   │   ├── geo/geo.go              # Haversine distance calculation & dynamic node ranking
│   │   └── storage_client/         # HTTP storage worker communication client
│   └── go.mod
│
├── Storage/                        # Storage Worker Service (Go standard library + rsync daemon)
│   ├── cmd/main.go                 # HTTP server listening on configured port
│   ├── handlers/
│   │   ├── storage_handler.go      # /health, /storage/files, /storage/sync endpoints
│   │   └── storage_handler_test.go # Storage unit tests
│   ├── internal/config/config.go   # Location, NodeID, DataDir, Port configuration
│   ├── services/storage_service.go # File IO and worker-side rsync execution
│   ├── storage/
│   │   ├── disk.go                 # Safe disk storage engine with path traversal protection
│   │   └── disk_test.go            # Path traversal and isolation tests
│   └── go.mod
│
├── Docker/
│   ├── docker-compose.yml          # Single/simulated cluster with storage-new-node ready
│   ├── docker-compose.multi.yml    # Multi-node cluster (Singapore, New York, London + new-node)
│   ├── api/Dockerfile              # Multi-stage build for Central API with rsync
│   ├── storage/
│   │   ├── Dockerfile              # Storage image with Alpine, Go binary, and rsync
│   │   ├── rsyncd.conf             # Safe rsync daemon configuration for /data/files
│   │   └── entrypoint.sh           # Runs rsyncd daemon + storage binary
│   └── website/Dockerfile          # Multi-stage build for Website
│
├── cmd/on_board/                   # Standalone Onboarding CLI Tool
│   ├── main.go                     # CLI implementation for addnode, listnodes, removenode, health
│   └── go.mod
│
├── on_board.exe                    # Compiled CLI binary for Windows
├── on_board                        # Compiled CLI binary for Linux
└── README.md                       # This documentation
```

---

## 4. How Worker Onboarding Works (The 6-Step State Machine)

When a node is added via CLI (`./on_board addnode ip=...`) or Website form (`POST /nodes/add`):

1. **Input Validation:** Central API validates IP address, port (default 8081), region, country, and coordinates.
2. **Duplicate Check:** Prevents registering duplicate IP/Port or existing Node IDs.
3. **Worker Health Check:** Calls `GET http://<ip>:<port>/health`.
   - The worker must respond with `{"status":"ok","service":"storage",...}`.
   - If the worker is unreachable or not a storage service, registration is immediately rejected with HTTP 502/Bad Gateway.
4. **Register as PENDING:** An entry is inserted into MongoDB collection `storage_nodes` with `status: "PENDING"`.
5. **Data Synchronization (rsync):**
   - Central API selects an existing `ONLINE` node as the synchronization source.
   - Central API sets status to `SYNCING`.
   - Triggers `rsync -avz` to copy existing stored files from the source worker's directory to the new worker.
   - If rsync daemon is not present (e.g. non-containerized local dev), it automatically performs HTTP synchronization.
6. **Activate to ONLINE:** After file synchronization is verified, the node status is updated to `ONLINE`. The new node immediately participates in dynamic CDN replication and routing!

---

## 5. CLI Tool Usage (`on_board`)

The `on_board` executable is compiled in the root folder:

### 5.1 Add Storage Node
```bash
# Basic syntax
./on_board addnode ip=198.45.98.44 region=Asia

# Full parameters with coordinates and port
./on_board addnode ip=storage-new-node region=Asia country=Japan port=8081 lat=35.6762 lon=139.6503
```

### 5.2 List Registered Storage Nodes
```bash
./on_board listnodes
```
Output:
```text
=== REGISTERED STORAGE NODES (2) ===

NODE ID          IP                 PORT   REGION   COUNTRY     COORDS          STATUS     LAST HEARTBEAT
-------          --                 ----   ------   -------     ------          ------     --------------
node-singapore   storage            8081   Asia     Singapore   1.35, 103.82    [ONLINE]   20:08:09
node-tokyo       storage-new-node   8081   Asia     Japan       35.68, 139.65   [ONLINE]   20:08:09
```

### 5.3 Health Check & Status
```bash
./on_board health
```

### 5.4 Remove Storage Node
```bash
./on_board removenode node-tokyo
```

---

## 6. How to Run and Test

### Option A: Run via Docker Compose (Recommended)

1. Start the cluster:
   ```bash
   cd Docker
   docker compose up --build -d
   ```
2. Verify all 5 containers are running and healthy:
   - `cdn-website` (:3000)
   - `cdn-central-api` (:8080)
   - `cdn-mongodb` (:27017)
   - `cdn-storage-local` (:8081)
   - `cdn-storage-new-node` (:8084) — Ready for onboarding demo!
3. Open your browser at `http://localhost:3000`.
4. Upload test files (`A.jpg`, `B.jpg`).
5. Open `http://localhost:3000/nodes` and click **Add Node** (or use the quick preset "Tokyo Node (Docker)").
6. Watch the live onboarding state machine verify the worker, rsync the files, and activate it to `ONLINE`!
7. Or run from the command line:
   ```bash
   ./on_board addnode ip=storage-new-node region=Asia country=Japan port=8081 lat=35.6762 lon=139.6503
   ```

### Option B: Run Locally Without Docker (Direct Go Binaries)

1. **Start MongoDB:**
   Ensure MongoDB is running locally on `localhost:27017`.
2. **Start Storage Node #1 (Singapore):**
   ```bash
   cd Storage
   $env:STORAGE_LOCATION="singapore"
   $env:STORAGE_PORT="8081"
   $env:STORAGE_DATA_DIR="./data/singapore"
   go run ./cmd
   ```
3. **Start Central API:**
   ```bash
   cd "API + MongoDB"
   $env:API_PORT="8080"
   $env:MONGO_URI="mongodb://localhost:27017"
   $env:STORAGE_LOCAL_URL="http://localhost:8081"
   go run ./cmd
   ```
4. **Start Website:**
   ```bash
   cd Website
   $env:WEBSITE_PORT="3000"
   $env:API_BASE_URL="http://localhost:8080"
   go run ./cmd
   ```
5. **Start Storage Node #2 (Tokyo) for dynamic onboarding:**
   ```bash
   cd Storage
   $env:STORAGE_LOCATION="tokyo"
   $env:STORAGE_NODE_ID="node-tokyo"
   $env:STORAGE_PORT="8084"
   $env:STORAGE_DATA_DIR="./data/tokyo"
   go run ./cmd
   ```
6. **Onboard Node #2 via CLI:**
   ```bash
   ./on_board addnode ip=127.0.0.1 port=8084 region=Asia country=Japan lat=35.6762 lon=139.6503
   ```

---

## 7. Running Unit & Integration Tests

All test suites pass completely:

```bash
# Run Storage tests (path traversal protection, storage service)
cd Storage
go test -v ./...

# Run Central API tests (Gin handlers, dynamic geo-routing, node onboarding)
cd "../API + MongoDB"
go test -v ./...

# Run UPDATE 5 full end-to-end integration test
go test -v -run TestUpdate5_FullLifecycle ./services

# Run Website tests (Gin routes, templates, reverse proxy)
cd "../Website"
go test -v ./...
```
