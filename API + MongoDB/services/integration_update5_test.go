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
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"mini-cdn/api/handlers"
	"mini-cdn/api/models"
	"mini-cdn/api/repository"
	"mini-cdn/api/services"
	"mini-cdn/api/services/barrier"
	"mini-cdn/api/services/geo"
	"mini-cdn/api/services/ssh_client"
)

// mockSSHStorageWorker simulates a native Linux storage server in memory.
type mockSSHStorageWorker struct {
	mu           sync.Mutex
	dataDir      string
	files        map[string][]byte
	failingNodes map[string]bool
}

func newMockSSHStorageWorker() *mockSSHStorageWorker {
	tempDir, _ := os.MkdirTemp("", "mock_ssh_storage_*")
	return &mockSSHStorageWorker{
		dataDir:      tempDir,
		files:        make(map[string][]byte),
		failingNodes: make(map[string]bool),
	}
}

func (w *mockSSHStorageWorker) close() {
	_ = os.RemoveAll(w.dataDir)
}

func (w *mockSSHStorageWorker) SetNodeFailing(host string, port int, failing bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	k := fmt.Sprintf("%s:%d", host, port)
	w.failingNodes[k] = failing
}

func (w *mockSSHStorageWorker) AutoEnrollHostKey(ctx context.Context, host string, port int) error {
	return nil
}

func (w *mockSSHStorageWorker) Upload(ctx context.Context, host string, port int, fileID string, localFilePath string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	k := fmt.Sprintf("%s:%d", host, port)
	if w.failingNodes[k] {
		return fmt.Errorf("ssh upload failed to %s", k)
	}
	data, err := os.ReadFile(localFilePath)
	if err != nil {
		return err
	}
	w.files[k+"/"+fileID] = data
	return nil
}

func (w *mockSSHStorageWorker) Download(ctx context.Context, host string, port int, fileID string) (io.ReadCloser, int64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	k := fmt.Sprintf("%s:%d", host, port)
	if w.failingNodes[k] {
		return nil, 0, fmt.Errorf("ssh download failed from %s", k)
	}
	data, exists := w.files[k+"/"+fileID]
	if !exists {
		return nil, 0, ssh_client.ErrFileNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), int64(len(data)), nil
}

func (w *mockSSHStorageWorker) Delete(ctx context.Context, host string, port int, fileID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	k := fmt.Sprintf("%s:%d", host, port)
	delete(w.files, k+"/"+fileID)
	return nil
}

func (w *mockSSHStorageWorker) Health(ctx context.Context, host string, port int) (bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	k := fmt.Sprintf("%s:%d", host, port)
	if w.failingNodes[k] {
		return false, fmt.Errorf("node %s offline", k)
	}
	return true, nil
}

func (w *mockSSHStorageWorker) ListFiles(ctx context.Context, host string, port int) ([]string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	k := fmt.Sprintf("%s:%d", host, port)
	prefix := k + "/"
	var res []string
	for key := range w.files {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			res = append(res, key[len(prefix):])
		}
	}
	return res, nil
}

func (w *mockSSHStorageWorker) StatFile(ctx context.Context, host string, port int, fileID string) (int64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	k := fmt.Sprintf("%s:%d", host, port)
	if data, ok := w.files[k+"/"+fileID]; ok {
		return int64(len(data)), nil
	}
	return 0, ssh_client.ErrFileNotFound
}

func TestEndToEnd_OnboardingAndReplicationPipeline(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Step A: Storage client
	mockStorage := newMockSSHStorageWorker()
	defer mockStorage.close()

	// Step B: Set up Repositories and Services
	fileRepo := repository.NewMockFileRepository()
	nodeRepo := repository.NewMockNodeRepository()
	geoSvc := geo.NewGeoService()
	b := barrier.NewMutationBarrier()

	syncSvc := services.NewSyncService(fileRepo, nodeRepo, mockStorage, b)
	nodeSvc := services.NewNodeService(nodeRepo, syncSvc, mockStorage)
	nodeSvc.SetFileRepo(fileRepo)
	nodeSvc.SetGeoService(geoSvc)

	// Pre-seed Singapore as initial ONLINE node
	sgNode := &models.StorageNode{
		ID:            "node-sg-id",
		NodeID:        "node-singapore",
		IP:            "127.0.0.1",
		Port:          2201,
		Region:        "Asia",
		Country:       "Singapore",
		Latitude:      1.3521,
		Longitude:     103.8198,
		Status:        models.NodeStatusOnline,
		LastHeartbeat: time.Now(),
		CreatedAt:     time.Now(),
	}
	_ = nodeRepo.Create(context.Background(), sgNode)

	fileSvc := services.NewFileService(fileRepo, nodeRepo, mockStorage, geoSvc, b, nil)
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
	// TEST 1: Initial upload replicating to Singapore
	// =========================================================================
	t.Run("1. Upload to Initial Cluster", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, _ := writer.CreateFormFile("file", "course_syllabus.pdf")
		_, _ = part.Write([]byte("Course Syllabus Content: Distributed Systems"))
		_ = writer.Close()

		req := httptest.NewRequest("POST", "/api/files", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		rec := httptest.NewRecorder()
		apiRouter.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated && rec.Code != http.StatusMultiStatus {
			t.Fatalf("Expected HTTP 201 Created or 207 MultiStatus, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		uploadedID := resp["id"].(string)

		// Verify stored in mock storage for node-singapore
		data, err := mockStorage.StatFile(context.Background(), "127.0.0.1", 2201, uploadedID)
		if err != nil || data == 0 {
			t.Fatalf("File not found on node-singapore: %v", err)
		}
	})

	// =========================================================================
	// TEST 2: Onboard New Storage Worker (Tokyo) via API
	// =========================================================================
	t.Run("2. Dynamic Onboarding of Tokyo Node", func(t *testing.T) {
		onboardPayload := map[string]interface{}{
			"ip":        "127.0.0.1",
			"port":      2202,
			"region":    "Asia",
			"country":   "Japan",
			"latitude":  35.6762,
			"longitude": 139.6503,
		}
		pBytes, _ := json.Marshal(onboardPayload)

		req := httptest.NewRequest("POST", "/api/nodes", bytes.NewReader(pBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		apiRouter.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("Expected HTTP 201 Created from OnboardNode, got %d: %s", rec.Code, rec.Body.String())
		}

		// Verify Tokyo node exists and is ONLINE
		onlineNodes, err := nodeRepo.FindOnline(context.Background())
		if err != nil || len(onlineNodes) < 2 {
			t.Fatalf("Expected at least 2 online nodes after onboarding, got %d", len(onlineNodes))
		}
	})
}
