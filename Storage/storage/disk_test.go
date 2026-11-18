package storage

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupTestStorage(t *testing.T) (*DiskStorage, string) {
	tempDir, err := os.MkdirTemp("", "storage_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	store, err := NewDiskStorage(tempDir)
	if err != nil {
		_ = os.RemoveAll(tempDir)
		t.Fatalf("Failed to create DiskStorage: %v", err)
	}

	return store, tempDir
}

func TestDiskStorage_SaveAndGet(t *testing.T) {
	store, tempDir := setupTestStorage(t)
	defer os.RemoveAll(tempDir)

	fileID := "test-file-123"
	content := "Hello Distributed Systems! This is a test file."

	// Test Save
	written, err := store.Save(fileID, strings.NewReader(content))
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	if written != int64(len(content)) {
		t.Errorf("Expected written bytes %d, got %d", len(content), written)
	}

	// Test Exists
	if !store.Exists(fileID) {
		t.Errorf("Expected file %s to exist", fileID)
	}

	// Test Get
	reader, size, err := store.Get(fileID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	defer reader.Close()

	if size != int64(len(content)) {
		t.Errorf("Expected size %d, got %d", len(content), size)
	}

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, reader)
	if err != nil {
		t.Fatalf("Failed to read content: %v", err)
	}

	if buf.String() != content {
		t.Errorf("Expected content %q, got %q", content, buf.String())
	}
}

func TestDiskStorage_Delete(t *testing.T) {
	store, tempDir := setupTestStorage(t)
	defer os.RemoveAll(tempDir)

	fileID := "file-to-delete"
	_, err := store.Save(fileID, strings.NewReader("temporary data"))
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	if !store.Exists(fileID) {
		t.Fatal("File should exist before deletion")
	}

	// Delete file
	err = store.Delete(fileID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if store.Exists(fileID) {
		t.Fatal("File should not exist after deletion")
	}

	// Delete non-existent file
	err = store.Delete("non-existent-id")
	if err != ErrFileNotFound {
		t.Errorf("Expected ErrFileNotFound, got %v", err)
	}
}

func TestDiskStorage_MissingFile(t *testing.T) {
	store, tempDir := setupTestStorage(t)
	defer os.RemoveAll(tempDir)

	_, _, err := store.Get("non-existent-file-id")
	if err != ErrFileNotFound {
		t.Errorf("Expected ErrFileNotFound, got %v", err)
	}
}

func TestDiskStorage_PathTraversalProtection(t *testing.T) {
	store, tempDir := setupTestStorage(t)
	defer os.RemoveAll(tempDir)

	maliciousIDs := []string{
		"../escape",
		"../../etc/passwd",
		"..\\windows\\system32",
		"/root/secret",
		"sub/folder",
		"id with spaces",
		"id;with;commands",
		"id$var",
		"",
	}

	for _, malID := range maliciousIDs {
		t.Run("Check "+malID, func(t *testing.T) {
			_, err := store.Save(malID, strings.NewReader("bad"))
			if err == nil {
				t.Errorf("Expected error for malicious ID %q, got nil", malID)
			}

			_, _, err = store.Get(malID)
			if err == nil {
				t.Errorf("Expected error for getting malicious ID %q, got nil", malID)
			}

			err = store.Delete(malID)
			if err == nil {
				t.Errorf("Expected error for deleting malicious ID %q, got nil", malID)
			}
		})
	}
}

func TestDiskStorage_Isolation(t *testing.T) {
	store, tempDir := setupTestStorage(t)
	defer os.RemoveAll(tempDir)

	// Save file and check that file is strictly inside dataDir
	fileID := "isolated-file"
	_, err := store.Save(fileID, strings.NewReader("test data"))
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	expectedPath := filepath.Join(tempDir, fileID)
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("Expected file to be located at %s", expectedPath)
	}
}
