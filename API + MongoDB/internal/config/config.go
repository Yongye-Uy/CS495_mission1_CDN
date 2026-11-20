package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"mini-cdn/api/models"
)

type Config struct {
	Port            string
	Env             string
	MongoURI        string
	MongoDB         string
	MaxUploadSizeMB int64
	StorageNodes    []models.Replica

	// Native Linux Storage SSH & Staging Configuration
	SSHKeyPath      string
	KnownHostsPath  string
	SSHUser         string
	StorageDataDir  string
	StagingDir      string
	AdminAPIKey     string
}

func Load() *Config {
	loadEnvFile(".env", "../.env")

	port := getEnv("API_PORT", "8080")
	env := getEnv("API_ENV", "development")
	mongoURI := getEnv("MONGO_URI", "mongodb://localhost:27017/cdn_metadata?replicaSet=rs0&directConnection=true")
	mongoDB := getEnv("MONGO_DB", "cdn_metadata")

	maxUploadMB, err := strconv.ParseInt(getEnv("MAX_UPLOAD_SIZE_MB", "100"), 10, 64)
	if err != nil || maxUploadMB <= 0 {
		maxUploadMB = 100
	}

	homeDir, _ := os.UserHomeDir()

	// SSH key defaults: check env, then ~/.ssh/storage_key, then Docker local test key
	sshKeyPath := getEnv("SSH_KEY_PATH", "")
	if sshKeyPath == "" {
		defaultKey := filepath.Join(homeDir, ".ssh", "storage_key")
		if _, err := os.Stat(defaultKey); err == nil {
			sshKeyPath = defaultKey
		} else {
			// Dev fallback: local Docker test key if present
			devKey := filepath.Join("..", "Docker", "keys", "storage_test_key")
			if _, err := os.Stat(devKey); err == nil {
				sshKeyPath = devKey
			} else {
				sshKeyPath = defaultKey
			}
		}
	}

	knownHostsPath := getEnv("KNOWN_HOSTS_PATH", "")
	if knownHostsPath == "" {
		defaultKnownHosts := filepath.Join(homeDir, ".ssh", "cdn_known_hosts")
		if _, err := os.Stat(defaultKnownHosts); err == nil {
			knownHostsPath = defaultKnownHosts
		} else {
			devKnownHosts := filepath.Join("..", "Docker", "api", "cdn_known_hosts")
			if _, err := os.Stat(devKnownHosts); err == nil {
				knownHostsPath = devKnownHosts
			} else {
				knownHostsPath = defaultKnownHosts
			}
		}
	}

	sshUser := getEnv("SSH_USER", "cdn-sync")
	storageDataDir := getEnv("STORAGE_DATA_DIR", "/var/cdn/data")
	stagingDir := getEnv("STAGING_DIR", "")
	if stagingDir == "" {
		if env == "production" {
			stagingDir = "/var/lib/mission1/staging"
		} else {
			stagingDir = filepath.Join(os.TempDir(), "mission1_staging")
		}
	}

	adminAPIKey := getEnv("ADMIN_API_KEY", "admin_dev_secret_key_123")

	var storageNodes []models.Replica
	if rawNodes := os.Getenv("STORAGE_NODES"); rawNodes != "" {
		pairs := strings.Split(rawNodes, ",")
		for _, pair := range pairs {
			parts := strings.SplitN(strings.TrimSpace(pair), "=", 2)
			if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
				storageNodes = append(storageNodes, models.Replica{
					Location: strings.ToLower(strings.TrimSpace(parts[0])),
					URL:      strings.TrimRight(strings.TrimSpace(parts[1]), "/"),
				})
			}
		}
	}

	return &Config{
		Port:            port,
		Env:             env,
		MongoURI:        mongoURI,
		MongoDB:         mongoDB,
		MaxUploadSizeMB: maxUploadMB,
		StorageNodes:    storageNodes,
		SSHKeyPath:      sshKeyPath,
		KnownHostsPath:  knownHostsPath,
		SSHUser:         sshUser,
		StorageDataDir:  storageDataDir,
		StagingDir:      stagingDir,
		AdminAPIKey:     adminAPIKey,
	}
}

func loadEnvFile(paths ...string) {
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				val := strings.TrimSpace(parts[1])
				val = strings.Trim(val, `"'`)
				if os.Getenv(key) == "" {
					os.Setenv(key, val)
				}
			}
		}
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
