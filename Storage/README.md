# Storage Service

The **Storage Service** represents a storage node in the distributed file storage / Mini CDN system. In production or multi-node simulation, multiple instances of this service run across different simulated geographical locations (e.g., Singapore, New York, London).

## Responsibilities

1. Store uploaded files safely on physical disk.
2. Serve files directly by unique file ID upon request from the Central API.
3. Delete files safely from physical disk when instructed by the Central API.
4. Report its geographical location and operational health.
5. Strictly prevent path traversal and arbitrary filesystem access.

> **Note:** The Storage service contains no business logic about replication or nearest-server calculation. It is an autonomous storage node coordinated exclusively by the Central API.

## API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/health` | Returns health status and node location |
| `POST` | `/storage/files` | Stores a file with form fields `id` and `file` |
| `POST` | `/storage/files/{id}` | Stores a file with path ID and form field `file` |
| `GET` | `/storage/files/{id}` | Streams the file content |
| `DELETE` | `/storage/files/{id}` | Deletes the file from disk |

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `STORAGE_LOCATION` | `local` | Geographical identifier (e.g., `singapore`, `newyork`, `london`) |
| `STORAGE_PORT` | `8081` | Port on which the HTTP server listens |
| `STORAGE_DATA_DIR` | `./data/files` | Directory path where files are persisted |

## Security Features

- **Path Traversal Protection:** Validates that IDs only match alphanumeric and hyphen/underscore characters (`[a-zA-Z0-9_\-]+`) and strictly ensures that the resolved path stays inside `STORAGE_DATA_DIR`.
- **Safe ID Naming:** Files are stored directly named by their unique file ID, avoiding conflicts or execution of user filenames.
