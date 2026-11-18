package config

import (
	"os"
	"strings"
)

type Config struct {
	Port         string
	ApiBaseURL   string
	PublicApiURL string
}

func Load() *Config {
	loadEnvFile(".env", "../.env")

	port := getEnv("WEBSITE_PORT", "3000")
	apiBaseURL := getEnv("API_BASE_URL", "http://localhost:8080")
	publicApiURL := getEnv("PUBLIC_API_URL", "")

	return &Config{
		Port:         port,
		ApiBaseURL:   strings.TrimRight(apiBaseURL, "/"),
		PublicApiURL: strings.TrimRight(publicApiURL, "/"),
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
