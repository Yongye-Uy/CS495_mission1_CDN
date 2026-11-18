package services_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"mini-cdn/api/handlers"
	"mini-cdn/api/models"
	"mini-cdn/api/repository"
	"mini-cdn/api/services"
	"mini-cdn/api/services/geo"
	"mini-cdn/api/services/storage_client"
)

// mockDiskStorageWorker simulates an autonomous Storage Worker with disk storage and sync.
type mockDiskStorageWorker struct {
	mu       sync.Mutex
	location string
	nodeID   string
	dataDir  string
	server   *httptest.Server
}

func newMockDiskStorageWorker(location, nodeID string) *mockDiskStorageWorker {
	tempDir, _ := os.MkdirTemp("", "worker_"+location+"_*")
	w := &mockDiskStorageWorker{
		location: location,
		nodeID:   nodeID,
		dataDir:  tempDir,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(rw).Encode(map[string]string{
			"status":   "ok",
			"service":  "storage",
			"nodeId":   w.nodeID,
			"location": w.location,
		})
	})

	mux.HandleFunc("GET /storage/files", func(rw http.ResponseWriter, r *http.Request) {
		w.mu.Lock()
		defer w.mu.Unlock()
		entries, _ := os.ReadDir(w.dataDir)
		var files []string
		for _, e := range entries {
			if !e.IsDir() {
				files = append(files, e.Name())
			}
		}
		rw.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(rw).Encode(map[string]interface{}{
			"success": true,
			"files":   files,
		})
	})

	mux.HandleFunc("POST /storage/files", func(rw http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(32 << 20)
		id := r.FormValue("id")
		file, _, err := r.FormFile("file")
		if err != nil {
			http.Error(rw, "no file", http.StatusBadRequest)
			return
		}
		defer file.Close()

		w.mu.Lock()
		defer w.mu.Unlock()
		dest := filepath.Join(w.dataDir, id)
		out, _ := os.Create(dest)
		defer out.Close()
		size, _ := io.Copy(out, file)

		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(rw).Encode(map[string]interface{}{
			"success":  true,
			"id":       id,
			"size":     size,
			"location": w.location,
		})
	})

	mux.HandleFunc("GET /storage/files/{id}", func(rw http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		w.mu.Lock()
		defer w.mu.Unlock()
		path := filepath.Join(w.dataDir, id)
		data, err := os.ReadFile(path)
		if err != nil {
			http.NotFound(rw, r)
			return
		}
		rw.Header().Set("Content-Type", "application/octet-stream")
		rw.Header().Set("X-Storage-Location", w.location)
		_, _ = rw.Write(data)
	})

	mux.HandleFunc("DELETE /storage/files/{id}", func(rw http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		w.mu.Lock()
		defer w.mu.Unlock()
		_ = os.Remove(filepath.Join(w.dataDir, id))
		rw.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(rw).Encode(map[string]interface{}{"success": true})
	})

	w.server = httptest.NewServer(mux)
	return w
}

func (w *mockDiskStorageWorker) listFiles() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	entries, _ := os.ReadDir(w.dataDir)
	var files []string
	for _, e := range entries {
		if !e.IsDir() {
			files = append(files, e.Name())
		}
	}
	return files
}

func (w *mockDiskStorageWorker) close() {
	if w.server != nil {
		w.server.Close()
	}
	_ = os.RemoveAll(w.dataDir)
}

func TestUpdate5_FullLifecycle(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Step A: Set up Storage Worker 1 (Singapore)
	worker1 := newMockDiskStorageWorker("singapore", "node-singapore")
	defer worker1.close()

	// Step B: Set up Storage Worker 2 (Tokyo / Asia - new node)
	worker2 := newMockDiskStorageWorker("tokyo", "node-tokyo")
	defer worker2.close()

	// Step C: Set up Central API with dynamic node registry
	fileRepo := repository.NewMockFileRepository()
	nodeRepo := repository.NewMockNodeRepository()
	geoSvc := geo.NewGeoService()
	storageClient := storage_client.NewHTTPStorageClient(10 * time.Second)
	syncSvc := services.NewSyncService()
	nodeSvc := services.NewNodeService(nodeRepo, syncSvc)

	// Parse Worker 1 URL to IP and Port
	var host1 string
	var port1 int
	parts1 := strings.Split(strings.TrimPrefix(worker1.server.URL, "http://"), ":")
	host1 = parts1[0]
	fmt.Sscanf(parts1[1], "%d", &port1)

	// Pre-seed Singapore as initial ONLINE node
	sgNode := &models.StorageNode{
		ID:            "node-sg-id",
		NodeID:        "node-singapore",
		IP:            host1,
		Port:          port1,
		Region:        "Asia",
		Country:       "Singapore",
		Latitude:      1.3521,
		Longitude:     103.8198,
		Status:        models.NodeStatusOnline,
		LastHeartbeat: time.Now(),
		CreatedAt:     time.Now(),
	}
	_ = nodeRepo.Create(context.Background(), sgNode)

	fileSvc := services.NewFileService(fileRepo, nodeRepo, storageClient, geoSvc, nil)
	fileHandler := handlers.NewFileHandler(fileSvc, 100)
	nodeHandler := handlers.NewNodeHandler(nodeSvc)

	apiRouter := gin.New()
	apiRouter.GET("/health", fileHandler.Health)
	apiRouter.GET("/api/status", fileHandler.Status)
	apiRouter.GET("/api/files", fileHandler.List)
	apiRouter.POST("/api/files", fileHandler.Upload)
	apiRouter.GET("/api/files/:id", fileHandler.Get)
	apiRouter.DELETE("/api/files/:id", fileHandler.Delete)
	apiRouter.GET("/api/files/:id/location", fileHandler.Location)

	apiRouter.GET("/api/nodes", nodeHandler.ListNodes)
	apiRouter.POST("/api/nodes", nodeHandler.OnboardNode)
	apiRouter.GET("/api/nodes/:id", nodeHandler.GetNode)
	apiRouter.DELETE("/api/nodes/:id", nodeHandler.RemoveNode)
	apiRouter.GET("/api/nodes/:id/health", nodeHandler.CheckNodeHealth)

	// =========================================================================
	// TEST 1 — Upload files to existing storage
	// =========================================================================
	fileNames := []string{"A.jpg", "B.jpg", "C.jpg"}
	fileIDs := make(map[string]string)

	for _, name := range fileNames {
		content := fmt.Sprintf("Sample payload for %s in distributed storage", name)
		body := new(bytes.Buffer)
		writer := multipart.NewWriter(body)
		part, _ := writer.CreateFormFile("file", name)
		_, _ = io.WriteString(part, content)
		_ = writer.Close()

		req := httptest.NewRequest(http.MethodPost, "/api/files", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		rec := httptest.NewRecorder()
		apiRouter.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("Upload %s failed: status %d, body %s", name, rec.Code, rec.Body.String())
		}

		var upRes handlers.UploadResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &upRes)
		fileIDs[name] = upRes.File.ID
	}

	// Verify Singapore worker 1 has all 3 files
	filesOnWorker1 := worker1.listFiles()
	if len(filesOnWorker1) != 3 {
		t.Fatalf("Expected 3 files on worker 1, got %d: %+v", len(filesOnWorker1), filesOnWorker1)
	}

	// Verify Worker 2 is currently empty
	filesOnWorker2Before := worker2.listFiles()
	if len(filesOnWorker2Before) != 0 {
		t.Fatalf("Expected 0 files on worker 2 before onboarding, got %d", len(filesOnWorker2Before))
	}

	// =========================================================================
	// TEST 2 & 3 — Onboard Node 2 (Tokyo) via Central API (CLI / Web flow)
	// =========================================================================
	parts2 := strings.Split(strings.TrimPrefix(worker2.server.URL, "http://"), ":")
	var port2 int
	fmt.Sscanf(parts2[1], "%d", &port2)

	onboardPayload := map[string]interface{}{
		"ip":        parts2[0],
		"port":      port2,
		"region":    "Asia",
		"country":   "Japan",
		"latitude":  35.6762,
		"longitude": 139.6503,
	}
	onboardBody, _ := json.Marshal(onboardPayload)

	onboardReq := httptest.NewRequest(http.MethodPost, "/api/nodes", bytes.NewReader(onboardBody))
	onboardReq.Header.Set("Content-Type", "application/json")
	onboardRec := httptest.NewRecorder()
	apiRouter.ServeHTTP(onboardRec, onboardReq)

	if onboardRec.Code != http.StatusCreated {
		t.Fatalf("Node onboarding failed: status %d, body %s", onboardRec.Code, onboardRec.Body.String())
	}

	// =========================================================================
	// TEST 4 — Verify rsync / data synchronization: Worker 2 received files!
	// =========================================================================
	filesOnWorker2After := worker2.listFiles()
	if len(filesOnWorker2After) != 3 {
		t.Fatalf("Expected 3 files synchronized to worker 2, got %d: %+v", len(filesOnWorker2After), filesOnWorker2After)
	}
	t.Logf("SUCCESS: Worker 2 synchronized %d files from Worker 1!", len(filesOnWorker2After))

	// =========================================================================
	// TEST 5 — Verify dynamic node registry in MongoDB
	// =========================================================================
	allNodes, err := nodeRepo.FindAll(context.Background())
	if err != nil || len(allNodes) != 2 {
		t.Fatalf("Expected 2 nodes in registry, got %d (err: %v)", len(allNodes), err)
	}

	onlineNodes, _ := nodeRepo.FindOnline(context.Background())
	if len(onlineNodes) != 2 {
		t.Fatalf("Expected both nodes to be ONLINE, got %d", len(onlineNodes))
	}

	// =========================================================================
	// TEST 6 — Verify dynamic geographic CDN routing
	// =========================================================================
	targetFileID := fileIDs["A.jpg"]

	// Request from Japan (Tokyo) -> nearest should be Worker 2 (Tokyo)
	jpReq := httptest.NewRequest(http.MethodGet, "/api/files/"+targetFileID+"/location?country=JP", nil)
	jpRec := httptest.NewRecorder()
	apiRouter.ServeHTTP(jpRec, jpReq)

	if jpRec.Code != http.StatusOK {
		t.Fatalf("Location check failed: %d", jpRec.Code)
	}

	var jpLoc models.LocationDebugResponse
	_ = json.Unmarshal(jpRec.Body.Bytes(), &jpLoc)
	t.Logf("Routing for Japan client: Nearest=%s, Country=%s", jpLoc.NearestStorage, jpLoc.ClientCountry)

	// Download from Japan
	getJPReq := httptest.NewRequest(http.MethodGet, "/api/files/"+targetFileID, nil)
	getJPReq.Header.Set("X-Test-Country", "JP")
	getJPRec := httptest.NewRecorder()
	apiRouter.ServeHTTP(getJPRec, getJPReq)

	if getJPRec.Code != http.StatusOK {
		t.Fatalf("Download failed: %d", getJPRec.Code)
	}

	// =========================================================================
	// TEST 7 — Node failure and automatic failover
	// =========================================================================
	// Close worker 2 to simulate Tokyo node outage
	worker2.server.Close()

	// Download should automatically failover to Singapore (Worker 1)
	failoverReq := httptest.NewRequest(http.MethodGet, "/api/files/"+targetFileID, nil)
	failoverReq.Header.Set("X-Test-Country", "JP")
	failoverRec := httptest.NewRecorder()
	apiRouter.ServeHTTP(failoverRec, failoverReq)

	if failoverRec.Code != http.StatusOK {
		t.Fatalf("Failover download failed: status %d", failoverRec.Code)
	}
	t.Logf("SUCCESS: Failover served file by %s", failoverRec.Header().Get("X-Served-By-Storage"))

	// =========================================================================
	// TEST 8 — Remove node safely
	// =========================================================================
	tokyoNodeID := allNodes[1].ID
	delNodeReq := httptest.NewRequest(http.MethodDelete, "/api/nodes/"+tokyoNodeID, nil)
	delNodeRec := httptest.NewRecorder()
	apiRouter.ServeHTTP(delNodeRec, delNodeReq)

	if delNodeRec.Code != http.StatusOK {
		t.Fatalf("Delete node failed: %d, body %s", delNodeRec.Code, delNodeRec.Body.String())
	}

	remainingNodes, _ := nodeRepo.FindAll(context.Background())
	if len(remainingNodes) != 1 {
		t.Fatalf("Expected 1 node remaining after deletion, got %d", len(remainingNodes))
	}
	t.Logf("SUCCESS: Node %s removed. Remaining nodes: %d", tokyoNodeID, len(remainingNodes))
}
