package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	"mini-cdn/api/models"
	"mini-cdn/api/repository"
	"mini-cdn/api/services"
	"mini-cdn/api/services/barrier"
	"mini-cdn/api/services/geo"
	"mini-cdn/api/services/ssh_client"
)

type testStorageClient struct {
	mu    sync.Mutex
	files map[string][]byte
}

func newTestStorageClient() *testStorageClient {
	return &testStorageClient{
		files: make(map[string][]byte),
	}
}

func (c *testStorageClient) Upload(ctx context.Context, host string, port int, fileID string, localFilePath string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, err := os.ReadFile(localFilePath)
	if err != nil {
		return err
	}
	c.files[fileID] = data
	return nil
}

func (c *testStorageClient) Download(ctx context.Context, host string, port int, fileID string) (io.ReadCloser, int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, ok := c.files[fileID]
	if !ok {
		return nil, 0, ssh_client.ErrFileNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), int64(len(data)), nil
}

func (c *testStorageClient) Delete(ctx context.Context, host string, port int, fileID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.files, fileID)
	return nil
}

func (c *testStorageClient) Health(ctx context.Context, host string, port int) (bool, error) {
	return true, nil
}

func (c *testStorageClient) ListFiles(ctx context.Context, host string, port int) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var res []string
	for k := range c.files {
		res = append(res, k)
	}
	return res, nil
}

func (c *testStorageClient) StatFile(ctx context.Context, host string, port int, fileID string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if d, ok := c.files[fileID]; ok {
		return int64(len(d)), nil
	}
	return 0, ssh_client.ErrFileNotFound
}

func setupTestFileHandler() (*FileHandler, *gin.Engine) {
	gin.SetMode(gin.TestMode)
	repo := repository.NewMockFileRepository()
	nodeRepo := repository.NewMockNodeRepository()
	client := newTestStorageClient()
	geoSvc := geo.NewGeoService()
	b := barrier.NewMutationBarrier()

	nodes := []models.Replica{
		{Location: "singapore", URL: "sg:22"},
		{Location: "newyork", URL: "ny:22"},
		{Location: "london", URL: "london:22"},
	}

	svc := services.NewFileService(repo, nodeRepo, client, geoSvc, b, nodes)
	handler := NewFileHandler(svc, 100)

	r := gin.New()
	r.GET("/health", handler.Health)
	r.POST("/api/files", handler.Upload)
	r.GET("/api/files", handler.List)
	r.GET("/api/files/:id", handler.Get)
	r.DELETE("/api/files/:id", handler.Delete)
	r.GET("/api/files/:id/location", handler.Location)

	return handler, r
}

func TestFileHandler_Health(t *testing.T) {
	_, r := setupTestFileHandler()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("Expected status=ok, got %v", resp["status"])
	}
}

func TestFileHandler_UploadAndGet(t *testing.T) {
	_, r := setupTestFileHandler()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "test.txt")
	if err != nil {
		t.Fatalf("Failed to create form file: %v", err)
	}
	_, _ = part.Write([]byte("File content from handler test"))
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/files", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
	}

	var uploadResp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &uploadResp)
	fileID, ok := uploadResp["id"].(string)
	if !ok || fileID == "" {
		t.Fatalf("Expected valid file id, got %+v", uploadResp)
	}

	// GET file
	getReq := httptest.NewRequest(http.MethodGet, "/api/files/"+fileID, nil)
	getRec := httptest.NewRecorder()
	r.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK from GET, got %d: %s", getRec.Code, getRec.Body.String())
	}
	if getRec.Body.String() != "File content from handler test" {
		t.Errorf("File body mismatch: %s", getRec.Body.String())
	}

	// DELETE file
	delReq := httptest.NewRequest(http.MethodDelete, "/api/files/"+fileID, nil)
	delRec := httptest.NewRecorder()
	r.ServeHTTP(delRec, delReq)

	if delRec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK from DELETE, got %d: %s", delRec.Code, delRec.Body.String())
	}

	// Confirm 404 after delete
	getReqAfter := httptest.NewRequest(http.MethodGet, "/api/files/"+fileID, nil)
	getRecAfter := httptest.NewRecorder()
	r.ServeHTTP(getRecAfter, getReqAfter)

	if getRecAfter.Code != http.StatusNotFound {
		t.Fatalf("Expected 404 Not Found after delete, got %d", getRecAfter.Code)
	}
}
