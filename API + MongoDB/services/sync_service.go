package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os/exec"
	"time"

	"mini-cdn/api/models"
)

type SyncService struct {
	httpClient *http.Client
}

func NewSyncService() *SyncService {
	return &SyncService{
		httpClient: &http.Client{
			Timeout: 45 * time.Second,
		},
	}
}

type syncWorkerRequest struct {
	SourceHost string `json:"sourceHost"`
	RsyncPort  int    `json:"rsyncPort"`
}

type syncWorkerResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Error   string `json:"error"`
}

type listFilesResponse struct {
	Success bool     `json:"success"`
	Files   []string `json:"files"`
}

// Synchronize transfers files from sourceNode to targetNode.
// First attempts rsync via the worker's /storage/sync endpoint.
// If rsync binary is not available (e.g. Windows local host dev), it provides an HTTP sync fallback.
func (s *SyncService) Synchronize(ctx context.Context, sourceNode, targetNode *models.StorageNode) error {
	if sourceNode == nil || targetNode == nil {
		return errors.New("source and target storage nodes must not be nil")
	}

	log.Printf("[SYNC] Starting synchronization from %s (%s) to %s (%s)",
		sourceNode.NodeID, sourceNode.BaseURL(), targetNode.NodeID, targetNode.BaseURL())

	// Strategy 1: Delegate rsync execution to the target worker via /storage/sync
	err := s.syncViaWorkerRsync(ctx, sourceNode, targetNode)
	if err == nil {
		log.Printf("[SYNC] Worker-level rsync succeeded from %s to %s", sourceNode.NodeID, targetNode.NodeID)
		return nil
	}

	log.Printf("[SYNC] Worker rsync failed (%v), trying Central API direct rsync...", err)

	// Strategy 2: If Central API has rsync installed
	if _, lookErr := exec.LookPath("rsync"); lookErr == nil {
		directErr := s.syncViaDirectRsync(ctx, sourceNode, targetNode)
		if directErr == nil {
			log.Printf("[SYNC] Direct rsync succeeded from %s to %s", sourceNode.NodeID, targetNode.NodeID)
			return nil
		}
		log.Printf("[SYNC] Direct rsync failed (%v)", directErr)
	}

	// Strategy 3: HTTP fallback for environments where rsync daemon or binary isn't active
	log.Printf("[SYNC] Falling back to HTTP replication sync from %s to %s", sourceNode.NodeID, targetNode.NodeID)
	return s.syncViaHTTP(ctx, sourceNode, targetNode)
}

func (s *SyncService) syncViaWorkerRsync(ctx context.Context, sourceNode, targetNode *models.StorageNode) error {
	reqBody := syncWorkerRequest{
		SourceHost: sourceNode.IP,
		RsyncPort:  873,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	targetURL := fmt.Sprintf("%s/storage/sync", targetNode.BaseURL())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call target /storage/sync: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var res syncWorkerResponse
		_ = json.NewDecoder(resp.Body).Decode(&res)
		return fmt.Errorf("target node sync returned status %d: %s", resp.StatusCode, res.Error)
	}

	return nil
}

func (s *SyncService) syncViaDirectRsync(ctx context.Context, sourceNode, targetNode *models.StorageNode) error {
	source := fmt.Sprintf("rsync://%s:873/files/", sourceNode.IP)
	target := fmt.Sprintf("rsync://%s:873/files/", targetNode.IP)

	cmdCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "rsync", "-avz", "--timeout=30", source, target)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("rsync command failed: %w (output: %s)", err, string(out))
	}
	return nil
}

func (s *SyncService) syncViaHTTP(ctx context.Context, sourceNode, targetNode *models.StorageNode) error {
	// 1. List files from source
	listURL := fmt.Sprintf("%s/storage/files", sourceNode.BaseURL())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	if err != nil {
		return err
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to list files from source node: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("source node list files returned status %d", resp.StatusCode)
	}

	var listRes listFilesResponse
	if err := json.NewDecoder(resp.Body).Decode(&listRes); err != nil {
		return fmt.Errorf("failed to decode list files response: %w", err)
	}

	log.Printf("[SYNC] Found %d files on source node %s to synchronize", len(listRes.Files), sourceNode.NodeID)

	for _, fileID := range listRes.Files {
		if err := s.transferFile(ctx, fileID, sourceNode, targetNode); err != nil {
			log.Printf("[SYNC] Warning: failed to transfer file %s: %v", fileID, err)
		}
	}

	return nil
}

func (s *SyncService) transferFile(ctx context.Context, fileID string, sourceNode, targetNode *models.StorageNode) error {
	getURL := fmt.Sprintf("%s/storage/files/%s", sourceNode.BaseURL(), fileID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, getURL, nil)
	if err != nil {
		return err
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to get file %s from source: %w", fileID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("source returned status %d for file %s", resp.StatusCode, fileID)
	}

	// Upload to target
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("id", fileID)
	part, err := writer.CreateFormFile("file", fileID)
	if err != nil {
		return err
	}

	if _, err := io.Copy(part, resp.Body); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}

	targetURL := fmt.Sprintf("%s/storage/files", targetNode.BaseURL())
	uploadReq, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, body)
	if err != nil {
		return err
	}
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())

	uploadResp, err := s.httpClient.Do(uploadReq)
	if err != nil {
		return fmt.Errorf("failed to upload file %s to target: %w", fileID, err)
	}
	defer uploadResp.Body.Close()

	if uploadResp.StatusCode != http.StatusOK && uploadResp.StatusCode != http.StatusCreated {
		return fmt.Errorf("target returned status %d on upload", uploadResp.StatusCode)
	}

	return nil
}
