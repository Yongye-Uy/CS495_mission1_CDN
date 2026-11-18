package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"mini-cdn/storage/services"
	"mini-cdn/storage/storage"
)

type StorageHandler struct {
	service *services.StorageService
}

func NewStorageHandler(service *services.StorageService) *StorageHandler {
	return &StorageHandler{
		service: service,
	}
}

type JSONResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

type HealthResponse struct {
	Status   string `json:"status"`
	Service  string `json:"service"`
	NodeID   string `json:"nodeId"`
	Location string `json:"location"`
}

type UploadResponse struct {
	Success  bool   `json:"success"`
	ID       string `json:"id"`
	Size     int64  `json:"size"`
	Location string `json:"location"`
}

type SyncRequest struct {
	SourceHost string `json:"sourceHost"`
	RsyncPort  int    `json:"rsyncPort"`
}

func (h *StorageHandler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(HealthResponse{
		Status:   "ok",
		Service:  "storage",
		NodeID:   h.service.NodeID(),
		Location: h.service.Location(),
	})
}

// Upload handles POST /storage/files or POST /storage/files/{id}
func (h *StorageHandler) Upload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 100MB max memory/temp parsing limit
	err := r.ParseMultipartForm(100 << 20)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(JSONResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to parse multipart form: %v", err),
		})
		return
	}

	id := r.PathValue("id")
	if id == "" {
		id = r.FormValue("id")
	}

	id = strings.TrimSpace(id)
	if id == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(JSONResponse{
			Success: false,
			Error:   "File ID is required",
		})
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(JSONResponse{
			Success: false,
			Error:   "File content under 'file' form key is required",
		})
		return
	}
	defer file.Close()

	size, err := h.service.SaveFile(id, file)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		if errors.Is(err, storage.ErrInvalidID) || errors.Is(err, storage.ErrPathTraversal) {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(JSONResponse{
				Success: false,
				Error:   err.Error(),
			})
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(JSONResponse{
			Success: false,
			Error:   "Failed to save file to disk",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(UploadResponse{
		Success:  true,
		ID:       id,
		Size:     size,
		Location: h.service.Location(),
	})
}

// Get handles GET /storage/files/{id}
func (h *StorageHandler) Get(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(JSONResponse{
			Success: false,
			Error:   "File ID is required",
		})
		return
	}

	reader, size, err := h.service.GetFile(id)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		if errors.Is(err, storage.ErrFileNotFound) {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(JSONResponse{
				Success: false,
				Error:   "File not found",
			})
			return
		}
		if errors.Is(err, storage.ErrInvalidID) || errors.Is(err, storage.ErrPathTraversal) {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(JSONResponse{
				Success: false,
				Error:   err.Error(),
			})
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(JSONResponse{
			Success: false,
			Error:   "Internal storage error",
		})
		return
	}
	defer reader.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", size))
	w.Header().Set("X-Storage-Location", h.service.Location())
	w.WriteHeader(http.StatusOK)

	_, copyErr := io.Copy(w, reader)
	if copyErr != nil {
		log.Printf("[STORAGE][%s] Error streaming file %s: %v", h.service.Location(), id, copyErr)
	}
}

// Delete handles DELETE /storage/files/{id}
func (h *StorageHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(JSONResponse{
			Success: false,
			Error:   "File ID is required",
		})
		return
	}

	err := h.service.DeleteFile(id)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		if errors.Is(err, storage.ErrFileNotFound) {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(JSONResponse{
				Success: false,
				Error:   "File not found",
			})
			return
		}
		if errors.Is(err, storage.ErrInvalidID) || errors.Is(err, storage.ErrPathTraversal) {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(JSONResponse{
				Success: false,
				Error:   err.Error(),
			})
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(JSONResponse{
			Success: false,
			Error:   "Failed to delete file",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(JSONResponse{
		Success: true,
		Message: fmt.Sprintf("File %s deleted successfully from %s", id, h.service.Location()),
	})
}

// ListFiles handles GET /storage/files
func (h *StorageHandler) ListFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	files, err := h.service.ListFiles()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(JSONResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to list files: %v", err),
		})
		return
	}

	if files == nil {
		files = []string{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"files":    files,
		"location": h.service.Location(),
		"nodeId":   h.service.NodeID(),
	})
}

// Sync handles POST /storage/sync
func (h *StorageHandler) Sync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req SyncRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(JSONResponse{
			Success: false,
			Error:   "Invalid JSON payload for sync request",
		})
		return
	}

	if req.SourceHost == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(JSONResponse{
			Success: false,
			Error:   "sourceHost is required",
		})
		return
	}

	err := h.service.SyncFromSource(r.Context(), req.SourceHost, req.RsyncPort)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(JSONResponse{
			Success: false,
			Error:   fmt.Sprintf("Sync failed: %v", err),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(JSONResponse{
		Success: true,
		Message: fmt.Sprintf("Successfully synchronized files from %s", req.SourceHost),
	})
}
