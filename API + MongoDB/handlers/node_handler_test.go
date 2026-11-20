package handlers

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"mini-cdn/api/models"
	"mini-cdn/api/repository"
	"mini-cdn/api/services"
	"mini-cdn/api/services/barrier"
)

type dummyStorageClient struct{}

func (d *dummyStorageClient) Upload(ctx context.Context, host string, port int, fileID string, localFilePath string) error {
	return nil
}

func (d *dummyStorageClient) Download(ctx context.Context, host string, port int, fileID string) (io.ReadCloser, int64, error) {
	return io.NopCloser(bytes.NewReader(nil)), 0, nil
}

func (d *dummyStorageClient) Delete(ctx context.Context, host string, port int, fileID string) error {
	return nil
}

func (d *dummyStorageClient) Health(ctx context.Context, host string, port int) (bool, error) {
	return true, nil
}

func (d *dummyStorageClient) ListFiles(ctx context.Context, host string, port int) ([]string, error) {
	return []string{}, nil
}

func (d *dummyStorageClient) StatFile(ctx context.Context, host string, port int, fileID string) (int64, error) {
	return 0, nil
}

func setupTestNodeHandler() (*NodeHandler, repository.NodeRepository, *gin.Engine) {
	gin.SetMode(gin.TestMode)
	fileRepo := repository.NewMockFileRepository()
	nodeRepo := repository.NewMockNodeRepository()
	journalRepo := repository.NewMockJournalRepository()
	syncJobRepo := repository.NewMockSyncJobRepository()
	client := &dummyStorageClient{}
	b := barrier.NewMutationBarrier()
	stagingDir, _ := os.MkdirTemp("", "api_node_test_staging_*")

	syncService := services.NewSyncService(fileRepo, nodeRepo, journalRepo, syncJobRepo, client, b, stagingDir)
	nodeService := services.NewNodeService(nodeRepo, syncService, client)
	handler := NewNodeHandler(nodeService)

	r := gin.New()
	r.GET("/api/nodes", handler.ListNodes)
	r.POST("/api/nodes", handler.OnboardNode)
	r.GET("/api/nodes/:id", handler.GetNode)
	r.DELETE("/api/nodes/:id", handler.RemoveNode)
	r.GET("/api/nodes/:id/health", handler.CheckNodeHealth)

	return handler, nodeRepo, r
}

func TestNodeHandler_ListNodesEmpty(t *testing.T) {
	_, _, r := setupTestNodeHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}
}

func TestNodeHandler_GetAndRemoveNode(t *testing.T) {
	_, repo, r := setupTestNodeHandler()

	node := &models.StorageNode{
		ID:            "test-node-1",
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
	_ = repo.Create(context.Background(), node)

	// GET node
	req := httptest.NewRequest(http.MethodGet, "/api/nodes/test-node-1", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}

	// DELETE node
	delReq := httptest.NewRequest(http.MethodDelete, "/api/nodes/test-node-1", nil)
	delRec := httptest.NewRecorder()
	r.ServeHTTP(delRec, delReq)

	if delRec.Code != http.StatusOK {
		t.Fatalf("Expected 200 on delete, got %d", delRec.Code)
	}
}
