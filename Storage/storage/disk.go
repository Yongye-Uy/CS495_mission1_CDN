package storage

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	ErrInvalidID     = errors.New("invalid file ID: contains illegal characters or path traversal attempt")
	ErrFileNotFound  = errors.New("file not found")
	ErrPathTraversal = errors.New("path traversal detected")
	
	// idRegex permits only alphanumeric, underscore, and hyphen characters.
	idRegex = regexp.MustCompile(`^[a-zA-Z0-9_\-]+$`)
)

type Storage interface {
	Save(id string, src io.Reader) (int64, error)
	Get(id string) (io.ReadCloser, int64, error)
	Delete(id string) error
	Exists(id string) bool
	List() ([]string, error)
	GetDataDir() string
}

type DiskStorage struct {
	dataDir string
}

func NewDiskStorage(dataDir string) (*DiskStorage, error) {
	absDir, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve absolute path for data dir: %w", err)
	}

	if err := os.MkdirAll(absDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage data directory: %w", err)
	}

	return &DiskStorage{
		dataDir: absDir,
	}, nil
}

func (d *DiskStorage) GetDataDir() string {
	return d.dataDir
}

// resolvePath validates the ID and constructs a secure file path inside dataDir.
func (d *DiskStorage) resolvePath(id string) (string, error) {
	if strings.TrimSpace(id) == "" || len(id) > 128 {
		return "", ErrInvalidID
	}

	if !idRegex.MatchString(id) {
		return "", ErrInvalidID
	}

	targetPath := filepath.Join(d.dataDir, id)
	cleanPath := filepath.Clean(targetPath)

	// Ensure target path is strictly within dataDir
	rel, err := filepath.Rel(d.dataDir, cleanPath)
	if err != nil || strings.HasPrefix(rel, "..") || rel == "." {
		return "", ErrPathTraversal
	}

	return cleanPath, nil
}

func (d *DiskStorage) Save(id string, src io.Reader) (int64, error) {
	filePath, err := d.resolvePath(id)
	if err != nil {
		return 0, err
	}

	// Write directly to file
	outFile, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return 0, fmt.Errorf("failed to create file on disk: %w", err)
	}
	defer outFile.Close()

	written, err := io.Copy(outFile, src)
	if err != nil {
		_ = os.Remove(filePath)
		return 0, fmt.Errorf("failed to write file content: %w", err)
	}

	return written, nil
}

func (d *DiskStorage) Get(id string) (io.ReadCloser, int64, error) {
	filePath, err := d.resolvePath(id)
	if err != nil {
		return nil, 0, err
	}

	info, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, ErrFileNotFound
		}
		return nil, 0, err
	}

	if info.IsDir() {
		return nil, 0, ErrFileNotFound
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, 0, err
	}

	return file, info.Size(), nil
}

func (d *DiskStorage) Delete(id string) error {
	filePath, err := d.resolvePath(id)
	if err != nil {
		return err
	}

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return ErrFileNotFound
	}

	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("failed to remove file: %w", err)
	}

	return nil
}

func (d *DiskStorage) Exists(id string) bool {
	filePath, err := d.resolvePath(id)
	if err != nil {
		return false
	}

	info, err := os.Stat(filePath)
	return err == nil && !info.IsDir()
}

func (d *DiskStorage) List() ([]string, error) {
	entries, err := os.ReadDir(d.dataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read data directory: %w", err)
	}

	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && idRegex.MatchString(entry.Name()) {
			files = append(files, entry.Name())
		}
	}
	return files, nil
}
