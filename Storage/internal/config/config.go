package config

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	NodeID   string
	Location string
	Port     string
	DataDir  string
}

func Load() *Config {
	loadEnvFile(".env", "../.env")

	location := getEnv("STORAGE_LOCATION", "auto")
	if strings.ToLower(location) == "auto" || strings.TrimSpace(location) == "" {
		location = detectLocation()
	}

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

func detectLocation() string {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("https://ipapi.co/json/")
	if err == nil && resp.StatusCode == http.StatusOK {
		defer resp.Body.Close()
		var geo struct {
			City        string `json:"city"`
			CountryName string `json:"country_name"`
			CountryCode string `json:"country_code"`
		}
		if json.NewDecoder(resp.Body).Decode(&geo) == nil {
			c := strings.ToLower(strings.TrimSpace(geo.City))
			if c != "" {
				return c
			}
			cnt := strings.ToLower(strings.TrimSpace(geo.CountryName))
			if cnt != "" {
				return cnt
			}
		}
	}
	return "singapore"
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
