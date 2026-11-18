package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os/exec"
	"time"

	"mini-cdn/storage/storage"
)

type StorageService struct {
	store    storage.Storage
	location string
	nodeID   string
}

func NewStorageService(store storage.Storage, location string, nodeID string) *StorageService {
	if nodeID == "" {
		nodeID = "node-" + location
	}
	return &StorageService{
		store:    store,
		location: location,
		nodeID:   nodeID,
	}
}

func (s *StorageService) Location() string {
	return s.location
}

func (s *StorageService) NodeID() string {
	return s.nodeID
}

func (s *StorageService) SaveFile(id string, src io.Reader) (int64, error) {
	log.Printf("[STORAGE][%s] Saving file id=%s", s.location, id)
	bytesWritten, err := s.store.Save(id, src)
	if err != nil {
		log.Printf("[STORAGE][%s] Error saving file id=%s: %v", s.location, id, err)
		return 0, err
	}
	log.Printf("[STORAGE][%s] Saved file id=%s (%d bytes)", s.location, id, bytesWritten)
	return bytesWritten, nil
}

func (s *StorageService) GetFile(id string) (io.ReadCloser, int64, error) {
	log.Printf("[STORAGE][%s] Retrieving file id=%s", s.location, id)
	reader, size, err := s.store.Get(id)
	if err != nil {
		if errors.Is(err, storage.ErrFileNotFound) {
			log.Printf("[STORAGE][%s] File not found id=%s", s.location, id)
		} else {
			log.Printf("[STORAGE][%s] Error retrieving file id=%s: %v", s.location, id, err)
		}
		return nil, 0, err
	}
	return reader, size, nil
}

func (s *StorageService) DeleteFile(id string) error {
	log.Printf("[STORAGE][%s] Deleting file id=%s", s.location, id)
	err := s.store.Delete(id)
	if err != nil {
		log.Printf("[STORAGE][%s] Error deleting file id=%s: %v", s.location, id, err)
		return err
	}
	log.Printf("[STORAGE][%s] File deleted id=%s", s.location, id)
	return nil
}

func (s *StorageService) Exists(id string) bool {
	return s.store.Exists(id)
}

func (s *StorageService) ListFiles() ([]string, error) {
	return s.store.List()
}

// SyncFromSource synchronizes files from a source storage node using rsync.
// Uses safe exec.Command without shell expansion.
func (s *StorageService) SyncFromSource(ctx context.Context, sourceHost string, rsyncPort int) error {
	if sourceHost == "" {
		return errors.New("source host cannot be empty")
	}
	if rsyncPort <= 0 {
		rsyncPort = 873
	}

	targetDir := s.store.GetDataDir()
	sourceURL := fmt.Sprintf("rsync://%s:%d/files/", sourceHost, rsyncPort)

	log.Printf("[STORAGE][%s] Initiating rsync from %s to %s", s.location, sourceURL, targetDir)

	// Validate rsync binary exists
	_, lookErr := exec.LookPath("rsync")
	if lookErr != nil {
		log.Printf("[STORAGE][%s] rsync binary not found on PATH: %v (skipping system rsync)", s.location, lookErr)
		return fmt.Errorf("rsync binary not installed on host: %w", lookErr)
	}

	// Safe command execution with timeout
	cmdCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "rsync", "-avz", "--timeout=30", sourceURL, targetDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("[STORAGE][%s] rsync failed: %v, output: %s", s.location, err, string(output))
		return fmt.Errorf("rsync failed: %w (output: %s)", err, string(output))
	}

	log.Printf("[STORAGE][%s] rsync completed successfully: %s", s.location, string(output))
	return nil
}
