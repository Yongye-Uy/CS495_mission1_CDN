package config

import (
	"os"
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
}

func Load() *Config {
	loadEnvFile(".env", "../.env")

	port := getEnv("API_PORT", "8080")
	env := getEnv("API_ENV", "development")
	mongoURI := getEnv("MONGO_URI", "mongodb://localhost:27017")
	mongoDB := getEnv("MONGO_DB", "mini_cdn")

	maxUploadMB, err := strconv.ParseInt(getEnv("MAX_UPLOAD_SIZE_MB", "100"), 10, 64)
	if err != nil || maxUploadMB <= 0 {
		maxUploadMB = 100
	}

	var storageNodes []models.Replica

	// 1. Check custom STORAGE_NODES comma-separated string (e.g., "singapore=http://...,newyork=http://...")
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

	// 2. Check individual regional environment variables
	sgURL := os.Getenv("STORAGE_SINGAPORE_URL")
	nyURL := os.Getenv("STORAGE_NEWYORK_URL")
	lonURL := os.Getenv("STORAGE_LONDON_URL")

	if len(storageNodes) == 0 && (sgURL != "" || nyURL != "" || lonURL != "") {
		if sgURL != "" {
			storageNodes = append(storageNodes, models.Replica{
				Location: "singapore",
				URL:      strings.TrimRight(sgURL, "/"),
			})
		}
		if nyURL != "" {
			storageNodes = append(storageNodes, models.Replica{
				Location: "newyork",
				URL:      strings.TrimRight(nyURL, "/"),
			})
		}
		if lonURL != "" {
			storageNodes = append(storageNodes, models.Replica{
				Location: "london",
				URL:      strings.TrimRight(lonURL, "/"),
			})
		}
	}

	// 3. Fallback to default storage node (Singapore)
	if len(storageNodes) == 0 {
		localURL := getEnv("STORAGE_LOCAL_URL", "http://localhost:8081")
		storageNodes = append(storageNodes, models.Replica{
			Location: "singapore",
			URL:      strings.TrimRight(localURL, "/"),
		})
	}

	return &Config{
		Port:            port,
		Env:             env,
		MongoURI:        mongoURI,
		MongoDB:         mongoDB,
		MaxUploadSizeMB: maxUploadMB,
		StorageNodes:    storageNodes,
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
