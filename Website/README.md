# Website Frontend Service (Server #1)

The **Website Service** serves as the user-facing web dashboard (Server #1) for the Distributed File Storage / Mini CDN system. It is written in pure Go and provides an interactive UI for uploading files, monitoring cluster system status, listing and searching distributed files, previewing/downloading files from the nearest storage node, and simulating client geographic locations.

## Responsibilities

1. Serve the dashboard HTML/CSS/JS frontend without external frameworks.
2. Provide a drag-and-drop file upload interface communicating strictly through the Central API.
3. Display real-time cluster health status (Website, Central API, MongoDB, Storage nodes).
4. Reverse-proxy `/api/*` requests to the Central API (`API_BASE_URL`), preventing browser CORS or cross-network complications.
5. Provide client country simulation controls (Cambodia, USA, UK) to test geolocation routing.

> **Architecture Principle:** The Website never connects directly to physical storage servers. All file operations pass through the Central API (Server #2).

## API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/` | Serves the main UI dashboard |
| `GET` | `/health` | Health check endpoint (returns 200 OK) |
| `GET` | `/static/*` | Serves static assets (CSS, JS) |
| `ALL` | `/api/*` | Reverse proxy forwarding requests to the Central API |

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `WEBSITE_PORT` | `3000` | Port on which the web server listens |
| `API_BASE_URL` | `http://localhost:8080` | URL of the Central API coordinator |
| `PUBLIC_API_URL` | *(empty)* | Optional direct public URL for the Central API |
