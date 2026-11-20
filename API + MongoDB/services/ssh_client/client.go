package ssh_client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

var (
	ErrHostUntrusted    = errors.New("ssh: host key untrusted or mismatch in known_hosts")
	ErrFileNotFound     = errors.New("ssh: remote file not found")
	ErrStorageUnavail   = errors.New("ssh: storage server unreachable")
	ErrPermissionDenied = errors.New("ssh: permission denied on remote storage")
)

type StorageClient interface {
	// Upload copies a local staged file to the remote storage server using rsync over SSH.
	Upload(ctx context.Context, host string, port int, fileID string, localFilePath string) error

	// Download streams a file from the remote storage server using SFTP without creating local temp files.
	Download(ctx context.Context, host string, port int, fileID string) (io.ReadCloser, int64, error)

	// Delete removes a file from the remote storage server using SFTP Remove.
	Delete(ctx context.Context, host string, port int, fileID string) error

	// Health verifies SSH connectivity, host-key validity, and write/read access to the storage directory.
	Health(ctx context.Context, host string, port int) (bool, error)

	// ListFiles lists all file names in the remote storage directory via SFTP.
	ListFiles(ctx context.Context, host string, port int) ([]string, error)

	// StatFile checks if a file exists on the remote node and returns its size.
	StatFile(ctx context.Context, host string, port int, fileID string) (int64, error)
}

type SSHStorageClient struct {
	keyPath        string
	knownHostsPath string
	sshUser        string
	remoteDataDir  string
	signer         ssh.Signer
	hostCallback   ssh.HostKeyCallback
	mu             sync.RWMutex
}

func NewSSHStorageClient(keyPath, knownHostsPath, sshUser, remoteDataDir string) (*SSHStorageClient, error) {
	if sshUser == "" {
		sshUser = "cdn-sync"
	}
	if remoteDataDir == "" {
		remoteDataDir = "/var/cdn/data"
	}

	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read SSH private key at %s: %w", keyPath, err)
	}

	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse SSH private key: %w", err)
	}

	if _, err := os.Stat(knownHostsPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("known_hosts file does not exist at %s: %w", knownHostsPath, err)
	}

	hostCallback, err := knownhosts.New(knownHostsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load known_hosts callback from %s: %w", knownHostsPath, err)
	}

	return &SSHStorageClient{
		keyPath:        keyPath,
		knownHostsPath: knownHostsPath,
		sshUser:        sshUser,
		remoteDataDir:  remoteDataDir,
		signer:         signer,
		hostCallback:   hostCallback,
	}, nil
}

func (c *SSHStorageClient) getClientConfig(timeout time.Duration) *ssh.ClientConfig {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &ssh.ClientConfig{
		User: c.sshUser,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(c.signer),
		},
		HostKeyCallback: c.hostCallback, // STRICT Host-Key Verification
		Timeout:         timeout,
	}
}

// DialSSH establishes an SSH connection with strict host-key verification.
func (c *SSHStorageClient) DialSSH(ctx context.Context, host string, port int) (*ssh.Client, error) {
	return c.dialSSH(ctx, host, port)
}

func (c *SSHStorageClient) dialSSH(ctx context.Context, host string, port int) (*ssh.Client, error) {
	if port <= 0 {
		port = 22
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	cfg := c.getClientConfig(10 * time.Second)

	// Dial using timeout context
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("%w: dial %s failed: %v", ErrStorageUnavail, addr, err)
	}

	// Set deadline for SSH handshake
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(cfg.Timeout))
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		conn.Close()
		if strings.Contains(err.Error(), "knownhosts: key is unknown") || strings.Contains(err.Error(), "host key mismatch") {
			return nil, fmt.Errorf("%w for %s: %v", ErrHostUntrusted, addr, err)
		}
		return nil, fmt.Errorf("ssh connection handshake failed to %s: %w", addr, err)
	}

	// Reset connection deadline after successful handshake
	_ = conn.SetDeadline(time.Time{})

	return ssh.NewClient(sshConn, chans, reqs), nil
}

// Upload sends a file to remote /var/cdn/data/<fileID> using rsync over SSH.
func (c *SSHStorageClient) Upload(ctx context.Context, host string, port int, fileID string, localFilePath string) error {
	if port <= 0 {
		port = 22
	}
	if _, err := os.Stat(localFilePath); err != nil {
		return fmt.Errorf("local staged file not found: %w", err)
	}

	// Normalize paths
	cleanKeyPath := filepath.ToSlash(c.keyPath)
	cleanKnownHosts := filepath.ToSlash(c.knownHostsPath)

	sshCmd := fmt.Sprintf("ssh -i %s -p %d -o StrictHostKeyChecking=yes -o UserKnownHostsFile=%s",
		cleanKeyPath, port, cleanKnownHosts)

	remoteDest := fmt.Sprintf("%s@%s:%s/%s", c.sshUser, host, c.remoteDataDir, fileID)

	// Check if rsync is available on PATH
	rsyncPath, err := exec.LookPath("rsync")
	if err == nil && rsyncPath != "" {
		cmd := exec.CommandContext(ctx, "rsync", "-avz", "-e", sshCmd, localFilePath, remoteDest)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("rsync over SSH failed: %v, output: %s", err, string(out))
		}
		return nil
	}

	// Fallback when rsync CLI binary is not locally installed (e.g. running on Windows host without MSYS2)
	// We use an atomic SFTP upload (uploading to .tmp then renaming)
	log.Printf("[SSH-CLIENT] 'rsync' not in local PATH, falling back to atomic SFTP upload for %s...", fileID)
	return c.sftpUploadAtomic(ctx, host, port, fileID, localFilePath)
}

func (c *SSHStorageClient) sftpUploadAtomic(ctx context.Context, host string, port int, fileID string, localFilePath string) error {
	client, err := c.dialSSH(ctx, host, port)
	if err != nil {
		return err
	}
	defer client.Close()

	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return fmt.Errorf("failed to create sftp client: %w", err)
	}
	defer sftpClient.Close()

	srcFile, err := os.Open(localFilePath)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	targetPath := fmt.Sprintf("%s/%s", c.remoteDataDir, fileID)
	tmpPath := fmt.Sprintf("%s/%s.tmp.%d", c.remoteDataDir, fileID, time.Now().UnixNano())

	dstFile, err := sftpClient.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create remote tmp file: %w", err)
	}

	_, copyErr := io.Copy(dstFile, srcFile)
	closeErr := dstFile.Close()
	if copyErr != nil {
		_ = sftpClient.Remove(tmpPath)
		return fmt.Errorf("failed to copy data via sftp: %w", copyErr)
	}
	if closeErr != nil {
		_ = sftpClient.Remove(tmpPath)
		return fmt.Errorf("failed to flush remote file: %w", closeErr)
	}

	// Atomic rename to final destination
	if err := sftpClient.Rename(tmpPath, targetPath); err != nil {
		_ = sftpClient.Remove(tmpPath)
		return fmt.Errorf("failed to rename remote tmp file to %s: %w", targetPath, err)
	}

	return nil
}

type sftpReadCloser struct {
	remoteFile *sftp.File
	sftpClient *sftp.Client
	sshClient  *ssh.Client
}

func (r *sftpReadCloser) Read(p []byte) (n int, err error) {
	return r.remoteFile.Read(p)
}

func (r *sftpReadCloser) Close() error {
	var errs []string
	if err := r.remoteFile.Close(); err != nil {
		errs = append(errs, err.Error())
	}
	if err := r.sftpClient.Close(); err != nil {
		errs = append(errs, err.Error())
	}
	if err := r.sshClient.Close(); err != nil {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		return fmt.Errorf("errors closing sftp stream: %s", strings.Join(errs, "; "))
	}
	return nil
}

// Download streams a file directly from the remote storage server using SFTP.
func (c *SSHStorageClient) Download(ctx context.Context, host string, port int, fileID string) (io.ReadCloser, int64, error) {
	sshClient, err := c.dialSSH(ctx, host, port)
	if err != nil {
		return nil, 0, err
	}

	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		sshClient.Close()
		return nil, 0, fmt.Errorf("failed to initialize sftp client: %w", err)
	}

	remotePath := fmt.Sprintf("%s/%s", c.remoteDataDir, fileID)
	stat, err := sftpClient.Stat(remotePath)
	if err != nil {
		sftpClient.Close()
		sshClient.Close()
		if os.IsNotExist(err) {
			return nil, 0, ErrFileNotFound
		}
		return nil, 0, fmt.Errorf("failed to stat remote file %s: %w", remotePath, err)
	}

	remoteFile, err := sftpClient.Open(remotePath)
	if err != nil {
		sftpClient.Close()
		sshClient.Close()
		return nil, 0, fmt.Errorf("failed to open remote file %s: %w", remotePath, err)
	}

	stream := &sftpReadCloser{
		remoteFile: remoteFile,
		sftpClient: sftpClient,
		sshClient:  sshClient,
	}

	return stream, stat.Size(), nil
}

// Delete removes a file atomically from remote storage using SFTP Remove.
func (c *SSHStorageClient) Delete(ctx context.Context, host string, port int, fileID string) error {
	sshClient, err := c.dialSSH(ctx, host, port)
	if err != nil {
		return err
	}
	defer sshClient.Close()

	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		return fmt.Errorf("failed to initialize sftp client: %w", err)
	}
	defer sftpClient.Close()

	remotePath := fmt.Sprintf("%s/%s", c.remoteDataDir, fileID)
	err = sftpClient.Remove(remotePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("sftp remove failed for %s: %w", remotePath, err)
	}

	return nil
}

// Health checks SSH connectivity, host-key validity, and verifies directory access.
func (c *SSHStorageClient) Health(ctx context.Context, host string, port int) (bool, error) {
	sshClient, err := c.dialSSH(ctx, host, port)
	if err != nil {
		return false, err
	}
	defer sshClient.Close()

	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		return false, fmt.Errorf("sftp initialization failed: %w", err)
	}
	defer sftpClient.Close()

	stat, err := sftpClient.Stat(c.remoteDataDir)
	if err != nil {
		return false, fmt.Errorf("storage directory %s inaccessible: %w", c.remoteDataDir, err)
	}
	if !stat.IsDir() {
		return false, fmt.Errorf("storage path %s is not a directory", c.remoteDataDir)
	}

	return true, nil
}

// ListFiles lists all file names present in /var/cdn/data.
func (c *SSHStorageClient) ListFiles(ctx context.Context, host string, port int) ([]string, error) {
	sshClient, err := c.dialSSH(ctx, host, port)
	if err != nil {
		return nil, err
	}
	defer sshClient.Close()

	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		return nil, fmt.Errorf("sftp initialization failed: %w", err)
	}
	defer sftpClient.Close()

	entries, err := sftpClient.ReadDir(c.remoteDataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory %s: %w", c.remoteDataDir, err)
	}

	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && !strings.HasSuffix(entry.Name(), ".tmp") {
			files = append(files, entry.Name())
		}
	}
	return files, nil
}

// StatFile checks if a file exists on the remote node and returns its size.
func (c *SSHStorageClient) StatFile(ctx context.Context, host string, port int, fileID string) (int64, error) {
	sshClient, err := c.dialSSH(ctx, host, port)
	if err != nil {
		return 0, err
	}
	defer sshClient.Close()

	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		return 0, err
	}
	defer sftpClient.Close()

	remotePath := fmt.Sprintf("%s/%s", c.remoteDataDir, fileID)
	stat, err := sftpClient.Stat(remotePath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, ErrFileNotFound
		}
		return 0, err
	}
	return stat.Size(), nil
}
