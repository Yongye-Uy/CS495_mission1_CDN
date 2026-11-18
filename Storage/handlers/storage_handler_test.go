package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"mini-cdn/storage/services"
	"mini-cdn/storage/storage"
)

func setupTestHandler(t *testing.T, location string) (*StorageHandler, string) {
	tempDir, err := os.MkdirTemp("", "handler_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	store, err := storage.NewDiskStorage(tempDir)
	if err != nil {
		_ = os.RemoveAll(tempDir)
		t.Fatalf("Failed to create DiskStorage: %v", err)
	}

	svc := services.NewStorageService(store, location, "node-"+location)
	handler := NewStorageHandler(svc)
	return handler, tempDir
}

func TestStorageHandler_Health(t *testing.T) {
	handler, tempDir := setupTestHandler(t, "singapore")
	defer os.RemoveAll(tempDir)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.Health(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rec.Code)
	}

	var res HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if res.Status != "ok" || res.Service != "storage" || res.Location != "singapore" {
		t.Errorf("Unexpected health response: %+v", res)
	}
}

func TestStorageHandler_UploadGetDelete(t *testing.T) {
	handler, tempDir := setupTestHandler(t, "newyork")
	defer os.RemoveAll(tempDir)

	fileID := "file-abc-999"
	fileContent := "Distributed storage test payload"

	// 1. Upload
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("id", fileID)
	part, err := writer.CreateFormFile("file", "original.txt")
	if err != nil {
		t.Fatalf("CreateFormFile failed: %v", err)
	}
	_, _ = io.WriteString(part, fileContent)
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/storage/files", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	handler.Upload(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Upload failed with code %d: %s", rec.Code, rec.Body.String())
	}

	var uploadRes UploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &uploadRes); err != nil {
		t.Fatalf("Failed to decode upload response: %v", err)
	}
	if !uploadRes.Success || uploadRes.ID != fileID || uploadRes.Location != "newyork" {
		t.Errorf("Unexpected upload response: %+v", uploadRes)
	}

	// 2. Get
	mux := http.NewServeMux()
	mux.HandleFunc("GET /storage/files/{id}", handler.Get)
	mux.HandleFunc("DELETE /storage/files/{id}", handler.Delete)

	getReq := httptest.NewRequest(http.MethodGet, "/storage/files/"+fileID, nil)
	getRec := httptest.NewRecorder()
	mux.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("Get failed with code %d: %s", getRec.Code, getRec.Body.String())
	}
	if getRec.Body.String() != fileContent {
		t.Errorf("Expected content %q, got %q", fileContent, getRec.Body.String())
	}
	if getRec.Header().Get("X-Storage-Location") != "newyork" {
		t.Errorf("Expected X-Storage-Location newyork, got %s", getRec.Header().Get("X-Storage-Location"))
	}

	// 3. Delete
	delReq := httptest.NewRequest(http.MethodDelete, "/storage/files/"+fileID, nil)
	delRec := httptest.NewRecorder()
	mux.ServeHTTP(delRec, delReq)

	if delRec.Code != http.StatusOK {
		t.Fatalf("Delete failed with code %d: %s", delRec.Code, delRec.Body.String())
	}

	// 4. Get after delete should return 404
	getAfterDelReq := httptest.NewRequest(http.MethodGet, "/storage/files/"+fileID, nil)
	getAfterDelRec := httptest.NewRecorder()
	mux.ServeHTTP(getAfterDelRec, getAfterDelReq)

	if getAfterDelRec.Code != http.StatusNotFound {
		t.Errorf("Expected status 404 after delete, got %d", getAfterDelRec.Code)
	}
}
