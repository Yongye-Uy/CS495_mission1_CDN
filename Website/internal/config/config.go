package config

import (
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

type Config struct {
	Port              string
	ApiBaseURL        string
	PublicApiURL      string
	AdminAPIKey       string
	AdminPasswordHash string
	SessionSecret     string
}

func Load() *Config {
	loadEnvFile(".env", "../.env")

	port := getEnv("WEBSITE_PORT", "3000")
	apiBaseURL := getEnv("API_BASE_URL", "http://localhost:8080")
	publicApiURL := getEnv("PUBLIC_API_URL", "")
	adminAPIKey := getEnv("CENTRAL_API_ADMIN_KEY", getEnv("ADMIN_API_KEY", "cdn-admin-secret-key-12345"))
	adminPassword := getEnv("ADMIN_PASSWORD", "admin123")
	adminHash := getEnv("ADMIN_PASSWORD_HASH", "")
	if adminHash == "" {
		hashBytes, _ := bcrypt.GenerateFromPassword([]byte(adminPassword), bcrypt.DefaultCost)
		adminHash = string(hashBytes)
	}
	sessionSecret := getEnv("SESSION_SECRET", "mission1-cdn-session-secret-key-999")

	return &Config{
		Port:              port,
		ApiBaseURL:        strings.TrimRight(apiBaseURL, "/"),
		PublicApiURL:      strings.TrimRight(publicApiURL, "/"),
		AdminAPIKey:       adminAPIKey,
		AdminPasswordHash: adminHash,
		SessionSecret:     sessionSecret,
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
