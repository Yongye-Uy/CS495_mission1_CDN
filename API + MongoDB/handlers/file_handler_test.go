package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	"mini-cdn/api/models"
	"mini-cdn/api/repository"
	"mini-cdn/api/services"
	"mini-cdn/api/services/geo"
	"mini-cdn/api/services/storage_client"
)

// Mock client for handler tests
type testStorageClient struct {
	mu    sync.Mutex
	files map[string]map[string][]byte
}

func newTestStorageClient() *testStorageClient {
	return &testStorageClient{files: make(map[string]map[string][]byte)}
}

func (c *testStorageClient) Upload(ctx context.Context, nodeURL string, fileID string, filename string, r io.Reader) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, _ := io.ReadAll(r)
	if _, ok := c.files[nodeURL]; !ok {
		c.files[nodeURL] = make(map[string][]byte)
	}
	c.files[nodeURL][fileID] = data
	return nil
}

func (c *testStorageClient) Download(ctx context.Context, nodeURL string, fileID string) (io.ReadCloser, int64, string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, ok := c.files[nodeURL][fileID]
	if !ok {
		return nil, 0, "", storage_client.ErrStorageNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), int64(len(data)), "text/plain", nil
}

func (c *testStorageClient) Delete(ctx context.Context, nodeURL string, fileID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.files[nodeURL], fileID)
	return nil
}

func (c *testStorageClient) Health(ctx context.Context, nodeURL string) (bool, string, error) {
	return true, "test-loc", nil
}

func setupTestFileHandler() (*FileHandler, *gin.Engine) {
	gin.SetMode(gin.TestMode)
	repo := repository.NewMockFileRepository()
	nodeRepo := repository.NewMockNodeRepository()
	client := newTestStorageClient()
	geoSvc := geo.NewGeoService()
	nodes := []models.Replica{{Location: "singapore", URL: "http://sg:8081"}}

	svc := services.NewFileService(repo, nodeRepo, client, geoSvc, nodes)
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

	var res map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if res["status"] != "ok" || res["service"] != "api" {
		t.Errorf("Unexpected health response: %+v", res)
	}
}

func TestFileHandler_UploadAndGetAndLocation(t *testing.T) {
	_, r := setupTestFileHandler()

	fileContent := "Test distributed file content"

	// 1. Upload
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "document.txt")
	_, _ = io.WriteString(part, fileContent)
	_ = writer.Close()

	uploadReq := httptest.NewRequest(http.MethodPost, "/api/files", body)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadRec := httptest.NewRecorder()
	r.ServeHTTP(uploadRec, uploadReq)

	if uploadRec.Code != http.StatusCreated {
		t.Fatalf("Upload failed with status %d: %s", uploadRec.Code, uploadRec.Body.String())
	}

	var upResp UploadResponse
	_ = json.Unmarshal(uploadRec.Body.Bytes(), &upResp)
	if !upResp.Success || upResp.File == nil || upResp.File.ID == "" {
		t.Fatalf("Invalid upload response: %+v", upResp)
	}

	fileID := upResp.File.ID

	// 2. List
	listReq := httptest.NewRequest(http.MethodGet, "/api/files", nil)
	listRec := httptest.NewRecorder()
	r.ServeHTTP(listRec, listReq)

	if listRec.Code != http.StatusOK {
		t.Fatalf("List returned status %d", listRec.Code)
	}

	var files []*models.FileMetadata
	_ = json.Unmarshal(listRec.Body.Bytes(), &files)
	if len(files) != 1 || files[0].ID != fileID {
		t.Errorf("Expected file in list, got %+v", files)
	}

	// 3. Location debug endpoint
	locReq := httptest.NewRequest(http.MethodGet, "/api/files/"+fileID+"/location", nil)
	locReq.Header.Set("X-Test-Country", "KH")
	locRec := httptest.NewRecorder()
	r.ServeHTTP(locRec, locReq)

	if locRec.Code != http.StatusOK {
		t.Fatalf("Location returned status %d: %s", locRec.Code, locRec.Body.String())
	}

	var locResp models.LocationDebugResponse
	_ = json.Unmarshal(locRec.Body.Bytes(), &locResp)
	if locResp.NearestStorage != "singapore" || locResp.ClientCountry != "Cambodia" {
		t.Errorf("Unexpected location debug response: %+v", locResp)
	}

	// 4. Download file
	getReq := httptest.NewRequest(http.MethodGet, "/api/files/"+fileID, nil)
	getReq.Header.Set("X-Test-Country", "KH")
	getRec := httptest.NewRecorder()
	r.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("Get returned status %d: %s", getRec.Code, getRec.Body.String())
	}
	if getRec.Body.String() != fileContent {
		t.Errorf("Expected content %q, got %q", fileContent, getRec.Body.String())
	}

	// 5. Delete file
	delReq := httptest.NewRequest(http.MethodDelete, "/api/files/"+fileID, nil)
	delRec := httptest.NewRecorder()
	r.ServeHTTP(delRec, delReq)

	if delRec.Code != http.StatusOK {
		t.Fatalf("Delete returned status %d: %s", delRec.Code, delRec.Body.String())
	}

	// 6. Get after delete -> 404
	getAgainReq := httptest.NewRequest(http.MethodGet, "/api/files/"+fileID, nil)
	getAgainRec := httptest.NewRecorder()
	r.ServeHTTP(getAgainRec, getAgainReq)

	if getAgainRec.Code != http.StatusNotFound {
		t.Errorf("Expected 404 after delete, got %d", getAgainRec.Code)
	}
}
