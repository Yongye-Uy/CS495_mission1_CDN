package storage_client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

var (
	ErrStorageUnavailable = errors.New("storage node unavailable")
	ErrStorageNotFound    = errors.New("file not found on storage node")
)

type StorageClient interface {
	Upload(ctx context.Context, nodeURL string, fileID string, filename string, r io.Reader) error
	Download(ctx context.Context, nodeURL string, fileID string) (io.ReadCloser, int64, string, error)
	Delete(ctx context.Context, nodeURL string, fileID string) error
	Health(ctx context.Context, nodeURL string) (bool, string, error)
}

type HTTPStorageClient struct {
	httpClient *http.Client
}

func NewHTTPStorageClient(timeout time.Duration) *HTTPStorageClient {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &HTTPStorageClient{
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *HTTPStorageClient) Upload(ctx context.Context, nodeURL string, fileID string, filename string, r io.Reader) error {
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)

	// Write id field
	if err := writer.WriteField("id", fileID); err != nil {
		return fmt.Errorf("failed to write multipart id: %w", err)
	}

	// Write file field
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return fmt.Errorf("failed to create multipart form file: %w", err)
	}

	if _, err := io.Copy(part, r); err != nil {
		return fmt.Errorf("failed to copy file payload: %w", err)
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to close multipart writer: %w", err)
	}

	targetURL := fmt.Sprintf("%s/storage/files", strings.TrimRight(nodeURL, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, body)
	if err != nil {
		return fmt.Errorf("failed to build upload request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("storage server returned error status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func (c *HTTPStorageClient) Download(ctx context.Context, nodeURL string, fileID string) (io.ReadCloser, int64, string, error) {
	targetURL := fmt.Sprintf("%s/storage/files/%s", strings.TrimRight(nodeURL, "/"), fileID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, 0, "", fmt.Errorf("failed to build download request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, "", fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}

	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, 0, "", ErrStorageNotFound
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, 0, "", fmt.Errorf("storage server returned error status %d: %s", resp.StatusCode, string(respBody))
	}

	contentType := resp.Header.Get("Content-Type")
	contentLength := resp.ContentLength

	return resp.Body, contentLength, contentType, nil
}

func (c *HTTPStorageClient) Delete(ctx context.Context, nodeURL string, fileID string) error {
	targetURL := fmt.Sprintf("%s/storage/files/%s", strings.TrimRight(nodeURL, "/"), fileID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, targetURL, nil)
	if err != nil {
		return fmt.Errorf("failed to build delete request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	defer resp.Body.Close()

	// 200 OK and 404 Not Found are both acceptable during deletion
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("storage server returned error status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func (c *HTTPStorageClient) Health(ctx context.Context, nodeURL string) (bool, string, error) {
	targetURL := fmt.Sprintf("%s/health", strings.TrimRight(nodeURL, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return false, "", err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, "", fmt.Errorf("unhealthy status code: %d", resp.StatusCode)
	}

	var data struct {
		Status   string `json:"status"`
		Service  string `json:"service"`
		Location string `json:"location"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return true, "", nil
	}

	return true, data.Location, nil
}
