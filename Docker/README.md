# Docker Local Testing Guide — Mini CDN

This guide provides step-by-step instructions for testing the **Distributed File Storage / Mini CDN** system locally using Docker Compose on Windows.

---

## 1. Required Software

Before starting, ensure you have installed:
1. **Docker Desktop for Windows** (with WSL2 backend enabled).
2. **Go** (version 1.22+ if testing or compiling locally outside Docker).
3. **PowerShell** or Windows Terminal.

Verify Docker is running:
```powershell
docker --version
docker compose version
```

---

## 2. Architecture Overview

### Single-Storage Local Test (Default)
```text
Browser / Client (http://localhost:3000)
    │
    ▼
[ Website ] (Port 3000)
    │
    ▼ HTTP
[ Central API ] (Port 8080)
    ├── [ MongoDB ] (Port 27017)  --> stores metadata
    └── [ Storage ] (Port 8081)    --> stores file on physical disk (/data/files)
```

---

## 3. How to Build & Start the System

Open PowerShell, navigate to the `Docker` directory, and launch the containers:

```powershell
# Navigate into the Docker directory
cd "c:\Year4\CS 495 - Distributed Systems\Mission 1 CDN\Docker"

# Build and start all 4 services in the background
docker compose up --build -d
```

Check the health and status of running containers:
```powershell
docker compose ps
```
You should see:
- `cdn-website` (healthy) on port 3000
- `cdn-central-api` (healthy) on port 8080
- `cdn-mongodb` (healthy) on port 27017
- `cdn-storage-local` (healthy) on port 8081

To view live aggregated logs:
```powershell
docker compose logs -f
```

---

## 4. How to Open the Website Dashboard

Open your web browser and navigate to:
```text
http://localhost:3000
```

You will see:
- **System Health Bar:** Showing Website, Central API, MongoDB, and Storage nodes all **ONLINE**.
- **Client Simulation Selector:** Dropdown for Cambodia (KH), USA (US), UK (GB), or Auto.
- **Drag-and-Drop Dropzone:** For uploading files.
- **Distributed File Index:** Showing all uploaded files and replica nodes.

---

## 5. How to Upload a File

### Method A: Through the Web Dashboard
1. Drag and drop any image, PDF, or text file into the upload box on `http://localhost:3000`.
2. Click **🚀 Upload & Replicate**.
3. The page displays a confirmation with the generated unique file ID (e.g., `f_1794883277_abc123`).

### Method B: Via PowerShell / curl
```powershell
# Create a test file
"Hello Distributed Systems CDN!" | Out-File -Encoding utf8 testfile.txt

# Upload to Central API
curl.exe -F "file=@testfile.txt" http://localhost:8080/api/files
```

Sample JSON response:
```json
{
  "success": true,
  "file": {
    "id": "f_1700000000_a1b2c3d4",
    "originalName": "testfile.txt",
    "contentType": "text/plain",
    "size": 32,
    "createdAt": "2026-11-17T09:00:00Z",
    "replicas": [
      {
        "location": "local",
        "url": "http://storage:8081"
      }
    ]
  },
  "replication": {
    "success": true,
    "message": "Replication complete",
    "successfulReplicas": ["local"],
    "failedReplicas": []
  }
}
```

---

## 6. How to Verify Storage (Physical File on Disk)

To prove that the physical file actually exists in the Storage container (and not just in memory or database):

```powershell
# List files stored inside the Storage container data directory
docker exec cdn-storage-local ls -la /data/files
```

You will see the exact file named by its ID (e.g. `f_1700000000_a1b2c3d4`).

You can even print the file's raw content:
```powershell
docker exec cdn-storage-local cat /data/files/<YOUR_FILE_ID>
```

---

## 7. How to Verify MongoDB Metadata

To inspect the metadata saved in MongoDB:

```powershell
docker exec cdn-mongodb mongosh mini_cdn --eval "db.files.find().pretty()"
```

Notice:
- MongoDB contains `_id`, `originalName`, `contentType`, `size`, `createdAt`, and `replicas`.
- MongoDB does **NOT** hold the binary file itself.

---

## 8. How to Download / View the File

### In the Web Browser
Click the **👁️ View** button (opens an inline preview modal for images and text) or **⬇️ Download** button.

### Via Command Line
```powershell
curl.exe -o downloaded.txt http://localhost:8080/api/files/<YOUR_FILE_ID>
Get-Content downloaded.txt
```

---

## 9. How to Test Geolocation / Nearest Server Routing

The Central API exposes a diagnostics endpoint `GET /api/files/{id}/location`.

Test how different simulated countries route to storage nodes:

### Test Cambodia (KH) $\to$ routes to Singapore
```powershell
curl.exe -H "X-Test-Country: KH" http://localhost:8080/api/files/<YOUR_FILE_ID>/location
```
Response:
```json
{
  "fileId": "f_...",
  "clientIP": "127.0.0.1",
  "clientCountry": "Cambodia",
  "nearestStorage": "singapore",
  "distances": {
    "singapore": 1141.2,
    "london": 10017.8,
    "newyork": 15000.5
  }
}
```

### Test USA (US) $\to$ routes to New York
```powershell
curl.exe -H "X-Test-Country: US" http://localhost:8080/api/files/<YOUR_FILE_ID>/location
```
Response shows `nearestStorage: "newyork"`.

### Test United Kingdom (GB) $\to$ routes to London
```powershell
curl.exe -H "X-Test-Country: GB" http://localhost:8080/api/files/<YOUR_FILE_ID>/location
```
Response shows `nearestStorage: "london"`.

*(In the Web UI, you can also select the country in the top-right dropdown and click **📍 Route** on any file to view this interactively).*

---

## 10. How to Test Storage Failure and Recovery

This test demonstrates that the system handles failures gracefully and does not hang forever.

### Step 1: Stop the Storage Server
```powershell
docker compose stop storage
```

### Step 2: Check System Status
Refresh `http://localhost:3000` or run:
```powershell
curl.exe http://localhost:8080/api/status
```
You will observe:
- `api`: `ONLINE`
- `mongodb`: `ONLINE`
- `storage`: `OFFLINE`

### Step 3: Attempt an Upload while Storage is Down
```powershell
curl.exe -F "file=@testfile.txt" http://localhost:8080/api/files
```
The API returns HTTP `502 Bad Gateway` with a clear JSON error:
```json
{
  "success": false,
  "error": "Storage node(s) unavailable: failed to replicate file"
}
```
The server does not hang and does not crash.

### Step 4: Restart Storage and Verify Recovery
```powershell
docker compose start storage
```
Wait 3 seconds for the healthcheck to pass, then re-check status:
```powershell
curl.exe http://localhost:8080/api/status
```
Status returns to **ONLINE**, and uploads succeed immediately!

---

## 11. How to Delete a File

### From Web UI
Click the red **🗑️** button next to the file.

### Via Command Line
```powershell
curl.exe -X DELETE http://localhost:8080/api/files/<YOUR_FILE_ID>
```
Response:
```json
{
  "success": true,
  "message": "File f_... deleted successfully"
}
```

Verify deletion from disk:
```powershell
docker exec cdn-storage-local ls /data/files
```
The file is gone.

---

## 12. How to Run the Three-Storage Distributed Simulation

When ready to demonstrate full multi-region replication across 3 separate storage nodes:

```text
                     [ Central API ]
                           │
            ┌──────────────┼──────────────┐
            ▼              ▼              ▼
     [ Singapore ]   [ New York ]    [ London ]
       (Port 8081)    (Port 8082)   (Port 8083)
```

### Step 1: Stop the single-storage cluster
```powershell
docker compose down
```

### Step 2: Start the 3-Storage cluster
```powershell
docker compose -f docker-compose.multi.yml up --build -d
```

Verify all 6 containers are running:
```powershell
docker compose -f docker-compose.multi.yml ps
```
Containers running:
- `cdn-website-multi`
- `cdn-central-api-multi`
- `cdn-mongodb-multi`
- `cdn-storage-singapore` (port 8081)
- `cdn-storage-newyork` (port 8082)
- `cdn-storage-london` (port 8083)

### Step 3: Upload a file
```powershell
curl.exe -F "file=@testfile.txt" http://localhost:8080/api/files
```
In the response, observe `replicas` contains **all 3 nodes**:
```json
"successfulReplicas": ["singapore", "newyork", "london"]
```

### Step 4: Verify physical replication across all 3 Docker volumes
```powershell
# Verify Singapore volume
docker exec cdn-storage-singapore ls -la /data/files

# Verify New York volume
docker exec cdn-storage-newyork ls -la /data/files

# Verify London volume
docker exec cdn-storage-london ls -la /data/files
```
The file physically exists on all 3 independent volumes!

### Step 5: Test Geolocation with 3 Nodes
- Request with `X-Test-Country: KH` $\to$ served by `singapore`.
- Request with `X-Test-Country: US` $\to$ served by `newyork`.
- Request with `X-Test-Country: GB` $\to$ served by `london`.

### Step 6: Test Dynamic Failover
Stop the Singapore storage node:
```powershell
docker stop cdn-storage-singapore
```
Request download as Cambodia (`KH`):
```powershell
curl.exe -i -H "X-Test-Country: KH" http://localhost:8080/api/files/<YOUR_FILE_ID>
```
Look at header:
```text
X-Served-By-Storage: london
```
Because Singapore was offline, Central API automatically fell back to the next nearest healthy node (London)!

---

## 13. How to Test Dynamic Storage Worker Onboarding (UPDATE 5)

UPDATE 5 introduces dynamic storage worker registration with `rsync` synchronization and the `on_board` CLI tool.

### Step 1: Start the cluster
```powershell
docker compose up --build -d
```
Notice `cdn-storage-new-node` is running on internal network `storage-new-node:8081` (host port 8084). Its file storage starts completely empty.

### Step 2: Upload initial files
Upload a few files through `http://localhost:3000` or using curl:
```powershell
curl.exe -F "file=@test1.jpg" http://localhost:8080/api/files
curl.exe -F "file=@test2.pdf" http://localhost:8080/api/files
```

### Step 3: Onboard the new storage node using CLI
Run the `on_board` command from root:
```powershell
cd ".."
.\on_board.exe addnode ip=storage-new-node region=Asia country=Japan port=8081 lat=35.6762 lon=139.6503
```
Output:
```text
[1/4] Connecting to Central API at http://localhost:8080...
[2/4] Verifying storage worker health at storage-new-node:8081...
[3/4] Registering node in MongoDB (storage_nodes)...
[4/4] Data synchronized via rsync!

[SUCCESS] Node Onboarded and Activated (ONLINE)!
  Node ID:     node-tokyo
  IP:          storage-new-node
  Port:        8081
  Region:      Asia
  Country:     Japan
  Status:      ONLINE
```

### Step 4: Verify rsync copied the existing files to the new node
```powershell
docker exec cdn-storage-new-node ls -la /data/files
```
You will see `test1.jpg` and `test2.pdf` physically present in the new node's directory!

### Step 5: Test via Web UI
Open `http://localhost:3000/nodes`:
- View all registered nodes in the cluster.
- Click **Add Storage Node** (`http://localhost:3000/nodes/add`) to test onboarding interactively with live progress feedback.

