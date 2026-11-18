package config

import (
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	NodeID   string
	Location string
	Port     string
	DataDir  string
}

func Load() *Config {
	loadEnvFile(".env", "../.env")

	location := getEnv("STORAGE_LOCATION", "singapore")
	nodeID := getEnv("STORAGE_NODE_ID", "node-"+location)
	port := getEnv("STORAGE_PORT", "8081")
	dataDir := getEnv("STORAGE_DATA_DIR", "./data/files")

	cleanDataDir := filepath.Clean(dataDir)

	return &Config{
		NodeID:   nodeID,
		Location: location,
		Port:     port,
		DataDir:  cleanDataDir,
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
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
