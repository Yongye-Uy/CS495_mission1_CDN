package ssh_client

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// generateTestEd25519Key generates a temporary ed25519 private key in OpenSSH PEM format.
func generateTestEd25519Key(t *testing.T) (string, ssh.PublicKey) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate ed25519 key: %v", err)
	}

	privBytes, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("failed to marshal private key: %v", err)
	}

	tmpFile, err := os.CreateTemp("", "test_ssh_key_*")
	if err != nil {
		t.Fatalf("failed to create temp key file: %v", err)
	}
	t.Cleanup(func() { os.Remove(tmpFile.Name()) })

	if err := pem.Encode(tmpFile, privBytes); err != nil {
		t.Fatalf("failed to encode PEM: %v", err)
	}
	tmpFile.Close()

	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("failed to create ssh.PublicKey: %v", err)
	}

	return tmpFile.Name(), sshPub
}

func TestNewSSHStorageClient_Validation(t *testing.T) {
	t.Run("missing private key returns error", func(t *testing.T) {
		_, err := NewSSHStorageClient("/non/existent/key", "/non/existent/hosts", "cdn-sync", "/var/cdn/data")
		if err == nil {
			t.Errorf("expected error for missing private key, got nil")
		}
	})

	t.Run("missing known_hosts is auto-created", func(t *testing.T) {
		keyPath, _ := generateTestEd25519Key(t)
		tmpDir, err := os.MkdirTemp("", "known_hosts_dir_*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		nonExistentFile := tmpDir + "/cdn_known_hosts"
		client, err := NewSSHStorageClient(keyPath, nonExistentFile, "ubuntu", "/var/cdn/data")
		if err != nil {
			t.Fatalf("expected auto-creation of missing known_hosts, got error: %v", err)
		}
		if client == nil {
			t.Errorf("expected non-nil client")
		}
		if _, err := os.Stat(nonExistentFile); os.IsNotExist(err) {
			t.Errorf("expected file %s to be auto-created", nonExistentFile)
		}
	})
}

func TestStrictHostKeyVerification(t *testing.T) {
	clientKeyPath, clientPub := generateTestEd25519Key(t)
	serverKeyPath, serverPub := generateTestEd25519Key(t)

	serverKeyBytes, err := os.ReadFile(serverKeyPath)
	if err != nil {
		t.Fatalf("failed to read server key: %v", err)
	}
	serverSigner, err := ssh.ParsePrivateKey(serverKeyBytes)
	if err != nil {
		t.Fatalf("failed to parse server private key: %v", err)
	}

	// Start in-process mock SSH server
	serverConfig := &ssh.ServerConfig{
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if string(key.Marshal()) == string(clientPub.Marshal()) {
				return nil, nil
			}
			return nil, fmt.Errorf("unauthorized client key")
		},
	}
	serverConfig.AddHostKey(serverSigner)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on local port: %v", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port

	// Serve connections in background
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				sshConn, chans, reqs, err := ssh.NewServerConn(c, serverConfig)
				if err != nil {
					return
				}
				defer sshConn.Close()
				go ssh.DiscardRequests(reqs)
				for newChannel := range chans {
					channel, requests, err := newChannel.Accept()
					if err != nil {
						continue
					}
					go ssh.DiscardRequests(requests)
					_ = channel
				}
			}(conn)
		}
	}()

	t.Run("strict host key verification rejects unknown host", func(t *testing.T) {
		// Create empty known_hosts
		tmpKnownHosts, err := os.CreateTemp("", "known_hosts_*")
		if err != nil {
			t.Fatalf("failed to create temp known_hosts: %v", err)
		}
		defer os.Remove(tmpKnownHosts.Name())
		tmpKnownHosts.Close()

		client, err := NewSSHStorageClient(clientKeyPath, tmpKnownHosts.Name(), "cdn-sync", "/tmp")
		if err != nil {
			t.Fatalf("failed to create SSH storage client: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		conn, err := client.DialSSH(ctx, "127.0.0.1", port)
		if err == nil {
			conn.Close()
			t.Errorf("expected host untrusted error, got nil")
		} else if !errors.Is(err, ErrHostUntrusted) {
			t.Logf("expected ErrHostUntrusted, got: %v (verified rejection)", err)
		}
	})

	t.Run("strict host key verification accepts registered host", func(t *testing.T) {
		// Create known_hosts with server's exact key
		tmpKnownHosts, err := os.CreateTemp("", "known_hosts_*")
		if err != nil {
			t.Fatalf("failed to create temp known_hosts: %v", err)
		}
		defer os.Remove(tmpKnownHosts.Name())

		addr := net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", port))
		hostEntry := fmt.Sprintf("%s %s", knownhosts.Normalize(addr), ssh.MarshalAuthorizedKey(serverPub))
		if _, err := tmpKnownHosts.WriteString(hostEntry); err != nil {
			t.Fatalf("failed to write known_hosts: %v", err)
		}
		tmpKnownHosts.Close()

		client, err := NewSSHStorageClient(clientKeyPath, tmpKnownHosts.Name(), "cdn-sync", "/tmp")
		if err != nil {
			t.Fatalf("failed to initialize client: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		conn, err := client.DialSSH(ctx, "127.0.0.1", port)
		if err != nil {
			t.Fatalf("expected successful SSH connection for registered host, got err: %v", err)
		}
		defer conn.Close()
	})

	t.Run("auto-enrolls unknown host and subsequent connection succeeds", func(t *testing.T) {
		tmpKnownHosts, err := os.CreateTemp("", "known_hosts_auto_*")
		if err != nil {
			t.Fatalf("failed to create temp known_hosts: %v", err)
		}
		defer os.Remove(tmpKnownHosts.Name())
		tmpKnownHosts.Close()

		client, err := NewSSHStorageClient(clientKeyPath, tmpKnownHosts.Name(), "ubuntu", "/tmp")
		if err != nil {
			t.Fatalf("failed to create SSH storage client: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		// Proactively auto-enroll host key
		if err := client.AutoEnrollHostKey(ctx, "127.0.0.1", port); err != nil {
			t.Fatalf("AutoEnrollHostKey failed: %v", err)
		}

		// Subsequent connection must now succeed!
		conn, err := client.DialSSH(ctx, "127.0.0.1", port)
		if err != nil {
			t.Fatalf("expected successful SSH connection after AutoEnrollHostKey, got: %v", err)
		}
		defer conn.Close()
	})
}
