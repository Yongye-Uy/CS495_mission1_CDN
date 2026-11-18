# Central API + MongoDB

The **Central API** is the core coordinator of the Distributed File Storage / Mini CDN system. It receives files from the Website, handles replication across storage nodes, tracks metadata in MongoDB, determines the nearest storage node based on client geolocation, and provides seamless fallback if a storage node fails.

## Key Responsibilities

1. **Coordination:** Bridges client web requests and distributed physical storage nodes.
2. **Replication Manager:** Fans out file uploads to configured storage locations and tracks individual node replication success/failure.
3. **Metadata Storage:** Persists file details (ID, name, size, type, timestamps, replica locations) in MongoDB.
4. **Geolocation Engine:** Computes great-circle distances using the Haversine formula to dynamically route file requests to the nearest replica.
5. **Failover / Fallback:** Automatically switches to the next nearest healthy replica if the primary replica is down.
6. **Diagnostics & Status:** Exposes diagnostic endpoints (`/api/files/{id}/location` and `/api/status`) for debugging and verification.

## Architecture

```text
HTTP Handler (handlers/file_handler.go)
    ↓
File Service (services/file_service.go)
    ├── Geolocation (services/geo/geo.go)
    ├── Storage Client (services/storage_client/client.go)
    └── MongoDB Repository (repository/file_repository.go)
```

## API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/health` | API operational health check |
| `GET` | `/api/status` | Cluster status (API, MongoDB, and Storage nodes) |
| `POST` | `/api/files` | Upload file (multipart form with `file`) |
| `GET` | `/api/files` | List all uploaded files and their replica nodes |
| `GET` | `/api/files/{id}` | Download file from nearest healthy storage replica |
| `DELETE` | `/api/files/{id}` | Delete file from all replicas and MongoDB |
| `GET` | `/api/files/{id}/location` | Geolocation debugging (nearest node, distances) |

## Geolocation Testing

For local development where client IP is localhost (`127.0.0.1`), simulate different countries using the `X-Test-Country` HTTP header or the `?country=` query parameter:

- `X-Test-Country: KH` (Cambodia) $\to$ routes to **Singapore**
- `X-Test-Country: US` (USA) $\to$ routes to **New York**
- `X-Test-Country: GB` (UK) $\to$ routes to **London**

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `API_PORT` | `8080` | Port on which the API listens |
| `API_ENV` | `development` | Environment mode |
| `MONGO_URI` | `mongodb://localhost:27017` | MongoDB connection string |
| `MONGO_DB` | `mini_cdn` | MongoDB database name |
| `STORAGE_LOCAL_URL` | `http://localhost:8081` | Single-node storage URL |
| `STORAGE_SINGAPORE_URL` | *(empty)* | Singapore storage node URL |
| `STORAGE_NEWYORK_URL` | *(empty)* | New York storage node URL |
| `STORAGE_LONDON_URL` | *(empty)* | London storage node URL |
| `MAX_UPLOAD_SIZE_MB` | `100` | Maximum allowed file upload size |
