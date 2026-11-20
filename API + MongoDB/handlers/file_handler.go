package handlers

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"mini-cdn/api/models"
	"mini-cdn/api/repository"
	"mini-cdn/api/services"
	"mini-cdn/api/services/storage_client"
)

type FileHandler struct {
	service         *services.FileService
	maxUploadSizeMB int64
}

func NewFileHandler(service *services.FileService, maxUploadSizeMB int64) *FileHandler {
	return &FileHandler{
		service:         service,
		maxUploadSizeMB: maxUploadSizeMB,
	}
}

type JSONResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

type UploadResponse struct {
	Success     bool                      `json:"success"`
	ID          string                    `json:"id,omitempty"`
	File        *models.FileMetadata      `json:"file,omitempty"`
	Replication *models.ReplicationStatus `json:"replication,omitempty"`
	Error       string                    `json:"error,omitempty"`
}

// Health handles GET /health
func (h *FileHandler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"service": "api",
	})
}

// Status handles GET /api/status
func (h *FileHandler) Status(c *gin.Context) {
	sysStatus := h.service.GetSystemStatus(c.Request.Context())
	c.JSON(http.StatusOK, sysStatus)
}

// ClientLocation handles GET /api/client-location
func (h *FileHandler) ClientLocation(c *gin.Context) {
	loc := h.service.ResolveClientLocation(c.Request)
	c.JSON(http.StatusOK, loc)
}

// Upload handles POST /api/files
func (h *FileHandler) Upload(c *gin.Context) {
	maxBytes := h.maxUploadSizeMB << 20
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)

	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, JSONResponse{
			Success: false,
			Error:   fmt.Sprintf("Missing or invalid 'file' in multipart form payload: %v", err),
		})
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, JSONResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to open uploaded file: %v", err),
		})
		return
	}
	defer file.Close()

	contentType := fileHeader.Header.Get("Content-Type")
	meta, repStatus, err := h.service.Upload(c.Request.Context(), fileHeader.Filename, contentType, file, fileHeader.Size)
	if err != nil {
		if errors.Is(err, services.ErrAllReplicasFailed) {
			c.JSON(http.StatusBadGateway, UploadResponse{
				Success: false,
				Error:   "Storage node(s) unavailable: failed to replicate file",
			})
			return
		}

		c.JSON(http.StatusInternalServerError, JSONResponse{
			Success: false,
			Error:   fmt.Sprintf("Upload failed: %v", err),
		})
		return
	}

	statusCode := http.StatusCreated
	if !repStatus.Success {
		statusCode = http.StatusMultiStatus
	}

	c.JSON(statusCode, UploadResponse{
		Success:     repStatus.Success,
		ID:          meta.ID,
		File:        meta,
		Replication: repStatus,
	})
}

// List handles GET /api/files
func (h *FileHandler) List(c *gin.Context) {
	files, err := h.service.ListFiles(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, JSONResponse{
			Success: false,
			Error:   "Failed to retrieve file list",
		})
		return
	}

	if files == nil {
		files = []*models.FileMetadata{}
	}

	c.JSON(http.StatusOK, files)
}

// Get handles GET /api/files/:id
func (h *FileHandler) Get(c *gin.Context) {
	fileID := strings.TrimSpace(c.Param("id"))
	if fileID == "" {
		c.JSON(http.StatusBadRequest, JSONResponse{
			Success: false,
			Error:   "File ID is required",
		})
		return
	}

	stream, meta, servedByLoc, err := h.service.GetFileStream(c.Request.Context(), fileID, c.Request)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			c.JSON(http.StatusNotFound, JSONResponse{
				Success: false,
				Error:   "File not found",
			})
			return
		}
		if errors.Is(err, storage_client.ErrStorageNotFound) {
			c.JSON(http.StatusNotFound, JSONResponse{
				Success: false,
				Error:   "File not found on storage replicas",
			})
			return
		}
		if errors.Is(err, services.ErrNoAvailableServer) {
			c.JSON(http.StatusServiceUnavailable, JSONResponse{
				Success: false,
				Error:   "No storage node available to serve the file",
			})
			return
		}

		c.JSON(http.StatusInternalServerError, JSONResponse{
			Success: false,
			Error:   "Internal server error retrieving file",
		})
		return
	}
	defer stream.Close()

	c.Header("Content-Type", meta.ContentType)
	c.Header("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, meta.OriginalName))
	c.Header("X-Served-By-Storage", servedByLoc)
	if meta.Size > 0 {
		c.Header("Content-Length", strconv.FormatInt(meta.Size, 10))
	}

	c.Status(http.StatusOK)
	if _, copyErr := io.Copy(c.Writer, stream); copyErr != nil {
		log.Printf("[API] Error streaming file %s to client: %v", fileID, copyErr)
	}
}

// Delete handles DELETE /api/files/:id
func (h *FileHandler) Delete(c *gin.Context) {
	fileID := strings.TrimSpace(c.Param("id"))
	if fileID == "" {
		c.JSON(http.StatusBadRequest, JSONResponse{
			Success: false,
			Error:   "File ID is required",
		})
		return
	}

	err := h.service.Delete(c.Request.Context(), fileID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			c.JSON(http.StatusNotFound, JSONResponse{
				Success: false,
				Error:   "File not found",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, JSONResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to delete file: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, JSONResponse{
		Success: true,
		Message: fmt.Sprintf("File %s deleted successfully", fileID),
	})
}

// Location handles GET /api/files/:id/location
func (h *FileHandler) Location(c *gin.Context) {
	fileID := strings.TrimSpace(c.Param("id"))
	if fileID == "" {
		c.JSON(http.StatusBadRequest, JSONResponse{
			Success: false,
			Error:   "File ID is required",
		})
		return
	}

	debugResp, err := h.service.GetLocationDebug(c.Request.Context(), fileID, c.Request)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			c.JSON(http.StatusNotFound, JSONResponse{
				Success: false,
				Error:   "File not found",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, JSONResponse{
			Success: false,
			Error:   "Failed to compute location diagnostics",
		})
		return
	}

	c.JSON(http.StatusOK, debugResp)
}

// CORSMiddleware returns a Gin middleware for CORS.
func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Test-Country, Authorization")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusOK)
			return
		}

		c.Next()
	}
}
